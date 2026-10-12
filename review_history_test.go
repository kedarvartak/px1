package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewReportHistoryPreservesEarlierRevisions(t *testing.T) {
	root := t.TempDir()
	origin := filepath.Join(t.TempDir(), "origin.git")
	runHistoryCommand(t, "", "git", "init", "--bare", origin)
	runHistoryCommand(t, root, "git", "init")
	runHistoryCommand(t, root, "git", "config", "user.name", "px1 test")
	runHistoryCommand(t, root, "git", "config", "user.email", "test@example.com")
	runHistoryCommand(t, root, "git", "remote", "add", "origin", origin)
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runHistoryCommand(t, root, "git", "add", "README.md")
	runHistoryCommand(t, root, "git", "commit", "-m", "initial")

	script, err := filepath.Abs(filepath.Join("scripts", "review-report-history.sh"))
	if err != nil {
		t.Fatal(err)
	}
	first := strings.Repeat("a", 40)
	second := strings.Repeat("b", 40)
	site := filepath.Join(root, "site")
	writeHistoryReport(t, site, first, "first")
	runHistoryCommand(t, root, "bash", script, "publish", site)

	restored := filepath.Join(root, "restored")
	runHistoryCommand(t, root, "bash", script, "restore", restored)
	assertHistoryReport(t, restored, first, "first")
	writeHistoryReport(t, restored, second, "second")
	runHistoryCommand(t, root, "bash", script, "publish", restored)

	final := filepath.Join(root, "final")
	runHistoryCommand(t, root, "bash", script, "restore", final)
	assertHistoryReport(t, final, first, "first")
	assertHistoryReport(t, final, second, "second")

	firstIndex := filepath.Join(final, "reviews", first, "index.html")
	if err := os.WriteFile(firstIndex, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	runHistoryCommandFailure(t, root, "invalid checksums", "bash", script, "publish", final)
	if err := os.WriteFile(firstIndex, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}

	unexpected := filepath.Join(final, "README.md")
	if err := os.WriteFile(unexpected, []byte("unexpected\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runHistoryCommandFailure(t, root, "refusing unexpected site entry", "bash", script, "publish", final)
	if err := os.Remove(unexpected); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(final, "reviews"), filepath.Join(final, "linked-reviews")); err != nil {
		t.Fatal(err)
	}
	runHistoryCommandFailure(t, root, "refusing symlink", "bash", script, "publish", final)
}

func writeHistoryReport(t *testing.T, root, revision, content string) {
	t.Helper()
	dir := filepath.Join(root, "reviews", revision)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	html := []byte(content)
	if err := os.WriteFile(filepath.Join(dir, "index.html"), html, 0o644); err != nil {
		t.Fatal(err)
	}
	review := []byte(`{"content":"` + content + `"}`)
	if err := os.WriteFile(filepath.Join(dir, "review.json"), review, 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf("%x  index.html\n%x  review.json\n", sha256.Sum256(html), sha256.Sum256(review))
	if err := os.WriteFile(filepath.Join(dir, "checksums.sha256"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertHistoryReport(t *testing.T, root, revision, want string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, "reviews", revision, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != want {
		t.Fatalf("report %s = %q, want %q", revision, body, want)
	}
	data, err := os.ReadFile(filepath.Join(root, "reviews", revision, "review.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), want) {
		t.Fatalf("review data %s = %q, want content %q", revision, data, want)
	}
	if _, err := os.Stat(filepath.Join(root, "reviews", revision, "checksums.sha256")); err != nil {
		t.Fatal(err)
	}
}

func runHistoryCommand(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out)
}

func runHistoryCommandFailure(t *testing.T, dir, want, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), want) {
		t.Fatalf("%s %s: error = %v, output = %q; want failure containing %q", name, strings.Join(args, " "), err, out, want)
	}
}
