package youtrack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const userAgent = "kandev-plugin-youtrack/0.7 (+https://github.com/kdlbs/kandev-plugin-youtrack)"

const maxBodySize = 4 << 20

const defaultTop = 50

var ErrNotConfigured = errors.New("youtrack: not configured")

type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("youtrack api: status %d: %s", e.StatusCode, e.Message)
}

func IsAPIError(err error) (*APIError, bool) {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr, true
	}
	return nil, false
}

type Client struct {
	http    *http.Client
	baseURL string
	token   string
}

func New(baseURL, token string) *Client {
	base := strings.TrimRight(baseURL, "/")
	if base != "" && !strings.Contains(base, "://") {
		base = "https://" + base
	}
	return &Client{
		http: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		baseURL: base,
		token:   token,
	}
}

func (c *Client) authorize(req *http.Request) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	raw, err := c.doRaw(ctx, method, path, body)
	if err != nil {
		return err
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func (c *Client) doRaw(ctx context.Context, method, path string, body any) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.authorize(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{StatusCode: resp.StatusCode, Message: summarizeBody(resp, raw)}
	}
	return raw, nil
}

func summarizeBody(resp *http.Response, raw []byte) string {
	ct := resp.Header.Get("Content-Type")
	if strings.Contains(strings.ToLower(ct), "text/html") || bytes.HasPrefix(bytes.TrimLeft(raw, " \t\r\n"), []byte("<")) {
		return "YouTrack returned an HTML page (status " + strconv.Itoa(resp.StatusCode) + "); token may be rejected or expired."
	}
	const maxMsg = 500
	if len(raw) > maxMsg {
		return string(raw[:maxMsg]) + "..."
	}
	return string(raw)
}

type Project struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ShortName string `json:"shortName"`
}

func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	const fields = "id,name,shortName"
	var projects []Project
	err := c.do(ctx, http.MethodGet, "/api/admin/projects?fields="+url.QueryEscape(fields), nil, &projects)
	if err != nil {
		return nil, err
	}
	return projects, nil
}

func (c *Client) Ping(ctx context.Context) (string, error) {
	var me struct {
		Login string `json:"login"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/users/me?fields=login,name,email", nil, &me); err != nil {
		return "", err
	}
	return me.Login, nil
}

// Issue is the enriched, UI-ready issue shape returned by the plugin actions.
type Issue struct {
	ID              string   `json:"id"`
	IDReadable      string   `json:"idReadable"`
	Summary         string   `json:"summary"`
	Description     string   `json:"description"`
	Number          *int64   `json:"number,omitempty"`
	URL             string   `json:"url,omitempty"`
	Created         string   `json:"created,omitempty"`
	Updated         string   `json:"updated,omitempty"`
	ProjectID       string   `json:"project_id,omitempty"`
	ProjectName     string   `json:"project_name,omitempty"`
	ProjectShort    string   `json:"project_short,omitempty"`
	State           string   `json:"state,omitempty"`
	StateResolved   bool     `json:"state_resolved,omitempty"`
	AssigneeLogin   string   `json:"assignee_login,omitempty"`
	AssigneeName    string   `json:"assignee_name,omitempty"`
	AssigneeAvatar  string   `json:"assignee_avatar,omitempty"`
	ReporterName    string   `json:"reporter_name,omitempty"`
	Priority        string   `json:"priority,omitempty"`
	Tags            []string `json:"tags,omitempty"`
}

type ytUser struct {
	Login     string `json:"login"`
	FullName  string `json:"fullName"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatarUrl"`
}

func (u *ytUser) displayName() string {
	if u == nil {
		return ""
	}
	if u.FullName != "" {
		return u.FullName
	}
	return u.Name
}

type rawCustomField struct {
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
}

type rawIssue struct {
	ID           string `json:"id"`
	IDReadable   string `json:"idReadable"`
	Summary      string `json:"summary"`
	Description  string `json:"description"`
	Created      int64  `json:"created"`
	Updated      int64  `json:"updated"`
	Resolved     int64  `json:"resolved"`
	Number       *int64 `json:"number"`
	Project      *struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		ShortName string `json:"shortName"`
	} `json:"project"`
	Reporter     *ytUser           `json:"reporter"`
	CustomFields []rawCustomField  `json:"customFields"`
	Tags         []struct {
		Name string `json:"name"`
	} `json:"tags"`
}

// convert fills the UI-ready Issue from the raw YouTrack projection.
func (c *Client) convert(r *rawIssue) Issue {
	issue := Issue{
		ID:          r.ID,
		IDReadable:  r.IDReadable,
		Summary:     r.Summary,
		Description: r.Description,
		Number:      r.Number,
		URL:         c.issueURL(r.IDReadable),
	}
	if r.Created > 0 {
		issue.Created = time.UnixMilli(r.Created).UTC().Format(time.RFC3339)
	}
	if r.Updated > 0 {
		issue.Updated = time.UnixMilli(r.Updated).UTC().Format(time.RFC3339)
	}
	if r.Project != nil {
		issue.ProjectID = r.Project.ID
		issue.ProjectName = r.Project.Name
		issue.ProjectShort = r.Project.ShortName
	}
	issue.ReporterName = r.Reporter.displayName()
	for _, t := range r.Tags {
		if t.Name != "" {
			issue.Tags = append(issue.Tags, t.Name)
		}
	}
	parseCustomFields(&issue, r.CustomFields)
	// YouTrack returns relative avatar URLs (/hub/api/rest/avatar/...); prefix
	// with the base URL so the browser can load them from the correct host.
	if issue.AssigneeAvatar != "" && !strings.HasPrefix(issue.AssigneeAvatar, "http") {
		issue.AssigneeAvatar = c.baseURL + issue.AssigneeAvatar
	}
	return issue
}

// parseCustomFields interprets the polymorphic customFields values: State is a
// single {name,isResolved} object, Assignee a single user or an array of users,
// Priority a single {name} object. Unknown shapes are skipped, never fatal.
func parseCustomFields(issue *Issue, fields []rawCustomField) {
	for _, f := range fields {
		if len(f.Value) == 0 || string(f.Value) == "null" {
			continue
		}
		switch strings.ToLower(f.Name) {
		case "state":
			var v struct {
				Name       string `json:"name"`
				IsResolved bool   `json:"isResolved"`
			}
			if json.Unmarshal(f.Value, &v) == nil {
				issue.State = v.Name
				issue.StateResolved = v.IsResolved
			}
		case "assignee":
			var single ytUser
			if json.Unmarshal(f.Value, &single) == nil && single.Login != "" {
				issue.AssigneeLogin = single.Login
				issue.AssigneeName = single.displayName()
				issue.AssigneeAvatar = single.AvatarURL
				continue
			}
			var many []ytUser
			if json.Unmarshal(f.Value, &many) == nil && len(many) > 0 {
				issue.AssigneeLogin = many[0].Login
				issue.AssigneeName = many[0].displayName()
				issue.AssigneeAvatar = many[0].AvatarURL
			}
		case "priority":
			var v struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(f.Value, &v) == nil {
				issue.Priority = v.Name
			}
		}
	}
}

type searchResponse struct {
	Issues      []rawIssue `json:"issues"`
	HasAfter    bool       `json:"hasAfter"`
	AfterCursor string     `json:"afterCursor,omitempty"`
}

type SearchResult struct {
	Issues     []Issue
	NextCursor string
	HasMore    bool
}

func issueFields() string {
	return "id,idReadable,summary,description,number,created,updated,resolved," +
		"project(id,name,shortName),reporter(login,fullName,name,avatarUrl)," +
		"customFields(name,value(name,isResolved,login,fullName,name,avatarUrl))," +
		"tags(name)"
}

func (c *Client) SearchIssues(ctx context.Context, query, project string, cursor string, top int) (*SearchResult, error) {
	if top <= 0 {
		top = defaultTop
	}
	q := url.Values{}
	q.Set("query", buildQuery(query, project))
	q.Set("$top", strconv.Itoa(top))
	q.Set("fields", issueFields())
	if cursor != "" {
		q.Set("$skip", cursor)
	}
	raw, err := c.doRaw(ctx, http.MethodGet, "/api/issues?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	issues, hasAfter, afterCursor, err := parseIssuesResponse(raw)
	if err != nil {
		return nil, err
	}
	out := make([]Issue, len(issues))
	for i := range issues {
		out[i] = c.convert(&issues[i])
	}
	return &SearchResult{
		Issues:     out,
		NextCursor: advanceCursor(cursor, top, hasAfter, afterCursor),
		HasMore:    hasAfter,
	}, nil
}

func parseIssuesResponse(raw []byte) ([]rawIssue, bool, string, error) {
	trimmed := bytes.TrimLeft(raw, " \t\r\n")
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var issues []rawIssue
		if err := json.Unmarshal(raw, &issues); err != nil {
			return nil, false, "", fmt.Errorf("parse issues array: %w", err)
		}
		return issues, false, "", nil
	}
	var resp searchResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, false, "", fmt.Errorf("parse issues response: %w", err)
	}
	return resp.Issues, resp.HasAfter, resp.AfterCursor, nil
}

func (c *Client) GetIssue(ctx context.Context, idOrReadableID string) (*Issue, error) {
	var raw rawIssue
	path := "/api/issues/" + url.PathEscape(idOrReadableID) + "?fields=" + url.QueryEscape(issueFields())
	if err := c.do(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return nil, err
	}
	issue := c.convert(&raw)
	return &issue, nil
}

func (c *Client) ApplyIssueCommand(ctx context.Context, idOrReadableID string, command any) error {
	return c.do(ctx, http.MethodPost, "/api/issues/"+url.PathEscape(idOrReadableID), command, nil)
}

func (c *Client) issueURL(idReadable string) string {
	if idReadable == "" {
		return ""
	}
	return c.baseURL + "/issue/" + url.PathEscape(idReadable)
}

func buildQuery(query, project string) string {
	if project == "" {
		return query
	}
	if strings.TrimSpace(query) == "" {
		return "project: " + project
	}
	if strings.Contains(strings.ToLower(query), "project:") {
		return query
	}
	return "project: " + project + " " + query
}

func advanceCursor(cursor string, top int, hasAfter bool, afterCursor string) string {
	if !hasAfter {
		return ""
	}
	if afterCursor != "" {
		return afterCursor
	}
	skip := 0
	if n, err := strconv.Atoi(cursor); err == nil {
		skip = n
	}
	return strconv.Itoa(skip + top)
}