package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kandev/kandev/pkg/pluginsdk"
	"github.com/kdlbs/kandev-plugin-youtrack/internal/youtrack"
)

func (p *Plugin) HandleAction(ctx context.Context, req *pluginsdk.PluginActionRequest) (*pluginsdk.PluginActionResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("youtrack plugin: missing action request")
	}
	wsID := req.Context.WorkspaceID
	if wsID == "" {
		var body map[string]any
		_ = json.Unmarshal(req.Body, &body)
		if v, ok := body["workspaceId"].(string); ok && v != "" {
			wsID = v
		}
	}
	switch req.ActionKey {
	case "connection.check":
		return p.actionConnectionCheck(ctx, wsID, req)
	case "connection.save":
		return p.actionConnectionSave(ctx, wsID, req)
	case "connection.delete":
		return p.actionConnectionDelete(ctx, wsID)
	case "connection.copy":
		return p.actionConnectionCopy(ctx, wsID, req)
	case "projects.list":
		return p.actionProjectsList(ctx, wsID)
	case "issues.list":
		return p.actionListIssues(ctx, wsID, req)
	case "issues.create_task":
		return p.actionCreateTask(ctx, wsID, req)
	case "issues.link":
		return p.actionLinkIssue(ctx, req)
	case "issue.change_state":
		return p.actionChangeState(ctx, wsID, req)
	case "watches.list":
		return p.actionWatchesList(ctx, wsID)
	case "watches.create":
		return p.actionWatchesCreate(ctx, wsID, req)
	case "watches.update":
		return p.actionWatchesUpdate(ctx, wsID, req)
	case "watches.delete":
		return p.actionWatchesDelete(ctx, wsID, req)
	case "watches.trigger":
		return p.actionWatchesTrigger(ctx, wsID, req)
	case "watches.reset_preview":
		return p.actionWatchesResetPreview(ctx, wsID, req)
	case "watches.reset":
		return p.actionWatchesReset(ctx, wsID, req)
	case "context.options":
		return p.actionContextOptions(ctx, wsID)
	default:
		return nil, fmt.Errorf("youtrack plugin: unknown action %q", req.ActionKey)
	}
}

type connectionSaveBody struct {
	BaseURL        string `json:"base_url"`
	PermanentToken string `json:"permanent_token"`
	DefaultProject string `json:"default_project"`
	DefaultQuery   string `json:"default_query"`
}

func (p *Plugin) actionConnectionSave(ctx context.Context, wsID string, req *pluginsdk.PluginActionRequest) (*pluginsdk.PluginActionResponse, error) {
	var body connectionSaveBody
	if err := json.Unmarshal(req.Body, &body); err != nil {
		return errorResp(400, "invalid body: "+err.Error())
	}
	if wsID == "" {
		return errorResp(400, "workspaceId is required")
	}
	if body.BaseURL == "" {
		return errorResp(400, "base_url is required")
	}
	host := p.Host()
	if host == nil {
		return errorResp(500, "host unavailable")
	}
	if err := host.SetState(ctx, "workspace", wsID, stateKey, map[string]any{
		"base_url":        body.BaseURL,
		"default_project": body.DefaultProject,
		"default_query":   body.DefaultQuery,
	}); err != nil {
		return errorRespFromAPI("save youtrack config", err)
	}
	if body.PermanentToken != "" && body.PermanentToken != "********" {
		if err := host.SetSecret(ctx, secretKey(wsID), body.PermanentToken); err != nil {
			return errorRespFromAPI("save youtrack token", err)
		}
	}
	return jsonResp(map[string]any{"saved": true})
}

// actionConnectionDelete removes the workspace configuration, secret, and
// every watch. Mirrors Jira's "Remove configuration".
func (p *Plugin) actionConnectionDelete(ctx context.Context, wsID string) (*pluginsdk.PluginActionResponse, error) {
	host := p.Host()
	if host == nil {
		return errorResp(500, "host unavailable")
	}
	if err := host.DeleteState(ctx, "workspace", wsID, stateKey); err != nil {
		return errorRespFromAPI("remove youtrack config", err)
	}
	if err := host.DeleteSecret(ctx, secretKey(wsID)); err != nil {
		return errorRespFromAPI("remove youtrack token", err)
	}
	_ = host.DeleteState(ctx, "workspace", wsID, stateKeyWatches)
	_ = host.DeleteState(ctx, "workspace", wsID, stateKeyWatchTick)
	_ = host.DeleteState(ctx, "workspace", wsID, stateKeySeenIssues)
	_ = host.DeleteState(ctx, "workspace", wsID, stateKeyWatchTasks)
	return jsonResp(map[string]any{"deleted": true})
}

// connectionCopyBody is the payload for connection.copy: the destination
// workspace. The source is always the authorized caller's workspace (the
// workspaceId on the request context), mirroring how Jira authorizes the
// source via middleware and still validates the target from the body.
type connectionCopyBody struct {
	TargetWorkspaceID string `json:"target_workspace_id"`
}

// actionConnectionCopy duplicates the YouTrack connection settings and token
// from the source workspace to a target workspace. Mirrors Jira's
// CopyConfigToWorkspace: same-workspace copies and source workspaces without
// a complete configuration are rejected; watchers are intentionally out of
// scope — only the connection settings and secret are copied.
func (p *Plugin) actionConnectionCopy(ctx context.Context, sourceWSID string, req *pluginsdk.PluginActionRequest) (*pluginsdk.PluginActionResponse, error) {
	var body connectionCopyBody
	if err := json.Unmarshal(req.Body, &body); err != nil {
		return errorResp(400, "invalid body: "+err.Error())
	}
	target := strings.TrimSpace(body.TargetWorkspaceID)
	if target == "" {
		return errorResp(400, "target_workspace_id is required")
	}
	if target == sourceWSID {
		return errorResp(400, "source and target workspaces are the same")
	}
	host := p.Host()
	if host == nil {
		return errorResp(500, "host unavailable")
	}
	value, found, err := host.GetState(ctx, "workspace", sourceWSID, stateKey)
	if err != nil {
		return errorRespFromAPI("read source youtrack config", err)
	}
	if !found || stringField(value, "base_url") == "" {
		return errorResp(400, "source workspace has no configuration to copy")
	}
	token, tokenFound, err := host.GetSecret(ctx, secretKey(sourceWSID))
	if err != nil {
		return errorRespFromAPI("read source youtrack token", err)
	}
	if !tokenFound || token == "" {
		// Copying a config without its token would leave the target pairing
		// the copied base URL with its own old credential — treat as nothing
		// to copy, exactly like Jira.
		return errorResp(400, "source workspace has no stored token to copy")
	}
	targetState := map[string]any{
		"base_url":        stringField(value, "base_url"),
		"default_project": stringField(value, "default_project"),
		"default_query":   stringField(value, "default_query"),
	}
	if err := host.SetState(ctx, "workspace", target, stateKey, targetState); err != nil {
		return errorRespFromAPI("save target youtrack config", err)
	}
	if err := host.SetSecret(ctx, secretKey(target), token); err != nil {
		return errorRespFromAPI("save target youtrack token", err)
	}
	return jsonResp(map[string]any{"copied": true, "base_url": targetState["base_url"]})
}

func (p *Plugin) actionProjectsList(ctx context.Context, wsID string) (*pluginsdk.PluginActionResponse, error) {
	client, err := p.client(ctx, wsID)
	if err != nil {
		return errorRespFromAPI("youtrack not configured", err)
	}
	projects, err := client.ListProjects(ctx)
	if err != nil {
		return errorRespFromAPI("list youtrack projects", err)
	}
	return jsonResp(map[string]any{"projects": projects})
}

func (p *Plugin) actionConnectionCheck(ctx context.Context, wsID string, _ *pluginsdk.PluginActionRequest) (*pluginsdk.PluginActionResponse, error) {
	cfg, err := p.loadConfig(ctx, wsID)
	if err != nil {
		return jsonResp(map[string]any{
			"connected": false, "error": err.Error(),
			"base_url": "", "default_project": "", "default_query": "", "has_token": false,
		})
	}
	client := youtrack.New(cfg.BaseURL, cfg.Token)
	login, err := client.Ping(ctx)
	if err != nil {
		return jsonResp(map[string]any{
			"connected": false, "error": err.Error(),
			"base_url": cfg.BaseURL, "default_project": cfg.DefaultProject,
			"default_query": cfg.DefaultQuery, "has_token": true,
		})
	}
	return jsonResp(map[string]any{
		"connected": true, "login": login,
		"base_url": cfg.BaseURL, "default_project": cfg.DefaultProject,
		"default_query": cfg.DefaultQuery, "has_token": true,
	})
}

type listIssuesBody struct {
	Query   string `json:"query"`
	Project string `json:"project"`
	Cursor  string `json:"cursor"`
	Top     int    `json:"top"`
}

func (p *Plugin) actionListIssues(ctx context.Context, wsID string, req *pluginsdk.PluginActionRequest) (*pluginsdk.PluginActionResponse, error) {
	client, err := p.client(ctx, wsID)
	if err != nil {
		return errorRespFromAPI("youtrack not configured for this workspace", err)
	}
	var body listIssuesBody
	if err := json.Unmarshal(req.Body, &body); err != nil {
		return nil, fmt.Errorf("decode issues.list body: %w", err)
	}
	cfg, _ := p.loadConfig(ctx, wsID)
	if body.Query == "" {
		body.Query = cfg.DefaultQuery
	}
	res, err := client.SearchIssues(ctx, body.Query, body.Project, body.Cursor, body.Top)
	if err != nil {
		return nil, err
	}
	return jsonResp(map[string]any{
		"issues":      res.Issues,
		"next_cursor": res.NextCursor,
		"has_more":    res.HasMore,
	})
}

type createTaskBody struct {
	IssueID     string `json:"issue_id"`
	WorkflowID  string `json:"workflow_id"`
	StartAgent  bool   `json:"start_agent"`
	AgentPrompt string `json:"agent_prompt"`
}

func (p *Plugin) actionCreateTask(ctx context.Context, wsID string, req *pluginsdk.PluginActionRequest) (*pluginsdk.PluginActionResponse, error) {
	var body createTaskBody
	if err := json.Unmarshal(req.Body, &body); err != nil {
		return nil, fmt.Errorf("decode issues.create_task body: %w", err)
	}
	if body.IssueID == "" {
		return errorResp(400, "issue_id is required")
	}
	client, err := p.client(ctx, wsID)
	if err != nil {
		return errorRespFromAPI("youtrack not configured", err)
	}
	issue, err := client.GetIssue(ctx, body.IssueID)
	if err != nil {
		return errorRespFromAPI("fetch youtrack issue", err)
	}
	host := p.Host()
	if host == nil {
		return errorResp(500, "host unavailable")
	}
	launch := (*pluginsdk.PluginTaskLaunchOptions)(nil)
	if body.StartAgent && body.AgentPrompt != "" {
		prompt := body.AgentPrompt
		launch = &pluginsdk.PluginTaskLaunchOptions{Prompt: &prompt}
	}
	task, err := host.Tasks().Create(ctx, pluginsdk.CreateTaskInput{
		WorkspaceID: wsID,
		WorkflowID: body.WorkflowID,
		Title:       fmt.Sprintf("%s: %s", issue.IDReadable, issue.Summary),
		Description: issue.Description,
		StartAgent:  body.StartAgent,
		Launch:      launch,
		Metadata: map[string]any{
			"youtrack_issue_id": issue.ID,
			"youtrack_url":      issue.URL,
			"youtrack_readable": issue.IDReadable,
		},
	})
	if err != nil {
		return errorRespFromAPI("create task from youtrack issue", err)
	}
	return jsonResp(map[string]any{"task_id": task.ID, "issue": issue})
}

type linkIssueBody struct {
	IssueID string `json:"issue_id"`
}

func (p *Plugin) actionLinkIssue(ctx context.Context, req *pluginsdk.PluginActionRequest) (*pluginsdk.PluginActionResponse, error) {
	var body linkIssueBody
	if err := json.Unmarshal(req.Body, &body); err != nil {
		return nil, fmt.Errorf("decode issues.link body: %w", err)
	}
	if body.IssueID == "" {
		return errorResp(400, "issue_id is required")
	}
	if req.Context.TaskID == "" {
		return errorResp(400, "task id is required to link an issue")
	}
	client, err := p.client(ctx, req.Context.WorkspaceID)
	if err != nil {
		return errorRespFromAPI("youtrack not configured", err)
	}
	issue, err := client.GetIssue(ctx, body.IssueID)
	if err != nil {
		return errorRespFromAPI("fetch youtrack issue", err)
	}
	host := p.Host()
	if host == nil {
		return errorResp(500, "host unavailable")
	}
	if err := host.SetState(ctx, "task", req.Context.TaskID, "youtrack_link", map[string]any{
		"issue_id": issue.ID,
		"readable": issue.IDReadable,
		"url":      issue.URL,
		"summary":  issue.Summary,
	}); err != nil {
		return errorRespFromAPI("persist youtrack link", err)
	}
	return jsonResp(map[string]any{"linked": true, "issue": issue})
}

type changeStateBody struct {
	IssueID string `json:"issue_id"`
	State   string `json:"state"`
}

func (p *Plugin) actionChangeState(ctx context.Context, wsID string, req *pluginsdk.PluginActionRequest) (*pluginsdk.PluginActionResponse, error) {
	var body changeStateBody
	if err := json.Unmarshal(req.Body, &body); err != nil {
		return nil, fmt.Errorf("decode issue.change_state body: %w", err)
	}
	if body.IssueID == "" || body.State == "" {
		return errorResp(400, "issue_id and state are required")
	}
	client, err := p.client(ctx, wsID)
	if err != nil {
		return errorRespFromAPI("youtrack not configured", err)
	}
	patch := map[string]any{
		"customFields": []map[string]any{{
			"name":  "State",
			"value": map[string]any{"name": body.State},
		}},
	}
	if err := client.ApplyIssueCommand(ctx, body.IssueID, patch); err != nil {
		return errorRespFromAPI("change youtrack issue state", err)
	}
	return jsonResp(map[string]any{"changed": true, "state": body.State})
}

// ── Watches (issue watchers, Jira-style) ────────────────────────────────────

func (p *Plugin) actionWatchesList(ctx context.Context, wsID string) (*pluginsdk.PluginActionResponse, error) {
	watches, err := loadWatches(ctx, p.Host(), wsID)
	if err != nil {
		return errorRespFromAPI("load watches", err)
	}
	if watches == nil {
		watches = []Watch{}
	}
	return jsonResp(map[string]any{"watches": watches})
}

type watchSaveBody struct {
	Query             string `json:"query"`
	Enabled           *bool  `json:"enabled"`
	IntervalSeconds   int    `json:"interval_seconds"`
	Prompt            string `json:"prompt"`
	WorkflowID        string `json:"workflow_id"`
	WorkflowStepID    string `json:"workflow_step_id"`
	RepositoryID      string `json:"repository_id"`
	BaseBranch        string `json:"base_branch"`
	AgentProfileID    string `json:"agent_profile_id"`
	ExecutorProfileID string `json:"executor_profile_id"`
	MaxInflight       *int   `json:"max_inflight"`
}

func (b *watchSaveBody) normalizedInterval() int {
	iv := b.IntervalSeconds
	if iv < minWatchEvery {
		iv = defaultWatchEvery
	}
	if iv > maxWatchEvery {
		iv = maxWatchEvery
	}
	return iv
}

func watchToMap(w Watch) map[string]any {
	return map[string]any{
		"id": w.ID, "query": w.Query, "enabled": w.Enabled,
		"interval_seconds": w.IntervalSeconds, "prompt": w.Prompt,
		"workflow_id": w.WorkflowID, "workflow_step_id": w.WorkflowStepID,
		"repository_id": w.RepositoryID, "base_branch": w.BaseBranch,
		"agent_profile_id": w.AgentProfileID, "executor_profile_id": w.ExecutorProfileID,
		"max_inflight": w.MaxInflight, "created_at": w.CreatedAt,
	}
}

func (p *Plugin) actionWatchesCreate(ctx context.Context, wsID string, req *pluginsdk.PluginActionRequest) (*pluginsdk.PluginActionResponse, error) {
	var body watchSaveBody
	if err := json.Unmarshal(req.Body, &body); err != nil {
		return errorResp(400, "invalid body: "+err.Error())
	}
	if wsID == "" {
		return errorResp(400, "workspaceId is required")
	}
	if strings.TrimSpace(body.Query) == "" {
		return errorResp(400, "query is required")
	}
	if body.MaxInflight != nil && *body.MaxInflight < 0 {
		return errorResp(400, "max_inflight must be zero or a positive integer")
	}
	host := p.Host()
	if host == nil {
		return errorResp(500, "host unavailable")
	}
	repoID, branch, err := resolveRepositoryBinding(ctx, host, wsID, body.RepositoryID, body.BaseBranch)
	if err != nil {
		return errorResp(400, err.Error())
	}
	watches, err := loadWatches(ctx, host, wsID)
	if err != nil {
		return errorRespFromAPI("load watches", err)
	}
	w := Watch{
		ID:                fmt.Sprintf("w_%d_%s", time.Now().UnixMilli(), randSuffix()),
		Query:             strings.TrimSpace(body.Query),
		Enabled:           body.Enabled == nil || *body.Enabled,
		IntervalSeconds:   body.normalizedInterval(),
		Prompt:            body.Prompt,
		WorkflowID:        body.WorkflowID,
		WorkflowStepID:    body.WorkflowStepID,
		RepositoryID:      repoID,
		BaseBranch:        branch,
		AgentProfileID:    body.AgentProfileID,
		ExecutorProfileID: body.ExecutorProfileID,
		MaxInflight:       0,
		CreatedAt:         time.Now().UTC().Format(time.RFC3339),
	}
	if body.MaxInflight != nil && *body.MaxInflight > 0 {
		w.MaxInflight = *body.MaxInflight
	}
	watches = append(watches, w)
	if err := saveWatches(ctx, host, wsID, watches); err != nil {
		return errorRespFromAPI("save watches", err)
	}
	return jsonResp(map[string]any{"watch": watchToMap(w)})
}

type watchUpdateBody struct {
	ID              string  `json:"id"`
	Query           *string `json:"query"`
	Enabled         *bool   `json:"enabled"`
	IntervalSeconds *int    `json:"interval_seconds"`
	Prompt          *string `json:"prompt"`
	WorkflowID      *string `json:"workflow_id"`
	WorkflowStepID  *string `json:"workflow_step_id"`
	RepositoryID    *string `json:"repository_id"`
	BaseBranch      *string `json:"base_branch"`
	AgentProfileID  *string `json:"agent_profile_id"`
	ExecutorProfileID *string `json:"executor_profile_id"`
	MaxInflight     *int    `json:"max_inflight"`
}

func clampInterval(v int) int {
	if v < minWatchEvery {
		return defaultWatchEvery
	}
	if v > maxWatchEvery {
		return maxWatchEvery
	}
	return v
}

func (p *Plugin) actionWatchesUpdate(ctx context.Context, wsID string, req *pluginsdk.PluginActionRequest) (*pluginsdk.PluginActionResponse, error) {
	var body watchUpdateBody
	if err := json.Unmarshal(req.Body, &body); err != nil {
		return errorResp(400, "invalid body: "+err.Error())
	}
	if body.ID == "" {
		return errorResp(400, "id is required")
	}
	if body.MaxInflight != nil && *body.MaxInflight < 0 {
		return errorResp(400, "max_inflight must be zero or a positive integer")
	}
	host := p.Host()
	if host == nil {
		return errorResp(500, "host unavailable")
	}
	watches, err := loadWatches(ctx, host, wsID)
	if err != nil {
		return errorRespFromAPI("load watches", err)
	}
	for i := range watches {
		if watches[i].ID != body.ID {
			continue
		}
		prevRepoID, prevBaseBranch := watches[i].RepositoryID, watches[i].BaseBranch
		if body.Query != nil {
			watches[i].Query = strings.TrimSpace(*body.Query)
		}
		if body.Enabled != nil {
			watches[i].Enabled = *body.Enabled
		}
		if body.IntervalSeconds != nil {
			watches[i].IntervalSeconds = clampInterval(*body.IntervalSeconds)
		}
		if body.Prompt != nil {
			watches[i].Prompt = *body.Prompt
		}
		if body.WorkflowID != nil {
			watches[i].WorkflowID = *body.WorkflowID
		}
		if body.WorkflowStepID != nil {
			watches[i].WorkflowStepID = *body.WorkflowStepID
		}
		if body.RepositoryID != nil {
			watches[i].RepositoryID = *body.RepositoryID
		}
		if body.BaseBranch != nil {
			watches[i].BaseBranch = *body.BaseBranch
		}
		if body.AgentProfileID != nil {
			watches[i].AgentProfileID = *body.AgentProfileID
		}
		if body.ExecutorProfileID != nil {
			watches[i].ExecutorProfileID = *body.ExecutorProfileID
		}
		if body.MaxInflight != nil {
			watches[i].MaxInflight = *body.MaxInflight
		}
		// Post-patch guard, mirroring Jira's UpdateIssueWatch: an empty-string
		// PATCH write must not blank the query the poller needs.
		if watches[i].Query == "" {
			return errorResp(400, "query cannot be empty")
		}
		// Only validate/resolve the binding when its value actually changed.
		// Switching to a different repository without an explicit base branch
		// resets the branch so the new repo's default is used instead of
		// carrying the old repo's branch.
		if watches[i].RepositoryID != prevRepoID && body.BaseBranch == nil {
			watches[i].BaseBranch = ""
		}
		if watches[i].RepositoryID != prevRepoID || watches[i].BaseBranch != prevBaseBranch {
			repoID, branch, bindErr := resolveRepositoryBinding(ctx, host, wsID, watches[i].RepositoryID, watches[i].BaseBranch)
			if bindErr != nil {
				return errorResp(400, bindErr.Error())
			}
			watches[i].RepositoryID = repoID
			watches[i].BaseBranch = branch
		}
		if err := saveWatches(ctx, host, wsID, watches); err != nil {
			return errorRespFromAPI("save watches", err)
		}
		return jsonResp(map[string]any{"watch": watchToMap(watches[i])})
	}
	return errorResp(404, "watch not found")
}

// resolveRepositoryBinding validates an optional repository binding against
// the workspace's repositories and fills the base branch from the repository's
// default branch when omitted — mirroring Jira's resolveRepositoryBinding. An
// empty repositoryID unbinds the watch (the base branch is cleared with it).
func resolveRepositoryBinding(ctx context.Context, host pluginsdk.Host, wsID, repoID, baseBranch string) (string, string, error) {
	if strings.TrimSpace(repoID) == "" {
		return "", "", nil
	}
	repos, _, err := host.Repositories().List(ctx, wsID, pluginsdk.Page{Limit: 100})
	if err != nil {
		return "", "", fmt.Errorf("list workspace repositories: %w", err)
	}
	for _, r := range repos {
		if r.ID != repoID {
			continue
		}
		branch := strings.TrimSpace(baseBranch)
		if branch == "" && r.DefaultBranch != nil {
			branch = *r.DefaultBranch
		}
		return r.ID, branch, nil
	}
	return "", "", fmt.Errorf("repository %q not found in workspace", repoID)
}

type watchIDBody struct {
	ID string `json:"id"`
}

func (p *Plugin) actionWatchesDelete(ctx context.Context, wsID string, req *pluginsdk.PluginActionRequest) (*pluginsdk.PluginActionResponse, error) {
	var body watchIDBody
	if err := json.Unmarshal(req.Body, &body); err != nil {
		return errorResp(400, "invalid body: "+err.Error())
	}
	if body.ID == "" {
		return errorResp(400, "id is required")
	}
	host := p.Host()
	watches, err := loadWatches(ctx, host, wsID)
	if err != nil {
		return errorRespFromAPI("load watches", err)
	}
	out := watches[:0]
	found := false
	for _, w := range watches {
		if w.ID == body.ID {
			found = true
			continue
		}
		out = append(out, w)
	}
	if !found {
		return errorResp(404, "watch not found")
	}
	if err := saveWatches(ctx, host, wsID, out); err != nil {
		return errorRespFromAPI("save watches", err)
	}
	// Cascade the watch's dedup state (tick, task list, seen-issue entries),
	// mirroring Jira's DeleteIssueWatch store cascade.
	pruneWatchDedupState(ctx, host, wsID, body.ID)
	return jsonResp(map[string]any{"deleted": true})
}

func (p *Plugin) actionWatchesTrigger(ctx context.Context, wsID string, req *pluginsdk.PluginActionRequest) (*pluginsdk.PluginActionResponse, error) {
	var body watchIDBody
	if err := json.Unmarshal(req.Body, &body); err != nil {
		return errorResp(400, "invalid body: "+err.Error())
	}
	if body.ID == "" {
		return errorResp(400, "id is required")
	}
	created, err := runWatchByID(ctx, p, wsID, body.ID)
	if err != nil {
		return errorRespFromAPI("trigger watch", err)
	}
	return jsonResp(map[string]any{"new_tasks": created})
}

func randSuffix() string {
	return fmt.Sprintf("%x", time.Now().UnixNano()%0x1000000)
}

// actionContextOptions returns everything the watcher dialog needs in one
// call: workflows (with steps), repositories, and agent/executor profiles.
func (p *Plugin) actionContextOptions(ctx context.Context, wsID string) (*pluginsdk.PluginActionResponse, error) {
	host := p.Host()
	if host == nil {
		return errorResp(500, "host unavailable")
	}
	workflows, _, err := host.Workflows().List(ctx, wsID, pluginsdk.Page{Limit: 100})
	if err != nil {
		return errorRespFromAPI("list workflows", err)
	}
	var workflowOpts []map[string]any
	for _, wf := range workflows {
		steps, stepErr := host.Workflows().ListSteps(ctx, wf.ID)
		if stepErr != nil {
			steps = nil
		}
		stepOpts := make([]map[string]any, 0, len(steps))
		for _, s := range steps {
			stepOpts = append(stepOpts, map[string]any{"id": s.ID, "name": s.Name})
		}
		workflowOpts = append(workflowOpts, map[string]any{
			"id": wf.ID, "name": wf.Name, "steps": stepOpts,
		})
	}
	var repoOpts []map[string]any
	repos, _, repoErr := host.Repositories().List(ctx, wsID, pluginsdk.Page{Limit: 100})
	if repoErr == nil {
		for _, r := range repos {
			repoOpts = append(repoOpts, map[string]any{
				"id": r.ID, "name": r.Name, "default_branch": r.DefaultBranch,
			})
		}
	}
	var agentOpts []map[string]any
	profiles, _, agentErr := host.AgentProfiles().List(ctx, pluginsdk.Page{Limit: 100})
	if agentErr == nil {
		for _, a := range profiles {
			name := a.DisplayName
			if name == "" {
				name = a.Name
			}
			agentOpts = append(agentOpts, map[string]any{"id": a.ID, "name": name})
		}
	}
	var execOpts []map[string]any
	if reader, ok := pluginsdk.ExecutorProfiles(host); ok {
		execs, _, execErr := reader.List(ctx, pluginsdk.Page{Limit: 100})
		if execErr == nil {
			for _, e := range execs {
				name := e.DisplayName
				if name == "" {
					name = e.ID
				}
				execOpts = append(execOpts, map[string]any{"id": e.ID, "name": name})
			}
		}
	}
	if workflowOpts == nil {
		workflowOpts = []map[string]any{}
	}
	if repoOpts == nil {
		repoOpts = []map[string]any{}
	}
	if agentOpts == nil {
		agentOpts = []map[string]any{}
	}
	if execOpts == nil {
		execOpts = []map[string]any{}
	}
	return jsonResp(map[string]any{
		"workflows": workflowOpts, "repositories": repoOpts,
		"agent_profiles": agentOpts, "executor_profiles": execOpts,
	})
}

func jsonResp(v any) (*pluginsdk.PluginActionResponse, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal action response: %w", err)
	}
	return &pluginsdk.PluginActionResponse{Body: b}, nil
}

func errorResp(status int, msg string) (*pluginsdk.PluginActionResponse, error) {
	b, _ := json.Marshal(map[string]any{"error": msg})
	return &pluginsdk.PluginActionResponse{Body: b, Status: status}, nil
}

func errorRespFromAPI(action string, err error) (*pluginsdk.PluginActionResponse, error) {
	if apiErr, ok := youtrack.IsAPIError(err); ok {
		return errorResp(mapAPIStatus(apiErr.StatusCode), fmt.Sprintf("%s: %s", action, apiErr.Message))
	}
	return nil, fmt.Errorf("%s: %w", action, err)
}

func mapAPIStatus(code int) int {
	if code >= 400 && code < 500 {
		return code
	}
	return 502
}