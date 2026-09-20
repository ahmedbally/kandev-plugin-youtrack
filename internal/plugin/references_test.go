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
