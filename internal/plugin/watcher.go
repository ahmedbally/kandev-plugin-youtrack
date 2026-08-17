package plugin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kandev/kandev/pkg/pluginsdk"
	"github.com/kdlbs/kandev-plugin-youtrack/internal/youtrack"
)

// Watch is one saved query watcher, mirroring Jira's IssueWatch surface: the
// plugin polls YouTrack on a schedule and creates a Kandev task for every
// newly-matching issue, optionally bound to a repository/workflow/agent.
type Watch struct {
	ID                string `json:"id"`
	Query             string `json:"query"`
	Enabled           bool   `json:"enabled"`
	IntervalSeconds   int    `json:"interval_seconds"`
	Prompt            string `json:"prompt"`
	WorkflowID        string `json:"workflow_id"`
	WorkflowStepID    string `json:"workflow_step_id"`
	RepositoryID      string `json:"repository_id"`
	BaseBranch        string `json:"base_branch"`
	AgentProfileID    string `json:"agent_profile_id"`
	ExecutorProfileID string `json:"executor_profile_id"`
	MaxInflight       int    `json:"max_inflight"` // 0 = uncapped
	CreatedAt         string `json:"created_at"`
}

const (
	stateKeyWatches    = "youtrack_watches"
	stateKeyWatchTick  = "youtrack_watch_ticks"
	stateKeySeenIssues = "youtrack_seen_issues"
	stateKeyWatchTasks = "youtrack_watch_task_ids" // watch id -> [task ids]
	defaultWatchEvery  = 300
	minWatchEvery      = 60
	maxWatchEvery      = 3600
	pollerTickInterval = 30 * time.Second
	pollerFetchTop     = 50
	defaultMaxInflight = 5
)

type watchesBundle struct{ Watches []Watch }
type tickBundle map[string]int64
type seenBundle map[string]string     // issue id -> task id
type watchTasksBundle map[string][]string // watch id -> created task ids

func loadWatches(ctx context.Context, host pluginsdk.Host, wsID string) ([]Watch, error) {
	v, found, err := host.GetState(ctx, "workspace", wsID, stateKeyWatches)
	if err != nil || !found {
		return nil, err
	}
	var out []Watch
	if b, ok := v["watches"]; ok {
		list, _ := b.([]any)
		for _, item := range list {
			m, _ := item.(map[string]any)
			w := Watch{
				ID:                stringField(m, "id"),
				Query:             stringField(m, "query"),
				Enabled:           boolField(m, "enabled"),
				IntervalSeconds:   intField(m, "interval_seconds"),
				Prompt:            stringField(m, "prompt"),
				WorkflowID:        stringField(m, "workflow_id"),
				WorkflowStepID:    stringField(m, "workflow_step_id"),
				RepositoryID:      stringField(m, "repository_id"),
				BaseBranch:        stringField(m, "base_branch"),
				AgentProfileID:    stringField(m, "agent_profile_id"),
				ExecutorProfileID: stringField(m, "executor_profile_id"),
				MaxInflight:       intField(m, "max_inflight"),
				CreatedAt:         stringField(m, "created_at"),
			}
			if w.IntervalSeconds < minWatchEvery {
				w.IntervalSeconds = defaultWatchEvery
			}
			out = append(out, w)
		}
	}
	return out, nil
}

func saveWatches(ctx context.Context, host pluginsdk.Host, wsID string, watches []Watch) error {
	items := make([]any, len(watches))
	for i, w := range watches {
		items[i] = map[string]any{
			"id": w.ID, "query": w.Query, "enabled": w.Enabled,
			"interval_seconds": w.IntervalSeconds, "prompt": w.Prompt,
			"workflow_id": w.WorkflowID, "workflow_step_id": w.WorkflowStepID,
			"repository_id": w.RepositoryID, "base_branch": w.BaseBranch,
			"agent_profile_id": w.AgentProfileID, "executor_profile_id": w.ExecutorProfileID,
			"max_inflight": w.MaxInflight, "created_at": w.CreatedAt,
		}
	}
	return host.SetState(ctx, "workspace", wsID, stateKeyWatches, map[string]any{"watches": items})
}

func loadTicks(ctx context.Context, host pluginsdk.Host, wsID string) tickBundle {
	ticks := tickBundle{}
	v, found, err := host.GetState(ctx, "workspace", wsID, stateKeyWatchTick)
	if err != nil || !found {
		return ticks
	}
	for k, val := range v {
		if f, ok := val.(float64); ok {
			ticks[k] = int64(f)
		}
	}
	return ticks
}

func saveTicks(ctx context.Context, host pluginsdk.Host, wsID string, ticks tickBundle) {
	out := map[string]any{}
	for k, v := range ticks {
		out[k] = v
	}
	_ = host.SetState(ctx, "workspace", wsID, stateKeyWatchTick, out)
}

func loadSeen(ctx context.Context, host pluginsdk.Host, wsID string) seenBundle {
	seen := seenBundle{}
	v, found, err := host.GetState(ctx, "workspace", wsID, stateKeySeenIssues)
	if err != nil || !found {
		return seen
	}
	for k, val := range v {
		if s, ok := val.(string); ok {
			seen[k] = s
		}
	}
	return seen
}

func saveSeen(ctx context.Context, host pluginsdk.Host, wsID string, seen seenBundle) {
	out := map[string]any{}
	for k, v := range seen {
		out[k] = v
	}
	_ = host.SetState(ctx, "workspace", wsID, stateKeySeenIssues, out)
}

func loadWatchTasks(ctx context.Context, host pluginsdk.Host, wsID string) watchTasksBundle {
	bundle := watchTasksBundle{}
	v, found, err := host.GetState(ctx, "workspace", wsID, stateKeyWatchTasks)
	if err != nil || !found {
		return bundle
	}
	for watchID, val := range v {
		if arr, ok := val.([]any); ok {
			ids := make([]string, 0, len(arr))
			for _, item := range arr {
				if s, ok := item.(string); ok {
					ids = append(ids, s)
				}
			}
			bundle[watchID] = ids
		}
	}
	return bundle
}

func saveWatchTasks(ctx context.Context, host pluginsdk.Host, wsID string, bundle watchTasksBundle) {
	out := map[string]any{}
	for k, ids := range bundle {
		arr := make([]any, len(ids))
		for i, id := range ids {
			arr[i] = id
		}
		out[k] = arr
	}
	_ = host.SetState(ctx, "workspace", wsID, stateKeyWatchTasks, out)
}

// StartPoller launches the background watch loop; it waits for Host injection
// and then ticks every pollerTickInterval, polling every workspace's due watches.
func StartPoller(p *Plugin) {
	go func() {
		for {
			time.Sleep(pollerTickInterval)
			host := p.Host()
			if host == nil {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			pollAllWorkspaces(ctx, p)
			cancel()
		}
	}()
}

func pollAllWorkspaces(ctx context.Context, p *Plugin) {
	workspaces, _, err := p.Host().Workspaces().List(ctx, pluginsdk.Page{Limit: 100})
	if err != nil {
		return
	}
	for _, ws := range workspaces {
		pollWorkspace(ctx, p, ws.ID)
	}
}

func pollWorkspace(ctx context.Context, p *Plugin, wsID string) {
	host := p.Host()
	watches, err := loadWatches(ctx, host, wsID)
	if err != nil || len(watches) == 0 {
		return
	}
	ticks := loadTicks(ctx, host, wsID)
	now := time.Now().Unix()
	changed := false
	for _, w := range watches {
		if !w.Enabled {
			continue
		}
		if now-ticks[w.ID] < int64(w.IntervalSeconds) {
			continue
		}
		runWatch(ctx, p, wsID, w)
		ticks[w.ID] = time.Now().Unix()
		changed = true
	}
	if changed {
		saveTicks(ctx, host, wsID, ticks)
	}
}

// applyPromptPlaceholders substitutes {key}/{url}/{title}/{description} tokens.
func applyPromptPlaceholders(prompt string, issue *youtrack.Issue) string {
	return strings.NewReplacer(
		"{key}", issue.IDReadable,
		"{url}", issue.URL,
		"{title}", issue.Summary,
		"{description}", issue.Description,
	).Replace(prompt)
}

// runWatch executes one watch poll: search, dedup against seen issues, create
// tasks for new matches while respecting the inflight cap. Returns tasks made.
func runWatch(ctx context.Context, p *Plugin, wsID string, w Watch) int {
	host := p.Host()
	cfg, err := p.loadConfig(ctx, wsID)
	if err != nil {
		return 0
	}
	res, err := youtrack.New(cfg.BaseURL, cfg.Token).SearchIssues(ctx, w.Query, "", "", pollerFetchTop)
	if err != nil {
		return 0
	}
	seen := loadSeen(ctx, host, wsID)
	taskBundle := loadWatchTasks(ctx, host, wsID)

	maxOpen := w.MaxInflight
	if maxOpen < 0 {
		maxOpen = 0
	}
	created := 0
	for _, issue := range res.Issues {
		if _, dup := seen[issue.ID]; dup {
			continue
		}
		if maxOpen > 0 {
			open := 0
			for _, id := range taskBundle[w.ID] {
				if t, err := host.Tasks().Get(ctx, id); err == nil && t != nil &&
					t.State != "completed" && t.State != "archived" && t.State != "cancelled" {
					open++
				}
			}
			if open >= maxOpen {
				break // defer to next poll, exactly like Jira
			}
		}

		input := pluginsdk.CreateTaskInput{
			WorkspaceID: wsID,
			WorkflowID:  w.WorkflowID,
			Title:       fmt.Sprintf("%s: %s", issue.IDReadable, issue.Summary),
			Description: issue.Description,
			StartAgent:  true,
			Metadata: map[string]any{
				"youtrack_issue_id": issue.ID,
				"youtrack_url":      issue.URL,
				"youtrack_readable": issue.IDReadable,
				"youtrack_watch_id": w.ID,
			},
		}
		if w.WorkflowStepID != "" {
			step := w.WorkflowStepID
			input.WorkflowStepID = &step
		}
		if w.RepositoryID != "" {
			repo := pluginsdk.PluginTaskRepository{RepositoryID: w.RepositoryID}
			if w.BaseBranch != "" {
				b := w.BaseBranch
				repo.BaseBranch = &b
			}
			input.Repositories = []pluginsdk.PluginTaskRepository{repo}
		}
		launch := pluginsdk.PluginTaskLaunchOptions{}
		hasLaunch := false
		if w.AgentProfileID != "" {
			launch.AgentProfileID = &w.AgentProfileID
			hasLaunch = true
		}
		if w.ExecutorProfileID != "" {
			launch.ExecutorProfileID = &w.ExecutorProfileID
			hasLaunch = true
		}
		if w.Prompt != "" {
			prompt := applyPromptPlaceholders(w.Prompt, &issue)
			launch.Prompt = &prompt
			hasLaunch = true
		}
		if hasLaunch {
			input.Launch = &launch
		}

		task, err := host.Tasks().Create(ctx, input)
		if err != nil {
			continue
		}
		seen[issue.ID] = task.ID
		taskBundle[w.ID] = append(taskBundle[w.ID], task.ID)
		created++
	}
	if created > 0 {
		saveSeen(ctx, host, wsID, seen)
		saveWatchTasks(ctx, host, wsID, taskBundle)
	}
	return created
}

func runWatchByID(ctx context.Context, p *Plugin, wsID, watchID string) (int, error) {
	watches, err := loadWatches(ctx, p.Host(), wsID)
	if err != nil {
		return 0, err
	}
	for _, w := range watches {
		if w.ID == watchID {
			return runWatch(ctx, p, wsID, w), nil
		}
	}
	return 0, fmt.Errorf("watch %q not found", watchID)
}

func boolField(m map[string]any, key string) bool {
	v, _ := m[key].(bool)
	return v
}

func intField(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}