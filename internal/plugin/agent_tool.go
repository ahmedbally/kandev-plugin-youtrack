package plugin

import (
	"context"
	"fmt"

	"github.com/kandev/kandev/pkg/pluginsdk"
	"github.com/kdlbs/kandev-plugin-youtrack/internal/youtrack"
)

const toolSearchIssues = "search_issues"

func (p *Plugin) InvokeAgentTool(ctx context.Context, req *pluginsdk.AgentToolRequest) (*pluginsdk.AgentToolResult, error) {
	if req == nil || req.Name != toolSearchIssues {
		return &pluginsdk.AgentToolResult{IsError: true, Text: fmt.Sprintf("unknown tool %q", req)}, nil
	}
	query, _ := req.Arguments["query"].(string)
	if query == "" {
		return &pluginsdk.AgentToolResult{IsError: true, Text: "query is required"}, nil
	}
	cfg, err := p.loadConfig(ctx, req.Context.WorkspaceID)
	if err != nil {
		return &pluginsdk.AgentToolResult{IsError: true, Text: "youtrack not configured for this workspace"}, nil
	}
	client := youtrack.New(cfg.BaseURL, cfg.Token)
	res, err := client.SearchIssues(ctx, query, cfg.DefaultProject, "", 10)
	if err != nil {
		return &pluginsdk.AgentToolResult{IsError: true, Text: fmt.Sprintf("youtrack search failed: %v", err)}, nil
	}
	return &pluginsdk.AgentToolResult{
		Text:              summarizeIssues(res.Issues),
		StructuredContent: map[string]any{"issues": res.Issues},
	}, nil
}

func summarizeIssues(issues []youtrack.Issue) string {
	if len(issues) == 0 {
		return "No YouTrack issues matched the query."
	}
	out := ""
	for _, it := range issues {
		out += fmt.Sprintf("- %s %s (%s)\n", it.IDReadable, it.Summary, it.URL)
	}
	return out
}