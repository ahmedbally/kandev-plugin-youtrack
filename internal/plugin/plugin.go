package plugin

import (
	"context"
	"fmt"

	"github.com/kandev/kandev/pkg/pluginsdk"
	"github.com/kdlbs/kandev-plugin-youtrack/internal/youtrack"
)

type Plugin struct {
	pluginsdk.UnimplementedPlugin
}

var (
	_ pluginsdk.Plugin          = (*Plugin)(nil)
	_ pluginsdk.ActionHandler   = (*Plugin)(nil)
	_ pluginsdk.AgentToolPlugin = (*Plugin)(nil)
)

func (p *Plugin) OnEvent(context.Context, *pluginsdk.Event) error { return nil }

const stateKey = "youtrack_config"

type config struct {
	BaseURL        string
	Token          string
	DefaultProject string
	DefaultQuery   string
}

func (p *Plugin) loadConfig(ctx context.Context, workspaceID string) (config, error) {
	host := p.Host()
	if host == nil {
		return config{}, fmt.Errorf("youtrack plugin: host not injected yet")
	}
	if workspaceID == "" {
		return config{}, youtrack.ErrNotConfigured
	}
	value, found, err := host.GetState(ctx, "workspace", workspaceID, stateKey)
	if err != nil {
		return config{}, fmt.Errorf("read youtrack config: %w", err)
	}
	cfg := config{
		BaseURL:        stringField(value, "base_url"),
		DefaultProject: stringField(value, "default_project"),
		DefaultQuery:   stringField(value, "default_query"),
	}
	token, tokenFound, err := host.GetSecret(ctx, secretKey(workspaceID))
	if err != nil {
		return config{}, fmt.Errorf("read youtrack token: %w", err)
	}
	cfg.Token = token
	if !found || !tokenFound || cfg.BaseURL == "" || cfg.Token == "" {
		return cfg, youtrack.ErrNotConfigured
	}
	if cfg.DefaultQuery == "" {
		cfg.DefaultQuery = "assignee: me"
	}
	return cfg, nil
}

func (p *Plugin) client(ctx context.Context, workspaceID string) (*youtrack.Client, error) {
	cfg, err := p.loadConfig(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	return youtrack.New(cfg.BaseURL, cfg.Token), nil
}

func secretKey(workspaceID string) string {
	return "youtrack_token_" + workspaceID
}

func stringField(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func workspaceIDFromReq(req *pluginsdk.PluginActionRequest) string {
	if req.Context.WorkspaceID != "" {
		return req.Context.WorkspaceID
	}
	return ""
}