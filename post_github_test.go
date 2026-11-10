package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	hostedreview "px1/internal/hosted/review"
)

const testHead = "32b2cee1c0b7a7aa98d4f1cda08f7d5dbbc374f3"

type fakeGitHub struct {
	mu          sync.Mutex
	issueBodies map[int64]string
	prComments  []map[string]any
	reviews     []map[string]any
	patches     map[string]string
	requests    []string
}

func (f *fakeGitHub) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.requests = append(f.requests, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		switch {
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/pulls/7/files"):
			var files []map[string]string
			for name, patch := range f.patches {
				files = append(files, map[string]string{"filename": name, "patch": patch})
			}
			json.NewEncoder(w).Encode(files)
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/pulls/7/comments"):
			json.NewEncoder(w).Encode(f.prComments)
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/pulls/7/reviews"):
			var payload map[string]any
			json.Unmarshal(body, &payload)
			f.reviews = append(f.reviews, payload)
			for _, c := range payload["comments"].([]any) {
				comment := c.(map[string]any)
				f.prComments = append(f.prComments, map[string]any{"body": comment["body"], "path": comment["path"], "line": comment["line"]})
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("{}"))
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/issues/7/comments"):
			var out []map[string]any
			for id, b := range f.issueBodies {
				out = append(out, map[string]any{"id": id, "body": b})
			}
			json.NewEncoder(w).Encode(out)
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/issues/7/comments"):
			var payload map[string]string
			json.Unmarshal(body, &payload)
			f.issueBodies[int64(len(f.issueBodies)+1)] = payload["body"]
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte("{}"))
		case r.Method == "PATCH" && strings.Contains(r.URL.Path, "/issues/comments/"):
			var payload map[string]string
			json.Unmarshal(body, &payload)
			f.issueBodies[1] = payload["body"]
			w.Write([]byte("{}"))
		default:
			http.NotFound(w, r)
		}
	})
}

func writeFindings(t *testing.T, findings ...map[string]any) string {
	t.Helper()
	data, _ := json.Marshal(map[string]any{"revision": testHead, "findings": findings})
	path := filepath.Join(t.TempDir(), "findings.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func finding(path string, line int, title string) map[string]any {
	return map[string]any{"path": path, "start_line": line, "end_line": line, "severity": "P2", "type": "logic", "title": title, "body": "why", "evidence": "line " + path, "confidence": 0.9}
}

func runPost(t *testing.T, server *httptest.Server, findings string, extra ...string) (string, error) {
	t.Helper()
	t.Setenv("GITHUB_TOKEN", "test-token")
	var out bytes.Buffer
	args := append([]string{"--findings", findings, "--repo", "o/r", "--pr", "7", "--api-url", server.URL}, extra...)
	err := runPostGitHub(args, &out)
	return out.String(), err
}

const samplePatch = "@@ -1,3 +10,4 @@\n ctx\n-old\n+new1\n+new2\n ctx2\n"

func TestPatchLinesCountsOnlyNewSideLines(t *testing.T) {
	lines := patchLines(samplePatch)
	for _, want := range []int{10, 11, 12, 13} {
		if !lines[want] {
			t.Fatalf("line %d should be commentable: %v", want, lines)
		}
	}
	if lines[9] || lines[14] || len(lines) != 4 {
		t.Fatalf("lines=%v", lines)
	}
}

func TestPostGitHubPostsInlineAndStickySummaryThenUpdatesInPlace(t *testing.T) {
	fake := &fakeGitHub{issueBodies: map[int64]string{}, patches: map[string]string{"src/a.ts": samplePatch}}
	server := httptest.NewServer(fake.handler())
	defer server.Close()
	findings := writeFindings(t, finding("src/a.ts", 11, "In the diff"), finding("src/a.ts", 99, "Outside the diff"), finding("src/other.ts", 3, "File not in PR"))

	out, err := runPost(t, server, findings, "--footer", "cost $0.02")
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.reviews) != 1 || len(fake.reviews[0]["comments"].([]any)) != 1 {
		t.Fatalf("want one review with one inline comment, got %+v (%s)", fake.reviews, out)
	}
	c := fake.reviews[0]["comments"].([]any)[0].(map[string]any)
	if c["path"] != "src/a.ts" || c["line"].(float64) != 11 || c["side"] != "RIGHT" || fake.reviews[0]["commit_id"] != testHead {
		t.Fatalf("inline comment=%v", c)
	}
	summary := fake.issueBodies[1]
	for _, want := range []string{agenticCommentMarker, "3 verified finding(s)", "In the diff", "Outside the diff", "File not in PR", "cost $0.02", "not on a changed line"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary lacks %q:\n%s", want, summary)
		}
	}

	// Second push, same findings: no duplicate inline comment, summary updated, not re-created.
	if _, err := runPost(t, server, findings); err != nil {
		t.Fatal(err)
	}
	if len(fake.reviews) != 1 {
		t.Fatalf("inline comment was posted twice: %d reviews", len(fake.reviews))
	}
	if len(fake.issueBodies) != 1 {
		t.Fatalf("summary was duplicated: %v", fake.issueBodies)
	}
	if !strings.Contains(fake.issueBodies[1], "In the diff") || !strings.Contains(fake.issueBodies[1], "inline") {
		t.Fatalf("summary after update:\n%s", fake.issueBodies[1])
	}
}

func TestPostGitHubWithNoFindingsStillSaysSoWithoutClaimingCorrectness(t *testing.T) {
	fake := &fakeGitHub{issueBodies: map[int64]string{}, patches: map[string]string{}}
	server := httptest.NewServer(fake.handler())
	defer server.Close()
	if _, err := runPost(t, server, writeFindings(t)); err != nil {
		t.Fatal(err)
	}
	if len(fake.reviews) != 0 {
		t.Fatal("no review should be created without findings")
	}
	if s := fake.issueBodies[1]; !strings.Contains(s, "No verified findings") || !strings.Contains(s, "not evidence the change is correct") {
		t.Fatalf("summary:\n%s", s)
	}
}

func TestPostGitHubRequiresTokenAndNeverEchoesIt(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	err := runPostGitHub([]string{"--findings", writeFindings(t), "--repo", "o/r", "--pr", "7"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("err=%v", err)
	}
	fake := &fakeGitHub{issueBodies: map[int64]string{}, patches: map[string]string{}}
	server := httptest.NewServer(fake.handler())
	defer server.Close()
	t.Setenv("GITHUB_TOKEN", "wrong-token-value")
	err = runPostGitHub([]string{"--findings", writeFindings(t), "--repo", "o/r", "--pr", "7", "--api-url", server.URL}, io.Discard)
	if err == nil || strings.Contains(err.Error(), "wrong-token-value") || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err=%v", err)
	}
}

func TestPostGitHubDryRunCallsNothing(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	var out bytes.Buffer
	err := runPostGitHub([]string{"--findings", writeFindings(t, finding("a.ts", 2, "T")), "--repo", "o/r", "--pr", "7", "--api-url", "http://127.0.0.1:1", "--dry-run"}, &out)
	if err != nil || !strings.Contains(out.String(), "Would post 1 inline") {
		t.Fatalf("err=%v out=%s", err, out.String())
	}
}

func TestPostGitHubCapsInlineCommentsAndKeepsTheRest(t *testing.T) {
	patch := "@@ -0,0 +1,40 @@\n" + strings.Repeat("+x\n", 40)
	fake := &fakeGitHub{issueBodies: map[int64]string{}, patches: map[string]string{"a.ts": patch}}
	server := httptest.NewServer(fake.handler())
	defer server.Close()
	var list []map[string]any
	for i := 1; i <= 25; i++ {
		list = append(list, finding("a.ts", i, "Issue "+strings.Repeat("x", i)))
	}
	if _, err := runPost(t, server, writeFindings(t, list...)); err != nil {
		t.Fatal(err)
	}
	if got := len(fake.reviews[0]["comments"].([]any)); got != maxInlineFindings {
		t.Fatalf("inline=%d", got)
	}
	if !strings.Contains(fake.issueBodies[1], "25 verified finding(s)") {
		t.Fatalf("summary dropped findings:\n%s", fake.issueBodies[1])
	}
}

func TestPostGitHubDoesNotRepostAFindingWhoseTitleWasReworded(t *testing.T) {
	fake := &fakeGitHub{issueBodies: map[int64]string{}, patches: map[string]string{"src/a.ts": samplePatch}}
	server := httptest.NewServer(fake.handler())
	defer server.Close()
	if _, err := runPost(t, server, writeFindings(t, finding("src/a.ts", 11, "Prefix matching treats names as equal")), "--head", testHead); err != nil {
		t.Fatal(err)
	}
	// Same bug, found again on the next push with different wording and a line or two off.
	reworded := finding("src/a.ts", 12, "Prefix names are treated as the same exercise")
	if _, err := runPost(t, server, writeFindings(t, reworded)); err != nil {
		t.Fatal(err)
	}
	if len(fake.reviews) != 1 {
		t.Fatalf("a reworded finding at the same place was posted again: %d reviews", len(fake.reviews))
	}
	if !strings.Contains(fake.issueBodies[1], "Prefix names are treated") || !strings.Contains(fake.issueBodies[1], "inline") {
		t.Fatalf("the summary should still list the current finding:\n%s", fake.issueBodies[1])
	}
	// A different problem in the same file, far away, is still posted.
	far := finding("src/a.ts", 13, "A different problem")
	fake.prComments[0]["line"] = float64(2) // pretend the first comment was elsewhere
	if _, err := runPost(t, server, writeFindings(t, far)); err != nil {
		t.Fatal(err)
	}
	if len(fake.reviews) != 2 {
		t.Fatalf("a finding well away from existing comments must be posted: %d reviews", len(fake.reviews))
	}
}

func TestAlreadyPostedUsesTheOriginalLineWhenTheCommentIsOutdated(t *testing.T) {
	f := finding("src/a.ts", 20, "x")
	hf := hostedFinding(f)
	posted := []postedComment{{Key: "000000000000", Path: "src/a.ts", Line: 21}, {Key: "111111111111", Path: "other.ts", Line: 20}}
	if !alreadyPosted(posted, "abc", hf) {
		t.Fatal("a comment one line away on the same file is the same finding")
	}
	if alreadyPosted(posted[1:], "abc", hf) {
		t.Fatal("a comment on a different file is not")
	}
	if alreadyPosted([]postedComment{{Key: "z", Path: "src/a.ts", Line: 0}}, "abc", hf) {
		t.Fatal("a comment with an unknown line cannot match by location")
	}
	if !alreadyPosted([]postedComment{{Key: "abc", Path: "gone.ts", Line: 0}}, "abc", hf) {
		t.Fatal("an identical key still matches")
	}
}

func hostedFinding(m map[string]any) hostedreview.Finding {
	return hostedreview.Finding{Path: m["path"].(string), StartLine: m["start_line"].(int), EndLine: m["end_line"].(int), Title: m["title"].(string)}
}
