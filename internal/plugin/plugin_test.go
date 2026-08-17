package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	pluginsdk "github.com/kandev/kandev/pkg/pluginsdk"
	"github.com/kdlbs/kandev-plugin-youtrack/internal/youtrack"
)

func newTestPlugin(t *testing.T, srvURL string) *Plugin {
	t.Helper()
	p := &Plugin{}
	host := &fakeHost{
		state: map[string]map[string]map[string]any{},
		secrets: map[string]string{},
	}
	host.state["ws-1"] = map[string]map[string]any{
		stateKey: {
			"base_url":        srvURL,
			"default_project": "",
			"default_query":   "assignee: me",
		},
	}
	host.secrets[secretKey("ws-1")] = "test-token"
	p.SetHost(host)
	return p
}

func TestPlugin_HandleAction_IssuesList(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/issues", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issues":[{"id":"2-1","idReadable":"FPU-1","summary":"first","description":"d"}],"hasAfter":false}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := newTestPlugin(t, srv.URL)
	body, _ := json.Marshal(listIssuesBody{Query: "for: me", Top: 5})
	resp, err := p.HandleAction(context.Background(), &pluginsdk.PluginActionRequest{
		ActionKey: "issues.list",
		Context:   pluginsdk.VerifiedActionContext{WorkspaceID: "ws-1"},
		Body:      body,
	})
	if err != nil {
		t.Fatalf("HandleAction: %v", err)
	}
	var out struct {
		Issues     []youtrack.Issue `json:"issues"`
		HasMore    bool             `json:"has_more"`
		NextCursor string           `json:"next_cursor"`
	}
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Issues) != 1 || out.Issues[0].IDReadable != "FPU-1" {
		t.Fatalf("issues = %+v", out.Issues)
	}
}

func TestPlugin_HandleAction_CreateTask(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/issues/FPU-42", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"2-42","idReadable":"FPU-42","summary":"build it","description":"desc"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := newTestPlugin(t, srv.URL)
	body, _ := json.Marshal(createTaskBody{IssueID: "FPU-42", WorkflowID: "wf-1", StartAgent: true})
	resp, err := p.HandleAction(context.Background(), &pluginsdk.PluginActionRequest{
		ActionKey: "issues.create_task",
		Context:  pluginsdk.VerifiedActionContext{WorkspaceID: "ws-1"},
		Body:     body,
	})
	if err != nil {
		t.Fatalf("HandleAction: %v", err)
	}
	var out struct {
		TaskID string          `json:"task_id"`
		Issue  youtrack.Issue  `json:"issue"`
	}
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.TaskID != "task-new" {
		t.Errorf("task_id = %q", out.TaskID)
	}
	if out.Issue.IDReadable != "FPU-42" {
		t.Errorf("issue = %+v", out.Issue)
	}
}

func TestPlugin_HandleAction_ConnectionCheck_OK(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/users/me", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"login":"tester","name":"Tester","email":"t@x.test"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := newTestPlugin(t, srv.URL)
	resp, err := p.HandleAction(context.Background(), &pluginsdk.PluginActionRequest{
		ActionKey: "connection.check",
		Context:   pluginsdk.VerifiedActionContext{WorkspaceID: "ws-1"},
	})
	if err != nil {
		t.Fatalf("HandleAction: %v", err)
	}
	var out struct {
		Connected bool   `json:"connected"`
		Login     string `json:"login"`
		BaseURL   string `json:"base_url"`
		HasToken  bool   `json:"has_token"`
	}
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !out.Connected || out.Login != "tester" || !out.HasToken {
		t.Errorf("connection = %+v", out)
	}
}

func TestPlugin_HandleAction_ConnectionCheck_NotConfigured(t *testing.T) {
	p := &Plugin{}
	p.SetHost(&fakeHost{state: map[string]map[string]map[string]any{}, secrets: map[string]string{}})
	resp, err := p.HandleAction(context.Background(), &pluginsdk.PluginActionRequest{
		ActionKey: "connection.check",
		Context:   pluginsdk.VerifiedActionContext{WorkspaceID: "ws-none"},
	})
	if err != nil {
		t.Fatalf("HandleAction returned error: %v", err)
	}
	var out struct {
		Connected bool `json:"connected"`
	}
	_ = json.Unmarshal(resp.Body, &out)
	if out.Connected {
		t.Error("expected connected=false when not configured")
	}
}

func TestPlugin_HandleAction_ConnectionSave(t *testing.T) {
	p := &Plugin{}
	host := &fakeHost{state: map[string]map[string]map[string]any{}, secrets: map[string]string{}}
	p.SetHost(host)

	saveBody, _ := json.Marshal(connectionSaveBody{
		BaseURL:        "https://youtrack.test",
		PermanentToken: "perm-123",
		DefaultProject: "FPU",
		DefaultQuery:   "for: me",
	})
	resp, err := p.HandleAction(context.Background(), &pluginsdk.PluginActionRequest{
		ActionKey: "connection.save",
		Context:   pluginsdk.VerifiedActionContext{WorkspaceID: "ws-save"},
		Body:      saveBody,
	})
	if err != nil {
		t.Fatalf("HandleAction: %v", err)
	}
	var out struct {
		Saved bool `json:"saved"`
	}
	_ = json.Unmarshal(resp.Body, &out)
	if !out.Saved {
		t.Error("expected saved=true")
	}
	cfg := host.state["ws-save"][stateKey]
	if cfg["base_url"] != "https://youtrack.test" {
		t.Errorf("base_url = %v", cfg["base_url"])
	}
	if cfg["default_project"] != "FPU" {
		t.Errorf("default_project = %v", cfg["default_project"])
	}
	if host.secrets[secretKey("ws-save")] != "perm-123" {
		t.Errorf("token = %v", host.secrets[secretKey("ws-save")])
	}
}

func TestPlugin_HandleAction_ConnectionSave_TokenMask(t *testing.T) {
	p := &Plugin{}
	host := &fakeHost{
		state: map[string]map[string]map[string]any{
			"ws-mask": {stateKey: {"base_url": "https://old.test"}},
		},
		secrets: map[string]string{secretKey("ws-mask"): "old-token"},
	}
	p.SetHost(host)

	saveBody, _ := json.Marshal(connectionSaveBody{
		BaseURL:        "https://new.test",
		PermanentToken: "********",
	})
	resp, _ := p.HandleAction(context.Background(), &pluginsdk.PluginActionRequest{
		ActionKey: "connection.save",
		Context:   pluginsdk.VerifiedActionContext{WorkspaceID: "ws-mask"},
		Body:      saveBody,
	})
	var out struct{ Saved bool `json:"saved"` }
	_ = json.Unmarshal(resp.Body, &out)
	if !out.Saved {
		t.Error("expected saved=true")
	}
	if host.secrets[secretKey("ws-mask")] != "old-token" {
		t.Error("token should be unchanged when masked")
	}
}

func TestPlugin_HandleAction_UnknownKey(t *testing.T) {
	p := newTestPlugin(t, "https://example.test")
	_, err := p.HandleAction(context.Background(), &pluginsdk.PluginActionRequest{ActionKey: "bogus"})
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
}

func TestPlugin_HandleAction_NotConfigured(t *testing.T) {
	p := &Plugin{}
	p.SetHost(&fakeHost{state: map[string]map[string]map[string]any{}, secrets: map[string]string{}})
	_, err := p.HandleAction(context.Background(), &pluginsdk.PluginActionRequest{
		ActionKey: "issues.list",
		Context:   pluginsdk.VerifiedActionContext{WorkspaceID: "ws-none"},
	})
	if err == nil {
		t.Fatal("expected error for not configured")
	}
}

func TestPlugin_InvokeAgentTool_Search(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/issues", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issues":[{"id":"2-1","idReadable":"FPU-1","summary":"first"},{"id":"2-2","idReadable":"FPU-2","summary":"second"}],"hasAfter":false}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := newTestPlugin(t, srv.URL)
	result, err := p.InvokeAgentTool(context.Background(), &pluginsdk.AgentToolRequest{
		Name:      "search_issues",
		Arguments: map[string]any{"query": "for: me"},
		Context:   pluginsdk.AgentToolContext{TaskID: "task-x", WorkspaceID: "ws-1"},
	})
	if err != nil {
		t.Fatalf("InvokeAgentTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Text)
	}
	if !contains(result.Text, "FPU-1") || !contains(result.Text, "FPU-2") {
		t.Errorf("text = %q", result.Text)
	}
}

func TestPlugin_InvokeAgentTool_UnknownName(t *testing.T) {
	p := newTestPlugin(t, "https://example.test")
	result, _ := p.InvokeAgentTool(context.Background(), &pluginsdk.AgentToolRequest{Name: "bogus"})
	if !result.IsError {
		t.Error("expected IsError")
	}
}

func TestPlugin_InvokeAgentTool_MissingQuery(t *testing.T) {
	p := newTestPlugin(t, "https://example.test")
	result, _ := p.InvokeAgentTool(context.Background(), &pluginsdk.AgentToolRequest{Name: "search_issues"})
	if !result.IsError || !contains(result.Text, "query is required") {
		t.Errorf("result = %+v", result)
	}
}

func TestPlugin_HandleWebhook_UnknownKey(t *testing.T) {
	p := newTestPlugin(t, "https://example.test")
	resp, err := p.HandleWebhook(context.Background(), &pluginsdk.WebhookRequest{WebhookKey: "other"})
	if err != nil || resp.Status != 404 {
		t.Fatalf("resp = %+v, err = %v", resp, err)
	}
}

func TestPlugin_OnEvent_NoOp(t *testing.T) {
	p := newTestPlugin(t, "https://example.test")
	if err := p.OnEvent(context.Background(), &pluginsdk.Event{EventType: "task.created"}); err != nil {
		t.Errorf("OnEvent: %v", err)
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && indexOf(s, sub) >= 0 }

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

type fakeHost struct {
	pluginsdk.UnimplementedHostData
	state   map[string]map[string]map[string]any
	secrets map[string]string
	creates []pluginsdk.CreateTaskInput
}

func (h *fakeHost) GetState(_ context.Context, scope, scopeID, key string) (map[string]any, bool, error) {
	scopeMap, ok := h.state[scopeID]
	if !ok {
		return nil, false, nil
	}
	val, ok := scopeMap[key]
	return val, ok, nil
}

func (h *fakeHost) SetState(_ context.Context, scope, scopeID, key string, value map[string]any) error {
	if h.state[scopeID] == nil {
		h.state[scopeID] = map[string]map[string]any{}
	}
	h.state[scopeID][key] = value
	return nil
}

func (h *fakeHost) DeleteState(_ context.Context, _, _, _ string) error { return nil }

func (h *fakeHost) ListState(_ context.Context, _, _ string) ([]pluginsdk.StateEntry, error) {
	return nil, nil
}

func (h *fakeHost) GetConfig(context.Context) (map[string]any, error) { return map[string]any{}, nil }

func (h *fakeHost) GetSecret(_ context.Context, key string) (string, bool, error) {
	v, ok := h.secrets[key]
	return v, ok, nil
}

func (h *fakeHost) SetSecret(_ context.Context, key, value string) error {
	h.secrets[key] = value
	return nil
}

func (h *fakeHost) DeleteSecret(_ context.Context, key string) error {
	delete(h.secrets, key)
	return nil
}

func (h *fakeHost) Tasks() pluginsdk.TaskReader { return fakeTaskReader{host: h} }

func (h *fakeHost) Workspaces() pluginsdk.WorkspaceReader { return fakeWorkflowReader{}.asWorkspace() }

func (h *fakeHost) Workflows() pluginsdk.WorkflowReader { return fakeWorkflowReader{} }

func (h *fakeHost) RevealSecret(_ context.Context, _ string) (string, error)       { return "", errors.New("noop") }
func (h *fakeHost) EmitEvent(_ context.Context, _ string, _ map[string]any) error     { return nil }

type fakeTaskReader struct {
	host *fakeHost
}

func (r fakeTaskReader) List(_ context.Context, _ pluginsdk.TaskFilter, _ pluginsdk.Page) ([]pluginsdk.Task, *pluginsdk.PageInfo, error) {
	return nil, nil, errors.New("not implemented")
}

func (r fakeTaskReader) Get(_ context.Context, _ string) (*pluginsdk.Task, error) {
	return nil, errors.New("not implemented")
}

func (r fakeTaskReader) Create(ctx context.Context, in pluginsdk.CreateTaskInput) (*pluginsdk.Task, error) {
	r.host.creates = append(r.host.creates, in)
	return &pluginsdk.Task{ID: "task-new", WorkspaceID: in.WorkspaceID, Title: in.Title}, nil
}

func (r fakeTaskReader) Update(_ context.Context, _ pluginsdk.UpdateTaskInput) (*pluginsdk.Task, error) {
	return nil, errors.New("not implemented")
}

type fakeWorkflowReader struct{}

func (fakeWorkflowReader) List(_ context.Context, _ string, _ pluginsdk.Page) ([]pluginsdk.Workflow, *pluginsdk.PageInfo, error) {
	return nil, nil, errors.New("not implemented")
}

func (fakeWorkflowReader) ListSteps(_ context.Context, _ string) ([]pluginsdk.WorkflowStep, error) {
	return nil, errors.New("not implemented")
}

func (w fakeWorkflowReader) asWorkspace() pluginsdk.WorkspaceReader {
	return fakeWorkspaceReader{}
}

type fakeWorkspaceReader struct{}

func (fakeWorkspaceReader) List(_ context.Context, _ pluginsdk.Page) ([]pluginsdk.Workspace, *pluginsdk.PageInfo, error) {
	return nil, nil, errors.New("not implemented")
}

var _ pluginsdk.Host = (*fakeHost)(nil)
var _ pluginsdk.WorkflowReader = fakeWorkflowReader{}
var _ pluginsdk.WorkspaceReader = fakeWorkspaceReader{}