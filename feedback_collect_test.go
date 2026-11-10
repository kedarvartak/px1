package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func px1Comment(id int64, title string, up, down int, created time.Time) map[string]any {
	return map[string]any{"id": id, "body": "<!-- px1-finding:abcdef123456 -->\n**P2: " + title + "**\n\nwhy", "path": "src/a.ts", "line": 10,
		"pull_request_url": "https://api.github.com/repos/o/r/pulls/7", "created_at": created.Format(time.RFC3339),
		"user": map[string]any{"type": "Bot"}, "reactions": map[string]any{"+1": up, "-1": down}}
}

func fakeCommentsServer(t *testing.T, comments []map[string]any) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" || !strings.HasSuffix(r.URL.Path, "/repos/o/r/pulls/comments") {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("since") == "" {
			http.Error(w, "since is required", http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(comments)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestFeedbackCollectCountsReactionsAndRepliesOnPx1CommentsOnly(t *testing.T) {
	now := time.Now().UTC()
	human := map[string]any{"id": int64(99), "body": "I disagree", "created_at": now.Format(time.RFC3339), "user": map[string]any{"type": "User"}, "in_reply_to_id": int64(1), "reactions": map[string]any{"+1": 5, "-1": 0}}
	botReply := map[string]any{"id": int64(98), "body": "thanks", "created_at": now.Format(time.RFC3339), "user": map[string]any{"type": "Bot"}, "in_reply_to_id": int64(1)}
	someoneElses := map[string]any{"id": int64(50), "body": "nit: rename this", "path": "x.ts", "created_at": now.Format(time.RFC3339), "user": map[string]any{"type": "User"}, "reactions": map[string]any{"+1": 9, "-1": 0}}
	old := px1Comment(60, "Too old", 3, 0, now.AddDate(0, 0, -90))
	server := fakeCommentsServer(t, []map[string]any{
		px1Comment(1, "Boundary off by one", 2, 1, now), human, botReply, someoneElses, old,
		px1Comment(2, "Never reacted", 0, 0, now),
	})
	t.Setenv("GITHUB_TOKEN", "tok")
	out := filepath.Join(t.TempDir(), "fb.json")
	var stdout bytes.Buffer
	if err := runFeedbackCollect([]string{"--repo", "o/r", "--days", "30", "--api-url", server.URL, "--out", out}, &stdout); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(out)
	var stats feedbackStats
	if err := json.Unmarshal(data, &stats); err != nil {
		t.Fatal(err)
	}
	if stats.Comments != 2 || stats.ThumbsUp != 2 || stats.ThumbsDown != 1 || stats.Replied != 1 || stats.Engaged != 1 {
		t.Fatalf("stats=%+v (a human reply and another person's comment must not count as px1's)", stats)
	}
	if a, ok := stats.Acceptance(); !ok || a < 0.66 || a > 0.67 {
		t.Fatalf("acceptance=%v ok=%v", a, ok)
	}
	md := stdout.String()
	for _, want := range []string{"2 inline comments", "Acceptance 67%", "Boundary off by one", "Never reacted"} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "Too old") || strings.Contains(md, "rename this") {
		t.Fatalf("out-of-period or non-px1 comments leaked in:\n%s", md)
	}
	info, _ := os.Stat(out)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("output is %v, want owner-only", info.Mode().Perm())
	}
}

func TestFeedbackSilenceIsNotAgreement(t *testing.T) {
	now := time.Now().UTC()
	server := fakeCommentsServer(t, []map[string]any{px1Comment(1, "Quiet finding", 0, 0, now)})
	t.Setenv("GITHUB_TOKEN", "tok")
	var stdout bytes.Buffer
	if err := runFeedbackCollect([]string{"--repo", "o/r", "--api-url", server.URL}, &stdout); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "Acceptance unknown") || strings.Contains(stdout.String(), "Acceptance 0%") {
		t.Fatalf("no reactions must read as unknown, not 0%% or 100%%:\n%s", stdout.String())
	}
}

func TestFeedbackCollectEmptyAndInvalidInput(t *testing.T) {
	server := fakeCommentsServer(t, nil)
	t.Setenv("GITHUB_TOKEN", "tok")
	var stdout bytes.Buffer
	if err := runFeedbackCollect([]string{"--repo", "o/r", "--api-url", server.URL}, &stdout); err != nil || !strings.Contains(stdout.String(), "No px1 inline comments") {
		t.Fatalf("err=%v out=%s", err, stdout.String())
	}
	for name, args := range map[string][]string{"bad repo": {"--repo", "nope"}, "zero days": {"--repo", "o/r", "--days", "0"}} {
		if err := runFeedbackCollect(args, &bytes.Buffer{}); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	t.Setenv("GITHUB_TOKEN", "")
	if err := runFeedbackCollect([]string{"--repo", "o/r", "--api-url", server.URL}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("missing token: %v", err)
	}
	t.Setenv("GITHUB_TOKEN", "wrong-secret-token")
	if err := runFeedbackCollect([]string{"--repo", "o/r", "--api-url", server.URL}, &bytes.Buffer{}); err == nil || strings.Contains(err.Error(), "wrong-secret-token") {
		t.Fatalf("a rejected token must not be echoed: %v", err)
	}
}
