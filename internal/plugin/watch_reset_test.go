package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	pluginsdk "github.com/kandev/kandev/pkg/pluginsdk"
)

func seedWatchState(t *testing.T, host *fakeHost, wsID string, watches ...Watch) {
	t.Helper()
	if err := saveWatches(context.Background(), host, wsID, watches); err != nil {
		t.Fatalf("seed watches: %v", err)
	}
}

func actionCall(t *testing.T, p *Plugin, key, wsID string, body any) *pluginsdk.PluginActionResponse {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	resp, err := p.HandleAction(context.Background(), &pluginsdk.PluginActionRequest{
		ActionKey: key,
		Context:   pluginsdk.VerifiedActionContext{WorkspaceID: wsID},
		Body:      raw,
	})
	if err != nil {
		t.Fatalf("HandleAction(%s): %v", key, err)
	}
	return resp
}

func decodeAction(t *testing.T, resp *pluginsdk.PluginActionResponse, out any) {
	t.Helper()
	if err := json.Unmarshal(resp.Body, out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func strPtr(s string) *string { return &s }

func TestPreviewResetWatch_CountsExistingTasksOnly(t *testing.T) {
	p := newTestPlugin(t, "https://example.test")
	host := p.Host().(*fakeHost)
	seedWatchState(t, host, "ws-1", Watch{ID: "w-1", Query: "for: me", Enabled: true, IntervalSeconds: 300})
	if err := saveWatchTasks(context.Background(), host, "ws-1", watchTasksBundle{
		"w-1": []string{"task-1", "task-2", "task-gone"},
	}); err != nil {
		t.Fatalf("save bundle: %v", err)
	}
	host.tasks["task-1"] = &pluginsdk.Task{ID: "task-1"}
	host.tasks["task-2"] = &pluginsdk.Task{ID: "task-2"}

	count, err := previewResetWatch(context.Background(), host, "ws-1", "w-1")
	if err != nil {
		t.Fatalf("previewResetWatch: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2 (deleted tasks must not count)", count)
	}

	if _, err := previewResetWatch(context.Background(), host, "ws-1", "nope"); !errors.Is(err, ErrWatchNotFound) {
		t.Errorf("unknown watch err = %v, want ErrWatchNotFound", err)
	}
}

func TestResetWatch_DeletesTreesAndWipesDedupState(t *testing.T) {
	p := newTestPlugin(t, "https://example.test")
	host := p.Host().(*fakeHost)
	seedWatchState(t, host, "ws-1",
		Watch{ID: "w-1", Query: "for: me", Enabled: true, IntervalSeconds: 300},
		Watch{ID: "w-2", Query: "for: me", Enabled: true, IntervalSeconds: 300})
	if err := saveWatchTasks(context.Background(), host, "ws-1", watchTasksBundle{
		"w-1": []string{"task-1"},
		"w-2": []string{"task-9"},
	}); err != nil {
		t.Fatalf("save bundle: %v", err)
	}
	if err := saveSeen(context.Background(), host, "ws-1", seenBundle{"2-1": "task-1", "2-9": "task-9"}); err != nil {
		t.Fatalf("save seen: %v", err)
	}
	if err := saveTicks(context.Background(), host, "ws-1", tickBundle{"w-1": 100, "w-2": 200}); err != nil {
		t.Fatalf("save ticks: %v", err)
	}
	host.tasks["task-1"] = &pluginsdk.Task{ID: "task-1"}

	deleted, err := resetWatch(context.Background(), p, "ws-1", "w-1")
	if err != nil {
		t.Fatalf("resetWatch: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}
	if len(host.deletedTrees) != 1 || host.deletedTrees[0] != "task-1" {
		t.Errorf("deletedTrees = %v, want [task-1]", host.deletedTrees)
	}

	bundle := loadWatchTasks(context.Background(), host, "ws-1")
	if _, ok := bundle["w-1"]; ok {
		t.Error("w-1 task bundle should be wiped")
	}
	if _, ok := bundle["w-2"]; !ok {
		t.Error("w-2 task bundle should remain")
	}
	seen := loadSeen(context.Background(), host, "ws-1")
	if _, ok := seen["2-1"]; ok {
		t.Error("seen entry pointing at a w-1 task should be wiped")
	}
	if seen["2-9"] != "task-9" {
		t.Errorf("seen entry for w-2 should remain, got %v", seen)
	}
	ticks := loadTicks(context.Background(), host, "ws-1")
	if _, ok := ticks["w-1"]; ok {
		t.Error("w-1 tick should be wiped")
	}
	if ticks["w-2"] != 200 {
		t.Errorf("w-2 tick should remain, got %v", ticks)
	}
}

func TestHandleAction_WatchesReset_MissingID(t *testing.T) {
	p := newTestPlugin(t, "https://example.test")
	resp := actionCall(t, p, "watches.reset", "ws-1", map[string]any{})
	if resp.Status != 400 {
		t.Errorf("status = %d, want 400", resp.Status)
	}
}

func TestHandleAction_WatchesReset_NotFound(t *testing.T) {
	p := newTestPlugin(t, "https://example.test")
	resp := actionCall(t, p, "watches.reset", "ws-1", map[string]any{"id": "nope"})
	if resp.Status != 404 {
		t.Errorf("status = %d, want 404", resp.Status)
	}
}

func TestHandleAction_WatchesResetPreview(t *testing.T) {
	p := newTestPlugin(t, "https://example.test")
	host := p.Host().(*fakeHost)
	seedWatchState(t, host, "ws-1", Watch{ID: "w-1", Query: "for: me", Enabled: true, IntervalSeconds: 300})
	if err := saveWatchTasks(context.Background(), host, "ws-1", watchTasksBundle{"w-1": []string{"task-1"}}); err != nil {
		t.Fatalf("save bundle: %v", err)
	}
	host.tasks["task-1"] = &pluginsdk.Task{ID: "task-1"}

	resp := actionCall(t, p, "watches.reset_preview", "ws-1", map[string]any{"id": "w-1"})
	var out struct {
		TaskCount int `json:"task_count"`
	}
	decodeAction(t, resp, &out)
	if out.TaskCount != 1 {
		t.Errorf("task_count = %d, want 1", out.TaskCount)
	}
}

func TestHandleAction_WatchesDelete_PrunesDedupState(t *testing.T) {
	p := newTestPlugin(t, "https://example.test")
	host := p.Host().(*fakeHost)
	seedWatchState(t, host, "ws-1",
		Watch{ID: "w-1", Query: "for: me", Enabled: true, IntervalSeconds: 300},
		Watch{ID: "w-2", Query: "for: me", Enabled: true, IntervalSeconds: 300})
	if err := saveWatchTasks(context.Background(), host, "ws-1", watchTasksBundle{
		"w-1": []string{"task-1"},
		"w-2": []string{"task-9"},
	}); err != nil {
		t.Fatalf("save bundle: %v", err)
	}
	if err := saveSeen(context.Background(), host, "ws-1", seenBundle{"2-1": "task-1", "2-9": "task-9"}); err != nil {
		t.Fatalf("save seen: %v", err)
	}

	resp := actionCall(t, p, "watches.delete", "ws-1", map[string]any{"id": "w-1"})
	if resp.Status != 0 {
		t.Errorf("status = %d, want success", resp.Status)
	}

	seen := loadSeen(context.Background(), host, "ws-1")
	if _, ok := seen["2-1"]; ok {
		t.Error("w-1 seen entry should be pruned on delete")
	}
	if seen["2-9"] != "task-9" {
		t.Error("w-2 seen entry should survive the w-1 delete")
	}
	bundle := loadWatchTasks(context.Background(), host, "ws-1")
	if _, ok := bundle["w-1"]; ok {
		t.Error("w-1 task bundle should be pruned on delete")
	}
}

func TestHandleAction_ConnectionCopy(t *testing.T) {
	p := newTestPlugin(t, "https://youtrack.test")
	host := p.Host().(*fakeHost)

	resp := actionCall(t, p, "connection.copy", "ws-1", map[string]any{"target_workspace_id": "ws-2"})
	var out struct {
		Copied  bool   `json:"copied"`
		BaseURL string `json:"base_url"`
	}
	decodeAction(t, resp, &out)
	if !out.Copied {
		t.Fatal("expected copied=true")
	}
	cfg := host.state["ws-2"][stateKey]
	if cfg["base_url"] != "https://youtrack.test" {
		t.Errorf("target base_url = %v", cfg["base_url"])
	}
	if host.secrets[secretKey("ws-2")] != "test-token" {
		t.Errorf("target token = %v", host.secrets[secretKey("ws-2")])
	}
}

func TestHandleAction_ConnectionCopy_Rejections(t *testing.T) {
	p := newTestPlugin(t, "https://youtrack.test")

	// Same workspace.
	if resp := actionCall(t, p, "connection.copy", "ws-1", map[string]any{"target_workspace_id": "ws-1"}); resp.Status != 400 {
		t.Errorf("same-workspace status = %d, want 400", resp.Status)
	}
	// Missing target.
	if resp := actionCall(t, p, "connection.copy", "ws-1", map[string]any{}); resp.Status != 400 {
		t.Errorf("missing-target status = %d, want 400", resp.Status)
	}
	// Source without configuration.
	if resp := actionCall(t, p, "connection.copy", "ws-empty", map[string]any{"target_workspace_id": "ws-2"}); resp.Status != 400 {
		t.Errorf("unconfigured-source status = %d, want 400", resp.Status)
	}
	// Source with config but no token.
	p2 := newTestPlugin(t, "https://youtrack.test")
	host := p2.Host().(*fakeHost)
	delete(host.secrets, secretKey("ws-1"))
	if resp := actionCall(t, p2, "connection.copy", "ws-1", map[string]any{"target_workspace_id": "ws-2"}); resp.Status != 400 {
		t.Errorf("tokenless-source status = %d, want 400", resp.Status)
	}
}
