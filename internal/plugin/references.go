package plugin

import (
	"context"
	"errors"
	"fmt"
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
