package plugin

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/kandev/kandev/pkg/pluginsdk"
)

const webhookKeyYouTrack = "youtrack"

func (p *Plugin) HandleWebhook(ctx context.Context, req *pluginsdk.WebhookRequest) (*pluginsdk.WebhookResponse, error) {
	if req.WebhookKey != webhookKeyYouTrack {
		return &pluginsdk.WebhookResponse{Status: 404}, nil
	}
	issues, err := parseWebhookIssues(req.Body)
	if err != nil {
		return &pluginsdk.WebhookResponse{Status: 400, Body: []byte("invalid youtrack webhook payload: " + err.Error())}, nil
	}
	if len(issues) == 0 {
		return &pluginsdk.WebhookResponse{Status: 200, Body: []byte("ok")}, nil
	}
	host := p.Host()
	if host == nil {
		return &pluginsdk.WebhookResponse{Status: 503, Body: []byte("host unavailable")}, nil
	}
	created := 0
	for _, ce := range issues {
		if _, err := host.Tasks().Create(ctx, pluginsdk.CreateTaskInput{
			Title:       ce.Summary,
			Description:  "",
			Metadata:    map[string]any{"youtrack_issue_id": ce.ID},
		}); err == nil {
			created++
		}
	}
	body, _ := json.Marshal(map[string]any{"created_tasks": created, "issues_seen": len(issues)})
	return &pluginsdk.WebhookResponse{Status: 200, Body: body}, nil
}

type webhookIssue struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
	FieldID string `json:"idReadable"`
}

func parseWebhookIssues(body []byte) ([]webhookIssue, error) {
	var wrapper struct {
		Issue   *webhookIssue `json:"issue"`
		Changes []struct {
			Issue *webhookIssue `json:"issue"`
		} `json:"changes"`
		Issues []webhookIssue `json:"issues"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return nil, err
	}
	var out []webhookIssue
	if wrapper.Issue != nil {
		out = append(out, *wrapper.Issue)
	}
	for _, c := range wrapper.Changes {
		if c.Issue != nil {
			out = append(out, *c.Issue)
		}
	}
	out = append(out, wrapper.Issues...)
	return out, nil
}

func matchesWebhookQuery(ce webhookIssue, project string) bool {
	if project == "" {
		return true
	}
	if ce.FieldID != "" {
		prefix := strings.ToUpper(project) + "-"
		return strings.HasPrefix(strings.ToUpper(ce.FieldID), prefix)
	}
	return true
}