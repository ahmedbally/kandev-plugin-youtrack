package plugin

import (
	"testing"

	pluginsdk "github.com/kandev/kandev/pkg/pluginsdk"
)

func TestHandleAction_WatchesCreate_FillsDefaultBranch(t *testing.T) {
	p := newTestPlugin(t, "https://example.test")
	host := p.Host().(*fakeHost)
	host.repos = []pluginsdk.Repository{
		{ID: "repo-1", Name: "Main repo", DefaultBranch: strPtr("develop")},
	}

	resp := actionCall(t, p, "watches.create", "ws-1", watchSaveBody{
		Query:        "  for: me  ",
		RepositoryID: "repo-1",
	})
	var out struct {
		Watch map[string]any `json:"watch"`
	}
	decodeAction(t, resp, &out)
	if out.Watch["repository_id"] != "repo-1" {
		t.Errorf("repository_id = %v", out.Watch["repository_id"])
	}
	if out.Watch["base_branch"] != "develop" {
		t.Errorf("base_branch = %v, want default branch filled", out.Watch["base_branch"])
	}
	if out.Watch["query"] != "for: me" {
		t.Errorf("query = %v, want trimmed", out.Watch["query"])
	}
}

func TestHandleAction_WatchesCreate_RejectsUnknownRepository(t *testing.T) {
	p := newTestPlugin(t, "https://example.test")
	host := p.Host().(*fakeHost)
	host.repos = nil

	resp := actionCall(t, p, "watches.create", "ws-1", watchSaveBody{
		Query:        "for: me",
		RepositoryID: "repo-x",
	})
	if resp.Status != 400 {
		t.Errorf("status = %d, want 400", resp.Status)
	}
}

func TestHandleAction_WatchesCreate_RejectsNegativeCap(t *testing.T) {
	p := newTestPlugin(t, "https://example.test")
	resp := actionCall(t, p, "watches.create", "ws-1", watchSaveBody{
		Query:       "for: me",
		MaxInflight: intPtr(-1),
	})
	if resp.Status != 400 {
		t.Errorf("status = %d, want 400", resp.Status)
	}
}

func TestHandleAction_WatchesUpdate_RejectsEmptyQuery(t *testing.T) {
	p := newTestPlugin(t, "https://example.test")
	host := p.Host().(*fakeHost)
	seedWatchState(t, host, "ws-1", Watch{ID: "w-1", Query: "for: me", Enabled: true, IntervalSeconds: 300})

	resp := actionCall(t, p, "watches.update", "ws-1", map[string]any{"id": "w-1", "query": ""})
	if resp.Status != 400 {
		t.Errorf("status = %d, want 400", resp.Status)
	}
}

func TestHandleAction_WatchesUpdate_ResetsBranchOnRepoChange(t *testing.T) {
	p := newTestPlugin(t, "https://example.test")
	host := p.Host().(*fakeHost)
	host.repos = []pluginsdk.Repository{
		{ID: "repo-b", Name: "Other repo", DefaultBranch: strPtr("develop")},
	}
	seedWatchState(t, host, "ws-1", Watch{
		ID: "w-1", Query: "for: me", Enabled: true, IntervalSeconds: 300,
		RepositoryID: "repo-a", BaseBranch: "old-branch",
	})

	resp := actionCall(t, p, "watches.update", "ws-1", map[string]any{"id": "w-1", "repository_id": "repo-b"})
	var out struct {
		Watch map[string]any `json:"watch"`
	}
	decodeAction(t, resp, &out)
	if out.Watch["repository_id"] != "repo-b" {
		t.Fatalf("repository_id = %v", out.Watch["repository_id"])
	}
	if out.Watch["base_branch"] != "develop" {
		t.Errorf("base_branch = %v, want old repo's branch replaced by the new repo's default", out.Watch["base_branch"])
	}
}

func TestHandleAction_WatchesUpdate_KeepsStaleBindingOnUnrelatedEdit(t *testing.T) {
	// Jira parity: an unchanged binding whose repository has since been
	// deleted must not block edits to other fields.
	p := newTestPlugin(t, "https://example.test")
	host := p.Host().(*fakeHost)
	host.repos = nil
	seedWatchState(t, host, "ws-1", Watch{
		ID: "w-1", Query: "for: me", Enabled: true, IntervalSeconds: 300,
		RepositoryID: "repo-deleted", BaseBranch: "main",
	})

	resp := actionCall(t, p, "watches.update", "ws-1", map[string]any{"id": "w-1", "prompt": "new prompt"})
	var out struct {
		Watch map[string]any `json:"watch"`
	}
	decodeAction(t, resp, &out)
	if out.Watch["prompt"] != "new prompt" {
		t.Errorf("prompt = %v, want updated", out.Watch["prompt"])
	}
	if out.Watch["repository_id"] != "repo-deleted" {
		t.Errorf("repository_id = %v, want unchanged", out.Watch["repository_id"])
	}
}

func TestHandleAction_WatchesUpdate_ClearsBindingOnEmptyRepo(t *testing.T) {
	p := newTestPlugin(t, "https://example.test")
	host := p.Host().(*fakeHost)
	seedWatchState(t, host, "ws-1", Watch{
		ID: "w-1", Query: "for: me", Enabled: true, IntervalSeconds: 300,
		RepositoryID: "repo-a", BaseBranch: "main",
	})

	resp := actionCall(t, p, "watches.update", "ws-1", map[string]any{"id": "w-1", "repository_id": ""})
	var out struct {
		Watch map[string]any `json:"watch"`
	}
	decodeAction(t, resp, &out)
	if out.Watch["repository_id"] != "" || out.Watch["base_branch"] != "" {
		t.Errorf("binding = %v/%v, want cleared", out.Watch["repository_id"], out.Watch["base_branch"])
	}
}

func TestHandleAction_WatchesUpdate_RejectsNegativeCap(t *testing.T) {
	p := newTestPlugin(t, "https://example.test")
	host := p.Host().(*fakeHost)
	seedWatchState(t, host, "ws-1", Watch{ID: "w-1", Query: "for: me", Enabled: true, IntervalSeconds: 300})

	resp := actionCall(t, p, "watches.update", "ws-1", map[string]any{"id": "w-1", "max_inflight": -5})
	if resp.Status != 400 {
		t.Errorf("status = %d, want 400", resp.Status)
	}
}

func intPtr(i int) *int { return &i }

func TestParseWebhookIssues_DedupesAndDropsIDless(t *testing.T) {
	payload := `{"issue":{"id":"2-1","summary":"a"},"changes":[{"issue":{"id":"2-1","summary":"a"}},{"issue":{"id":"","summary":"no id"}}],"issues":[{"id":"2-2","summary":"b"}]}`
	issues, err := parseWebhookIssues([]byte(payload))
	if err != nil {
		t.Fatalf("parseWebhookIssues: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("issues = %d, want 2", len(issues))
	}
	if issues[0].ID != "2-1" || issues[1].ID != "2-2" {
		t.Errorf("issues = %+v", issues)
	}
}
