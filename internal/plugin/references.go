package plugin

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/kandev/kandev/pkg/pluginsdk"
	"github.com/kdlbs/kandev-plugin-youtrack/internal/youtrack"
)

const (
	defaultReferenceLimit = 5
	maxReferenceLimit     = 10
	maxReferenceQueryLen  = 200
)

var (
	// ErrReferenceWorkspaceRequired mirrors Jira's ErrMentionWorkspaceRequired.
	ErrReferenceWorkspaceRequired = errors.New("youtrack: reference workspace ID required")
	// ErrReferenceInvalidQuery mirrors Jira's ErrMentionInvalidQuery.
	ErrReferenceInvalidQuery = errors.New("youtrack: invalid reference query")
)

// The plugin owns a complete reference source (search + authorize).
var _ pluginsdk.EntityReferenceHandler = (*Plugin)(nil)

// referenceIssueKeyPattern mirrors Jira's mentionIssueKeyPattern: YouTrack
// readable IDs look like FPU-123 (project short name is letters/digits).
var referenceIssueKeyPattern = regexp.MustCompile(`(?i)^[a-z][a-z0-9]+-[0-9]+$`)

// buildReferenceQuery translates plain user text into one fixed YouTrack query
// shape, mirroring Jira's buildMentionJQL: an exact readable-ID lookup when the
// text looks like FPU-123, a summary substring search otherwise. Free-text
// values are wrapped in YouTrack's {...} literal quoting with backslash and
// closing brace escaped, so user input can never widen the query.
func buildReferenceQuery(rawQuery string) (string, error) {
	query := strings.TrimSpace(rawQuery)
	if query == "" || !utf8.ValidString(query) || utf8.RuneCountInString(query) > maxReferenceQueryLen {
		return "", ErrReferenceInvalidQuery
	}
	if referenceIssueKeyPattern.MatchString(query) {
		return strings.ToUpper(query), nil
	}
	return "Summary: {" + escapeYouTrackLiteral(query) + "}", nil
}

func escapeYouTrackLiteral(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, `}`, `\}`)
}

// normalizeReferenceLimit mirrors Jira's normalizeMentionSearchLimit: 0 means
// the default, values are clamped into [1, max].
func normalizeReferenceLimit(limit int32) int {
	switch {
	case limit == 0:
		return defaultReferenceLimit
	case limit < 1:
		return 1
	case limit > maxReferenceLimit:
		return maxReferenceLimit
	default:
		return int(limit)
	}
}

// SearchEntityReferences implements pluginsdk.EntityReferenceSearcher so
// YouTrack issues can be @-referenced from the composer, mirroring the built-in
// Jira mention source. The host only routes sources declared in the manifest
// (reference_sources) and always builds the canonical reference itself; the
// candidates here are untrusted presentation data.
func (p *Plugin) SearchEntityReferences(ctx context.Context, req *pluginsdk.SearchEntityReferencesRequest) (*pluginsdk.SearchEntityReferencesResponse, error) {
	wsID := strings.TrimSpace(req.WorkspaceID)
	if wsID == "" {
		return nil, ErrReferenceWorkspaceRequired
	}
	query, err := buildReferenceQuery(req.Query)
	if err != nil {
		return nil, err
	}
	limit := normalizeReferenceLimit(req.Limit)
	client, err := p.client(ctx, wsID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", youtrack.ErrNotConfigured, err)
	}
	res, err := client.SearchIssues(ctx, query, "", "", limit)
	if err != nil {
		return nil, err
	}
	count := len(res.Issues)
	if count > limit {
		count = limit
	}
	candidates := make([]pluginsdk.EntityReferenceCandidate, 0, count)
	for _, issue := range res.Issues[:count] {
		candidates = append(candidates, pluginsdk.EntityReferenceCandidate{
			ProviderLocalID: issue.ID,
			Title:           issue.IDReadable + " " + issue.Summary,
			URL:             issue.URL,
			Attributes: map[string]any{
				"key":   issue.IDReadable,
				"state": issue.State,
			},
		})
	}
	return &pluginsdk.SearchEntityReferencesResponse{Candidates: candidates}, nil
}

// Entity reference authorization purposes, mirroring the host's
// mentions.ReferencePurposeSearch / ReferencePurposeSubmission values.
const (
	referencePurposeSearch     = "search"
	referencePurposeSubmission = "submission"
)

var (
	// referenceProvider / referenceKind must match the manifest's
	// reference_sources provider and kind.
	referenceProvider = "youtrack"
	referenceKind     = "issue"
)

// deniedReference is a uniform "not allowed" answer, mirroring Jira's
// ErrReferenceUnauthorized semantics: the host surfaces only allowed/not.
func deniedReference(reason string) (*pluginsdk.AuthorizeEntityReferenceResponse, error) {
	return &pluginsdk.AuthorizeEntityReferenceResponse{Allowed: false, Reason: reason}, nil
}

// AuthorizeEntityReference implements pluginsdk.EntityReferenceAuthorizer.
// The host's mention bridge requires a live authorizer: search results are
// filtered through it and every submission is re-checked, so a plugin that
// only implements the searcher would have all its candidates dropped.
//
// Mirrors the built-in Jira provider's AuthorizeReference: validate the
// canonical reference shape offline, then require the reference URL to point
// at this workspace's configured YouTrack instance — a user-crafted
// reference to a different instance (or a non-http URL) is rejected without
// any YouTrack API call.
func (p *Plugin) AuthorizeEntityReference(ctx context.Context, req *pluginsdk.AuthorizeEntityReferenceRequest) (*pluginsdk.AuthorizeEntityReferenceResponse, error) {
	if req == nil {
		return deniedReference("missing request")
	}
	wsID := strings.TrimSpace(req.WorkspaceID)
	if wsID == "" || wsID != req.WorkspaceID {
		return deniedReference("workspace required")
	}
	if req.Purpose != referencePurposeSearch && req.Purpose != referencePurposeSubmission {
		return deniedReference("invalid purpose")
	}
	reference := req.Reference
	if reference == nil {
		return deniedReference("missing reference")
	}
	if stringField(reference, "provider") != referenceProvider || stringField(reference, "kind") != referenceKind {
		return deniedReference("provider/kind mismatch")
	}
	// The bridge pre-checks scope; kept here as defense in depth, exactly like
	// Jira's validJiraReferenceShape guards id/key shape.
	if stringField(reference, "scope") != wsID {
		return deniedReference("scope mismatch")
	}
	id := stringField(reference, "id")
	key := stringField(reference, "key")
	if id == "" || strings.TrimSpace(id) != id || key == "" || strings.TrimSpace(key) != key {
		return deniedReference("invalid id/key shape")
	}
	cfg, err := p.loadConfig(ctx, wsID)
	if err != nil {
		return deniedReference("youtrack not configured for this workspace")
	}
	if !referenceURLPointsAtBase(stringField(reference, "url"), cfg.BaseURL) {
		return deniedReference("reference URL does not match the configured YouTrack instance")
	}
	return &pluginsdk.AuthorizeEntityReferenceResponse{Allowed: true}, nil
}

// referenceURLPointsAtBase reports whether refURL has a valid http(s) shape
// and the same scheme+host as the configured YouTrack base URL. Host compare
// is case-insensitive per RFC 3986.
func referenceURLPointsAtBase(refURL, baseURL string) bool {
	parsedRef, err := url.Parse(refURL)
	if err != nil || parsedRef.User != nil || parsedRef.Host == "" ||
		(parsedRef.Scheme != "http" && parsedRef.Scheme != "https") {
		return false
	}
	parsedBase, err := url.Parse(youtrack.NormalizeBaseURL(baseURL))
	if err != nil || parsedBase.Host == "" {
		return false
	}
	return parsedRef.Scheme == parsedBase.Scheme && strings.EqualFold(parsedRef.Host, parsedBase.Host)
}
