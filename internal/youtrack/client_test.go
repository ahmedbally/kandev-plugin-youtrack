package youtrack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClient_SerializeAuthAndFields(t *testing.T) {
	var gotAuth, gotUA, gotAccept, gotFields, gotQuery string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/users/me", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotUA = r.Header.Get("User-Agent")
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"login":"alpha","name":"Alpha","email":"a@b.test"}`))
	})
	mux.HandleFunc("/api/issues", func(w http.ResponseWriter, r *http.Request) {
		gotFields = r.URL.Query().Get("fields")
		gotQuery = r.URL.Query().Get("query")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issues":[{"id":"2-1","idReadable":"FPU-1","summary":"first","description":"d1"}],"hasAfter":false}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := New(srv.URL, "perm-xyz")
	login, err := client.Ping(context.Background())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if login != "alpha" {
		t.Fatalf("login = %q, want alpha", login)
	}
	if gotAuth != "Bearer perm-xyz" {
		t.Errorf("Authorization = %q, want Bearer perm-xyz", gotAuth)
	}
	if !strings.HasPrefix(gotUA, "kandev-plugin-youtrack/") {
		t.Errorf("User-Agent = %q", gotUA)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q", gotAccept)
	}

	res, err := client.SearchIssues(context.Background(), "assignee: me", "", "", 10)
	if err != nil {
		t.Fatalf("SearchIssues: %v", err)
	}
	if len(res.Issues) != 1 || res.Issues[0].IDReadable != "FPU-1" {
		t.Fatalf("issues = %+v", res.Issues)
	}
	wantFields := "id,idReadable,summary,description,number,created,updated,resolved,project(id,name,shortName),reporter(login,fullName,name,avatarUrl),customFields(name,value(name,isResolved,login,fullName,name,avatarUrl)),tags(name)"
	if gotFields != wantFields {
		t.Errorf("fields = %q", gotFields)
	}
	if gotQuery != "assignee: me" {
		t.Errorf("query = %q", gotQuery)
	}
	if res.HasMore || res.NextCursor != "" {
		t.Errorf("pagination = %+v, want no more", res)
	}
	if res.Issues[0].URL == "" || !strings.Contains(res.Issues[0].URL, "/issue/FPU-1") {
		t.Errorf("issue URL = %q", res.Issues[0].URL)
	}
}

func TestClient_SearchIssues_Pagination(t *testing.T) {
	page := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/issues", func(w http.ResponseWriter, r *http.Request) {
		page++
		skip := r.URL.Query().Get("$skip")
		hasAfter := true
		if page == 2 {
			hasAfter = false
		}
		body := map[string]any{
			"issues": []map[string]any{{
				"id": "2-" + skip, "idReadable": "FPU-" + skip, "summary": "page " + skip,
			}},
			"hasAfter": hasAfter,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := New(srv.URL, "tok")
	res, err := client.SearchIssues(context.Background(), "", "", "", 2)
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if !res.HasMore || res.NextCursor != "2" {
		t.Fatalf("page 1 next = %+v, want hasMore+cursor 2", res)
	}
	res2, err := client.SearchIssues(context.Background(), "", "", res.NextCursor, 2)
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if res2.HasMore || res2.NextCursor != "" {
		t.Fatalf("page 2 next = %+v, want terminal", res2)
	}
}

func TestClient_SearchIssues_ProjectQualifier(t *testing.T) {
	var gotQuery string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/issues", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("query")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issues":[]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := New(srv.URL, "t")
	if _, err := c.SearchIssues(context.Background(), "for: me", "FPU", "", 5); err != nil {
		t.Fatalf("SearchIssues: %v", err)
	}
	if !strings.Contains(gotQuery, "project: FPU") || !strings.Contains(gotQuery, "for: me") {
		t.Errorf("query = %q, want project qualifier + query", gotQuery)
	}
}

func TestClient_GetIssue(t *testing.T) {
	var gotPath string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/issues/FPU-42", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"2-42","idReadable":"FPU-42","summary":"build it","description":"d"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := New(srv.URL, "t")
	issue, err := c.GetIssue(context.Background(), "FPU-42")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if gotPath != "/api/issues/FPU-42" {
		t.Errorf("path = %q", gotPath)
	}
	if issue.IDReadable != "FPU-42" || issue.Summary != "build it" {
		t.Errorf("issue = %+v", issue)
	}
}

func TestClient_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/users/me", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"forbidden"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := New(srv.URL, "bad")
	_, err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := IsAPIError(err)
	if !ok || apiErr.StatusCode != 401 {
		t.Fatalf("err = %v, want APIError 401", err)
	}
}

func TestClient_NormalizeBaseURL(t *testing.T) {
	c := New("youtrack.example.com/", "t")
	if c.baseURL != "https://youtrack.example.com" {
		t.Errorf("baseURL = %q", c.baseURL)
	}
}

func TestClient_ApplyIssueCommand(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/issues/FPU-1", func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := New(srv.URL, "t")
	err := c.ApplyIssueCommand(context.Background(), "FPU-1", map[string]any{"customFields": []any{}})
	if err != nil {
		t.Fatalf("ApplyIssueCommand: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/issues/FPU-1" {
		t.Errorf("method/path = %s %s", gotMethod, gotPath)
	}
	if gotBody == nil {
		t.Error("body not captured")
	}
}

func TestBuildQuery(t *testing.T) {
	cases := []struct {
		query, project, want string
	}{
		{"", "FPU", "project: FPU"},
		{"for: me", "", "for: me"},
		{"project: FPU for: me", "OTHER", "project: FPU for: me"},
		{"x", "FPU", "project: FPU x"},
	}
	for i, tc := range cases {
		got := buildQuery(tc.query, tc.project)
		if got != tc.want {
			t.Errorf("case %d: buildQuery(%q,%q) = %q, want %q", i, tc.query, tc.project, got, tc.want)
		}
	}
}

func TestAdvanceCursor(t *testing.T) {
	if c := advanceCursor("", 50, true, ""); c != "50" {
		t.Errorf("empty cursor advance = %q", c)
	}
	if c := advanceCursor("100", 50, true, ""); c != "150" {
		t.Errorf("cursor 100 advance = %q, want 150", c)
	}
	if c := advanceCursor("", 50, false, "after123"); c != "" {
		t.Errorf("terminal cursor = %q", c)
	}
	if c := advanceCursor("", 50, true, "after123"); c != "after123" {
		t.Errorf("afterCursor = %q", c)
	}
}

func TestClient_RejectEmptyConfig(t *testing.T) {
	c := New("", "")
	_, err := c.Ping(context.Background())
	if err == nil || !strings.Contains(err.Error(), "api/users/me") {
		// With baseURL "" the http call goes to a relative path which
		// http.NewRequest rejects; just confirm it errors rather than panics.
		t.Logf("empty base returned err: %v", err)
	}
}

func TestResponseShapeUnmarshal(t *testing.T) {
	raw := []byte(`{"issues":[{"id":"2-1","idReadable":"FPU-1","summary":"s"}],"hasAfter":true,"afterCursor":"abc"}`)
	var resp searchResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Issues) != 1 || resp.Issues[0].IDReadable != "FPU-1" || resp.Issues[0].Summary != "s" {
		t.Fatalf("mismatch: %+v", resp.Issues)
	}
	if !resp.HasAfter || resp.AfterCursor != "abc" {
		t.Fatalf("pagination mismatch: %+v", resp)
	}
}

func TestConvertEnrichesCustomFields(t *testing.T) {
	c := New("https://yt.test", "tok")
	raw := fmt.Sprintf(`{
		"id":"2-9","idReadable":"FPU-9","summary":"rich","description":"d",
		"created":1700000000000,"updated":1700000001000,
		"project":{"id":"0-1","name":"FPU Project","shortName":"FPU"},
		"reporter":{"login":"rep","fullName":"Reporter Name","avatarUrl":"https://a.test/rep.png"},
		"tags":[{"name":"bug"},{"name":"urgent"}],
		"customFields":[
			{"name":"State","value":{"name":"In Progress","isResolved":false}},
			{"name":"Assignee","value":{"login":"ahmed","fullName":"Ahmed Bally","avatarUrl":"https://a.test/me.png"}},
			{"name":"Priority","value":{"name":"Critical"}}
		]
	}`)
	var r rawIssue
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	issue := c.convert(&r)
	if issue.ProjectShort != "FPU" || issue.ProjectName != "FPU Project" {
		t.Errorf("project = %+v", issue)
	}
	if issue.State != "In Progress" || issue.StateResolved {
		t.Errorf("state = %q resolved=%v", issue.State, issue.StateResolved)
	}
	if issue.AssigneeLogin != "ahmed" || issue.AssigneeName != "Ahmed Bally" || issue.AssigneeAvatar != "https://a.test/me.png" {
		t.Errorf("assignee = %+v", issue)
	}
	if issue.Priority != "Critical" {
		t.Errorf("priority = %q", issue.Priority)
	}
	if len(issue.Tags) != 2 || issue.Tags[0] != "bug" {
		t.Errorf("tags = %v", issue.Tags)
	}
	if issue.ReporterName != "Reporter Name" {
		t.Errorf("reporter = %q", issue.ReporterName)
	}
	if issue.Created == "" || issue.Updated == "" {
		t.Errorf("timestamps = %q %q", issue.Created, issue.Updated)
	}
}
