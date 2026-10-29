package gitlab

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"px1/internal/hosted/scm"
)

type tokenSource string

func (t tokenSource) Token(context.Context, int64) (string, error) { return string(t), nil }

func TestProviderReadsGitLabDiffAndFiles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "secret" || r.URL.EscapedPath() != "/projects/acme%2Fproject/merge_requests/7/diffs" || r.URL.Query().Get("per_page") != "100" {
			t.Fatalf("request %s token=%q", r.URL.EscapedPath(), r.Header.Get("PRIVATE-TOKEN"))
		}
		_, _ = io.WriteString(w, `[{"old_path":"old.go","new_path":"new.go","diff":"@@ -1 +1 @@","renamed_file":true}]`)
	}))
	defer server.Close()
	provider := Provider{HTTP: server.Client(), Tokens: tokenSource("secret"), BaseURL: server.URL}
	repository := scm.Repository{InstallationID: 1, Owner: "acme", Name: "project"}
	pull := scm.PullRequest{Number: 7, BaseSHA: strings.Repeat("a", 40), HeadSHA: strings.Repeat("b", 40)}
	diff, err := provider.Diff(context.Background(), repository, pull)
	if err != nil || !strings.Contains(string(diff.Patch), "old.go") {
		t.Fatalf("diff=%q err=%v", diff.Patch, err)
	}
	files, err := provider.Files(context.Background(), repository, pull)
	if err != nil || len(files) != 1 || files[0].Status != "renamed" {
		t.Fatalf("files=%#v err=%v", files, err)
	}
}

func TestMapDiffPositionBuildsStableGitLabLineCode(t *testing.T) {
	position, err := MapDiffPosition("app.go", "app.go", 8, 9)
	if err != nil || position.LineCode != "275dfb31131cad1e6cecd2e3532d8ec752ba6c3e_8_9" {
		t.Fatalf("position=%#v err=%v", position, err)
	}
}

func TestVerifyWebhookUsesConstantTokenAndBoundedJSON(t *testing.T) {
	provider := Provider{WebhookToken: "hook-secret"}
	webhook, err := provider.VerifyWebhook(strings.NewReader(`{"object_kind":"merge_request"}`), "delivery", "hook-secret")
	if err != nil || webhook.Event != "merge_request" {
		t.Fatalf("webhook=%#v err=%v", webhook, err)
	}
	if _, err := provider.VerifyWebhook(strings.NewReader(`{}`), "delivery", "wrong"); err == nil {
		t.Fatal("accepted wrong token")
	}
}

func TestStatusMappingUsesGitLabStates(t *testing.T) {
	for input, expected := range map[string]string{"in_progress": "running", "completed": "success", "failure": "failed"} {
		got, err := mapStatus(input)
		if err != nil || got != expected {
			t.Fatalf("mapStatus(%q)=%q, %v", input, got, err)
		}
	}
}

func TestProviderRejectsSilentlyTruncatedDiffs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `[{"old_path":"large.go","new_path":"large.go","too_large":true}]`)
	}))
	defer server.Close()
	provider := Provider{HTTP: server.Client(), Tokens: tokenSource("secret"), BaseURL: server.URL}
	repository := scm.Repository{InstallationID: 1, Owner: "acme", Name: "project"}
	pull := scm.PullRequest{Number: 7, BaseSHA: strings.Repeat("a", 40), HeadSHA: strings.Repeat("b", 40)}
	if _, err := provider.Diff(context.Background(), repository, pull); err == nil {
		t.Fatal("accepted incomplete GitLab diff")
	}
}
