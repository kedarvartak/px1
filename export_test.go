package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddedLinesByPath(t *testing.T) {
	diff := `diff --git a/api/users.ts b/api/users.ts
index 1111111..2222222 100644
--- a/api/users.ts
+++ b/api/users.ts
@@ -1,2 +1,3 @@
 const old = true
-const removed = true
+const added = fetch('/users')
+const second = true
diff --git a/README.md b/README.md
index 1111111..2222222 100644
--- a/README.md
+++ b/README.md
@@ -4 +4 @@
-old
+new
`
	got := addedLinesByPath(diff)
	if len(got) != 2 || got["api/users.ts"][2] != "const added = fetch('/users')" || got["api/users.ts"][3] != "const second = true" || got["README.md"][4] != "new" {
		t.Fatalf("added lines = %#v", got)
	}
}

func TestStaticReviewContextStaysWithinHunk(t *testing.T) {
	diff := `diff --git a/api/users.ts b/api/users.ts
index 1111111..2222222 100644
--- a/api/users.ts
+++ b/api/users.ts
@@ -1,4 +1,5 @@
 const before = true
-const old = true
+const added = fetch('/users')
+const after = true
 const trailing = true
`
	hits := addStaticReviewContexts([]ruleHit{{Path: "api/users.ts", Line: 2, Text: "const added = fetch('/users')"}}, diff)
	if len(hits) != 1 || len(hits[0].Context) != 5 {
		t.Fatalf("contexts = %#v", hits)
	}
	if hits[0].Context[0].Kind != "context" || hits[0].Context[1].Kind != "removed" || hits[0].Context[2].Kind != "added" || hits[0].Context[2].Number != 2 {
		t.Fatalf("context kinds = %#v", hits[0].Context)
	}
}

func TestGenerateStaticReviewUsesPinnedCommits(t *testing.T) {
	root := t.TempDir()
	runGitTest(t, root, "init")
	runGitTest(t, root, "config", "user.email", "test@example.com")
	runGitTest(t, root, "config", "user.name", "px1 test")
	writeExportTestFile(t, root, "api/users.ts", "export const load = () => true\n")
	runGitTest(t, root, "add", "api/users.ts")
	runGitTest(t, root, "commit", "-m", "base")
	base := strings.TrimSpace(runGitTest(t, root, "rev-parse", "HEAD"))

	writeExportTestFile(t, root, "api/users.ts", "export const load = () => true\nexport const get = () => fetch('/users')\n")
	writeExportTestFile(t, root, ".px1/rules.json", `{"rules":[{"pattern":"\\bfetch\\(","glob":"*.ts","message":"Use apiClient, not fetch"}]}`)
	runGitTest(t, root, "add", "api/users.ts", ".px1/rules.json")
	runGitTest(t, root, "commit", "-m", "head")
	head := strings.TrimSpace(runGitTest(t, root, "rev-parse", "HEAD"))

	report, err := generateStaticReview(root, base, head)
	if err != nil {
		t.Fatal(err)
	}
	if report.Base != base || report.Head != head {
		t.Fatalf("identity = %s..%s, want %s..%s", report.Base, report.Head, base, head)
	}
	if len(report.Files) != 2 || report.Files[0].Path != ".px1/rules.json" || report.Files[1].Path != "api/users.ts" {
		t.Fatalf("files = %#v", report.Files)
	}
	if len(report.Files[1].Hunks) != 1 || len(report.Files[1].Hunks[0].Lines) == 0 {
		t.Fatalf("structured hunks = %#v", report.Files[1].Hunks)
	}
	if len(report.RuleHits) != 1 {
		t.Fatalf("rule hits = %#v", report.RuleHits)
	}
	hit := report.RuleHits[0]
	if hit.Path != "api/users.ts" || hit.Line != 2 || hit.Message != "Use apiClient, not fetch" {
		t.Fatalf("hit = %#v", hit)
	}
	if len(hit.Context) == 0 || hit.Context[0].Text != "export const load = () => true" {
		t.Fatalf("context = %#v", hit.Context)
	}
	if report.Verification.Available || report.Verification.Error != "" || len(report.Verification.Checks) != 0 {
		t.Fatalf("verification = %#v", report.Verification)
	}

	// A mutable checkout change must not change the report's rules or diff.
	writeExportTestFile(t, root, ".px1/rules.json", `{"rules":[{"pattern":"\\bfetch\\(","glob":"*.ts","message":"Changed after commit"}]}`)
	again, err := generateStaticReview(root, base, head)
	if err != nil {
		t.Fatal(err)
	}
	if again.RuleHits[0].Message != "Use apiClient, not fetch" {
		t.Fatalf("checkout rule leaked into export: %#v", again.RuleHits)
	}

	out := t.TempDir()
	if err := writeStaticReviewReport(out, report); err != nil {
		t.Fatal(err)
	}
	snapshotPath := filepath.Join(out, "snapshot.json")
	if err := writeStaticReviewSnapshot(snapshotPath, report); err != nil {
		t.Fatal(err)
	}
	snapshotBytes, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot staticReviewReport
	if err := json.Unmarshal(snapshotBytes, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Version != 1 || snapshot.Base != base || snapshot.Head != head || len(snapshot.Files) != len(report.Files) {
		t.Fatalf("snapshot JSON lost report identity: %#v", snapshot)
	}
	reviewBytes, err := os.ReadFile(filepath.Join(out, "review.json"))
	if err != nil {
		t.Fatal(err)
	}
	var reviewData staticReviewReport
	if err := json.Unmarshal(reviewBytes, &reviewData); err != nil {
		t.Fatal(err)
	}
	if reviewData.Head != head || len(reviewData.RuleHits) != 1 || len(reviewData.Files) != len(report.Files) {
		t.Fatalf("machine-readable report lost review data: %#v", reviewData)
	}
	b, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(b)
	for _, want := range []string{"id=\"report-data\"", "api/users.ts", "Use apiClient, not fetch", "Automated findings", "Your code review", "px1:ack:", "localStorage", "aria-pressed", "Acknowledge"} {
		if !strings.Contains(html, want) {
			t.Errorf("report missing %q", want)
		}
	}
}

func writeExportTestFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runGitTest(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}
