package issues

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

type tokens struct{}

func (tokens) OAuthToken(context.Context, int64, Provider) (string, error) { return "oauth-token", nil }

func TestDetectIssueKeysAndAcceptanceCriteria(t *testing.T) {
	keys := DetectIssueKeys("feature/eng-123", "Implements OPS-9 and ENG-123")
	if !reflect.DeepEqual(keys, []string{"ENG-123", "OPS-9"}) {
		t.Fatalf("keys=%v", keys)
	}
	criteria := ExtractAcceptanceCriteria("## Acceptance criteria\n- [ ] retries safely\n- preserves evidence\n## Notes\n- ignore")
	if !reflect.DeepEqual(criteria, []string{"retries safely", "preserves evidence"}) {
		t.Fatalf("criteria=%v", criteria)
	}
}

func TestLinearClientFetchesBoundedOAuthContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer oauth-token" {
			t.Fatal("missing OAuth token")
		}
		_, _ = io.WriteString(w, `{"data":{"issue":{"identifier":"ENG-123","title":"Retry safely","description":"## Acceptance criteria\n- [ ] retries safely","url":"https://linear.app/acme/issue/ENG-123"}}}`)
	}))
	defer server.Close()
	issue, err := (LinearClient{HTTP: server.Client(), Tokens: tokens{}, BaseURL: server.URL}).FetchIssue(context.Background(), 1, "ENG-123")
	if err != nil || issue.Title != "Retry safely" || len(issue.AcceptanceCriteria) != 1 {
		t.Fatalf("issue=%#v err=%v", issue, err)
	}
}

func TestJiraClientFlattensADFDescription(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"key":"OPS-9","fields":{"summary":"Keep evidence","description":{"content":[{"content":[{"text":"Acceptance criteria"}]},{"content":[{"text":"- [ ] show provenance"}]}]}}}`)
	}))
	defer server.Close()
	issue, err := (JiraClient{HTTP: server.Client(), Tokens: tokens{}, BaseURL: server.URL}).FetchIssue(context.Background(), 1, "OPS-9")
	if err != nil || issue.Key != "OPS-9" || len(issue.AcceptanceCriteria) != 1 {
		t.Fatalf("issue=%#v err=%v", issue, err)
	}
}
