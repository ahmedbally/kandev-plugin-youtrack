package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kandev/kandev/pkg/pluginsdk"
)

// ErrWatchNotFound mirrors Jira's ErrIssueWatchNotFound: the requested watch
// ID does not exist in the workspace.
var ErrWatchNotFound = errors.New("youtrack: watch not found")

// errResetUnavailable mirrors Jira's "task deleter not wired" error: the host
// does not expose the provenance-safe task-tree manager this plugin needs to
// cascade-delete watch-created tasks.
var errResetUnavailable = errors.New("youtrack: host does not expose plugin-owned task trees; reset unavailable")

func findWatch(watches []Watch, id string) (Watch, bool) {
	for _, w := range watches {
		if w.ID == id {
			return w, true
		}
	}
	return Watch{}, false
}

// previewResetWatch returns how many tasks resetting the watch would cascade-
// delete. Mirrors Jira's PreviewResetIssueWatch, used to populate the
// confirmation dialog.
func previewResetWatch(ctx context.Context, host pluginsdk.Host, wsID, watchID string) (int, error) {
	watches, err := loadWatches(ctx, host, wsID)
	if err != nil {
		return 0, err
	}
	if _, ok := findWatch(watches, watchID); !ok {
		return 0, fmt.Errorf("%w: %s", ErrWatchNotFound, watchID)
	}
	taskBundle := loadWatchTasks(ctx, host, wsID)
	existing := 0
	for _, id := range taskBundle[watchID] {
		if t, err := host.Tasks().Get(ctx, id); err == nil && t != nil {
			existing++
		}
	}
	return existing, nil
}

// resetWatch is destructive: cascade-deletes every task previously created by
// the watch, wipes its per-watch dedup state, and clears its tick so the next
// poll re-imports every currently-matching issue. Mirrors Jira's
// ResetIssueWatch. Returns the number of tasks deleted.
func resetWatch(ctx context.Context, p *Plugin, wsID, watchID string) (int, error) {
	host := p.Host()
	watches, err := loadWatches(ctx, host, wsID)
	if err != nil {
		return 0, err
	}
	if _, ok := findWatch(watches, watchID); !ok {
		return 0, fmt.Errorf("%w: %s", ErrWatchNotFound, watchID)
	}
	trees, ok := pluginsdk.PluginOwnedTaskTrees(host)
	if !ok {
		return 0, errResetUnavailable
	}
	taskBundle := loadWatchTasks(ctx, host, wsID)
	deleted := 0
	for _, id := range taskBundle[watchID] {
		// Delete treats an absent root as an idempotent success. On partial
		// failure the ids removed before the error still come back, so the
		// count stays truthful and the dedup wipe below stays consistent.
		ids, delErr := trees.Delete(ctx, id)
		deleted += len(ids)
		if delErr != nil {
			return deleted, fmt.Errorf("delete watch task tree %s: %w", id, delErr)
		}
	}
	pruneWatchDedupState(ctx, host, wsID, watchID)
	return deleted, nil
}

// pruneWatchDedupState removes a watch's tick, created-task list, and the
// seen-issue entries pointing at those tasks. This is the plugin-state
// counterpart of Jira's cascade delete of dedup rows; without it a deleted
// watch would leave orphaned entries in the workspace-level bundles forever.
func pruneWatchDedupState(ctx context.Context, host pluginsdk.Host, wsID, watchID string) {
	taskBundle := loadWatchTasks(ctx, host, wsID)
	if removed, ok := taskBundle[watchID]; ok && len(removed) > 0 {
		taskIDs := make(map[string]bool, len(removed))
		for _, id := range removed {
			taskIDs[id] = true
		}
		seen := loadSeen(ctx, host, wsID)
		for issueID, taskID := range seen {
			if taskIDs[taskID] {
				delete(seen, issueID)
			}
		}
		_ = saveSeen(ctx, host, wsID, seen)
	}
	delete(taskBundle, watchID)
	_ = saveWatchTasks(ctx, host, wsID, taskBundle)
	ticks := loadTicks(ctx, host, wsID)
	if _, ok := ticks[watchID]; ok {
		delete(ticks, watchID)
		_ = saveTicks(ctx, host, wsID, ticks)
	}
}

func (p *Plugin) actionWatchesResetPreview(ctx context.Context, wsID string, req *pluginsdk.PluginActionRequest) (*pluginsdk.PluginActionResponse, error) {
	var body watchIDBody
	if err := json.Unmarshal(req.Body, &body); err != nil {
		return errorResp(400, "invalid body: "+err.Error())
	}
	if body.ID == "" {
		return errorResp(400, "id is required")
	}
	if p.Host() == nil {
		return errorResp(500, "host unavailable")
	}
	count, err := previewResetWatch(ctx, p.Host(), wsID, body.ID)
	if err != nil {
		return watchStateErrorResponse(err)
	}
	return jsonResp(map[string]any{"task_count": count})
}

func (p *Plugin) actionWatchesReset(ctx context.Context, wsID string, req *pluginsdk.PluginActionRequest) (*pluginsdk.PluginActionResponse, error) {
	var body watchIDBody
	if err := json.Unmarshal(req.Body, &body); err != nil {
		return errorResp(400, "invalid body: "+err.Error())
	}
	if body.ID == "" {
		return errorResp(400, "id is required")
	}
	if p.Host() == nil {
		return errorResp(500, "host unavailable")
	}
	deleted, err := resetWatch(ctx, p, wsID, body.ID)
	if err != nil {
		return watchStateErrorResponse(err)
	}
	return jsonResp(map[string]any{"deleted_tasks": deleted})
}

// watchStateErrorResponse maps watch-state errors onto HTTP semantics:
// 404 for a missing watch, 501 when the host can't cascade-delete, 500 for
// anything else.
func watchStateErrorResponse(err error) (*pluginsdk.PluginActionResponse, error) {
	switch {
	case errors.Is(err, ErrWatchNotFound):
		return errorResp(404, err.Error())
	case errors.Is(err, errResetUnavailable):
		return errorResp(501, err.Error())
	default:
		return errorResp(500, err.Error())
	}
}
