package plugin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pluginsdk "github.com/kandev/kandev/pkg/pluginsdk"
	"github.com/kdlbs/kandev-plugin-youtrack/internal/youtrack"
)

func TestBuildReferenceQuery(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "FPU-123", want: "FPU-123"},
		{in: "fpu-123", want: "FPU-123"},
		{in: "  fix login bug  ", want: "Summary: {fix login bug}"},
		{in: "curly } brace", want: "Summary: {curly \\} brace}"},
		{in: `back\slash`, want: `Summary: {back\\slash}`},
		{in: "", wantErr: true},
		{in: "   ", wantErr: true},
		{in: strings.Repeat("x", 201), wantErr: true},
	}
	for _, tc := range cases {
		got, err := buildReferenceQuery(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("buildReferenceQuery(%q) = %q, want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("buildReferenceQuery(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("buildReferenceQuery(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeReferenceLimit(t *testing.T) {
	cases := []struct {
		in   int32
		want int
	}{
		{in: 0, want: defaultReferenceLimit},
		{in: -1, want: 1},
		{in: 1, want: 1},
		{in: 3, want: 3},
		{in: 11, want: maxReferenceLimit},
	}
	for _, tc := range cases {
		if got := normalizeReferenceLimit(tc.in); got != tc.want {
			t.Errorf("normalizeReferenceLimit(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestSearchEntityReferences(t *testing.T) {
	mux := newSearchIssuesServer(t)
	p := newTestPlugin(t, mux.URL)

	resp, err := p.SearchEntityReferences(context.Background(), &pluginsdk.SearchEntityReferencesRequest{
		WorkspaceID: "ws-1",
		Query:       "crash",
		Limit:       5,
	})
	if err != nil {
		t.Fatalf("SearchEntityReferences: %v", err)
	}
	if len(resp.Candidates) != 2 {
		t.Fatalf("candidates = %d, want 2", len(resp.Candidates))
	}
	first := resp.Candidates[0]
	if first.ProviderLocalID != "2-1" {
		t.Errorf("ProviderLocalID = %q", first.ProviderLocalID)
	}
	if !strings.HasPrefix(first.Title, "FPU-1 ") {
		t.Errorf("Title = %q, want FPU-1 prefix", first.Title)
	}
	if first.URL == "" {
		t.Error("URL should be populated")
	}
	if first.Attributes["key"] != "FPU-1" {
		t.Errorf("Attributes[key] = %v", first.Attributes["key"])
	}
}

func TestSearchEntityReferences_LimitClamped(t *testing.T) {
	mux := newSearchIssuesServer(t)
	p := newTestPlugin(t, mux.URL)

	resp, err := p.SearchEntityReferences(context.Background(), &pluginsdk.SearchEntityReferencesRequest{
		WorkspaceID: "ws-1",
		Query:       "crash",
		Limit:       99,
	})
	if err != nil {
		t.Fatalf("SearchEntityReferences: %v", err)
	}
	if len(resp.Candidates) != 2 {
		t.Errorf("candidates = %d, want 2 (the fake server only returns two)", len(resp.Candidates))
	}
}

func TestSearchEntityReferences_Validations(t *testing.T) {
	mux := newSearchIssuesServer(t)
	p := newTestPlugin(t, mux.URL)
	ctx := context.Background()

	if _, err := p.SearchEntityReferences(ctx, &pluginsdk.SearchEntityReferencesRequest{Query: "x"}); !errors.Is(err, ErrReferenceWorkspaceRequired) {
		t.Errorf("missing workspace err = %v", err)
	}
	if _, err := p.SearchEntityReferences(ctx, &pluginsdk.SearchEntityReferencesRequest{WorkspaceID: "ws-1", Query: ""}); !errors.Is(err, ErrReferenceInvalidQuery) {
		t.Errorf("empty query err = %v", err)
	}
	if _, err := p.SearchEntityReferences(ctx, &pluginsdk.SearchEntityReferencesRequest{WorkspaceID: "ws-none", Query: "x"}); !errors.Is(err, youtrack.ErrNotConfigured) {
		t.Errorf("unconfigured workspace err = %v", err)
	}
}

func TestAuthorizeEntityReference(t *testing.T) {
	srv := newSearchIssuesServer(t)
	p := newTestPlugin(t, srv.URL)
	ctx := context.Background()
	allowedRef := func() map[string]any {
		return map[string]any{
			"version": 1, "ref": "youtrack/issue/" + srv.URL + "/2-1",
			"provider": "youtrack", "kind": "issue",
			"id": "2-1", "key": "2-1", "title": "FPU-1 first crash",
			"url": srv.URL + "/issue/FPU-1", "scope": "ws-1",
		}
	}

	// Well-formed reference pointing at the configured instance: allowed for
	// both purposes (Jira authorizes search and submission the same way).
	for _, purpose := range []string{"search", "submission"} {
		resp, err := p.AuthorizeEntityReference(ctx, &pluginsdk.AuthorizeEntityReferenceRequest{
			WorkspaceID: "ws-1", Purpose: purpose, Reference: allowedRef(),
		})
		if err != nil {
			t.Fatalf("AuthorizeEntityReference(%s): %v", purpose, err)
		}
		if !resp.Allowed {
			t.Errorf("purpose %s: denied (%s)", purpose, resp.Reason)
		}
	}

	// Rejections.
	deny := func(name string, req *pluginsdk.AuthorizeEntityReferenceRequest) {
		t.Helper()
		resp, err := p.AuthorizeEntityReference(ctx, req)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", name, err)
		}
		if resp.Allowed {
			t.Errorf("%s: expected denial", name)
		}
	}
	deny("empty workspace", &pluginsdk.AuthorizeEntityReferenceRequest{WorkspaceID: " ", Reference: allowedRef()})
	deny("bad purpose", &pluginsdk.AuthorizeEntityReferenceRequest{WorkspaceID: "ws-1", Purpose: "other", Reference: allowedRef()})
	ref := allowedRef()
	ref["provider"] = "jira"
	deny("wrong provider", &pluginsdk.AuthorizeEntityReferenceRequest{WorkspaceID: "ws-1", Purpose: "search", Reference: ref})
	ref = allowedRef()
	ref["kind"] = "pull_request"
	deny("wrong kind", &pluginsdk.AuthorizeEntityReferenceRequest{WorkspaceID: "ws-1", Purpose: "search", Reference: ref})
	ref = allowedRef()
	ref["scope"] = "ws-other"
	deny("scope mismatch", &pluginsdk.AuthorizeEntityReferenceRequest{WorkspaceID: "ws-1", Purpose: "search", Reference: ref})
	ref = allowedRef()
	ref["id"] = " 2-1 "
	deny("untrimmed id", &pluginsdk.AuthorizeEntityReferenceRequest{WorkspaceID: "ws-1", Purpose: "search", Reference: ref})
	ref = allowedRef()
	ref["key"] = ""
	deny("empty key", &pluginsdk.AuthorizeEntityReferenceRequest{WorkspaceID: "ws-1", Purpose: "search", Reference: ref})
	ref = allowedRef()
	ref["url"] = "https://evil.example.com/issue/FPU-1"
	deny("foreign instance URL", &pluginsdk.AuthorizeEntityReferenceRequest{WorkspaceID: "ws-1", Purpose: "search", Reference: ref})
	ref = allowedRef()
	ref["url"] = "javascript:alert(1)"
	deny("non-http URL", &pluginsdk.AuthorizeEntityReferenceRequest{WorkspaceID: "ws-1", Purpose: "search", Reference: ref})
	deny("unconfigured workspace", &pluginsdk.AuthorizeEntityReferenceRequest{WorkspaceID: "ws-none", Purpose: "search", Reference: allowedRef()})
}

func TestAuthorizeEntityReference_AcceptsOwnCandidates(t *testing.T) {
	// End-to-end: every candidate the searcher emits must pass the authorizer,
	// or the host's mention bridge would drop it from the composer results.
	srv := newSearchIssuesServer(t)
	p := newTestPlugin(t, srv.URL)
	ctx := context.Background()

	search, err := p.SearchEntityReferences(ctx, &pluginsdk.SearchEntityReferencesRequest{
		WorkspaceID: "ws-1", Query: "crash", Limit: 5,
	})
	if err != nil {
		t.Fatalf("SearchEntityReferences: %v", err)
	}
	if len(search.Candidates) == 0 {
		t.Fatal("expected candidates")
	}
	for _, candidate := range search.Candidates {
		resp, err := p.AuthorizeEntityReference(ctx, &pluginsdk.AuthorizeEntityReferenceRequest{
			WorkspaceID: "ws-1",
			Purpose:     "search",
			Reference: map[string]any{
				"provider": "youtrack", "kind": "issue",
				"id": candidate.ProviderLocalID, "key": candidate.ProviderLocalID,
				"title": candidate.Title, "url": candidate.URL, "scope": "ws-1",
			},
		})
		if err != nil {
			t.Fatalf("AuthorizeEntityReference: %v", err)
		}
		if !resp.Allowed {
			t.Errorf("own candidate rejected: %+v (%s)", candidate, resp.Reason)
		}
	}
}

func TestReferenceURLPointsAtBase(t *testing.T) {
	cases := []struct {
		refURL, baseURL string
		want            bool
	}{
		{refURL: "https://yt.example.com/issue/FPU-1", baseURL: "https://yt.example.com", want: true},
		{refURL: "https://yt.example.com/issue/FPU-1", baseURL: "https://yt.example.com/", want: true},
		{refURL: "https://YT.example.com/issue/FPU-1", baseURL: "yt.example.com", want: true},
		{refURL: "https://yt.example.com/issue/FPU-1", baseURL: "https://other.example.com", want: false},
		{refURL: "http://yt.example.com/issue/FPU-1", baseURL: "https://yt.example.com", want: false},
		{refURL: "javascript:alert(1)", baseURL: "https://yt.example.com", want: false},
		{refURL: "https://user@yt.example.com/issue/FPU-1", baseURL: "https://yt.example.com", want: false},
		{refURL: "", baseURL: "https://yt.example.com", want: false},
		{refURL: "https://yt.example.com/issue/FPU-1", baseURL: "", want: false},
	}
	for _, tc := range cases {
		if got := referenceURLPointsAtBase(tc.refURL, tc.baseURL); got != tc.want {
			t.Errorf("referenceURLPointsAtBase(%q, %q) = %v, want %v", tc.refURL, tc.baseURL, got, tc.want)
		}
	}
}

// newSearchIssuesServer builds a fake YouTrack API returning two issues for
// any /api/issues request.
func newSearchIssuesServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/issues", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issues":[{"id":"2-1","idReadable":"FPU-1","summary":"first crash"},{"id":"2-2","idReadable":"FPU-2","summary":"second crash"}],"hasAfter":false}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}
