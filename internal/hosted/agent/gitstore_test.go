package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type fixture struct {
	repo   GitRepo
	scope  Scope
	first  string
	marker string
}

func runGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root, "-c", "user.email=t@example.com", "-c", "user.name=Tess Tester", "-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newFixture builds a two-commit repository whose own configuration is
// hostile: it points textconv and fsmonitor at a script that records a run.
func newFixture(t *testing.T) fixture {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	write(t, root, "pkg/a.go", "package pkg\n\nfunc Charge() {}\n")
	write(t, root, "b.txt", "-x needle\r\nsecond\n")
	write(t, root, "d:ir/we:ird.txt", "needle in a odd path\n")
	write(t, root, "bin.dat", "ab\x00cd")
	write(t, root, "big.txt", strings.Repeat("x", maxBlobBytes+10))
	write(t, root, ".gitattributes", "*.go diff=evil\n*.txt diff=evil\n")
	if err := os.Symlink("pkg/a.go", filepath.Join(root, "link.go")); err != nil {
		t.Skip("symlinks unavailable")
	}
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-qm", "first commit")
	first := runGit(t, root, "rev-parse", "HEAD")
	write(t, root, "pkg/a.go", "package pkg\n\nfunc Charge() { refund() }\n")
	runGit(t, root, "commit", "-qam", "second: call refund")
	head := runGit(t, root, "rev-parse", "HEAD")

	marker := filepath.Join(t.TempDir(), "marker")
	script := filepath.Join(t.TempDir(), "evil.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho ran >> "+marker+"\ncat \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "config", "diff.evil.textconv", script)
	runGit(t, root, "config", "core.fsmonitor", script)
	return fixture{repo: GitRepo{Root: root}, scope: Scope{InstallationID: 1, RepositoryID: 1, HeadSHA: head}, first: first, marker: marker}
}

func (f fixture) ran() bool {
	_, err := os.Stat(f.marker)
	return err == nil
}

func TestGitRepoNeverRunsRepositoryConfiguredCommands(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.repo.ReadLines(ctx, f.scope, "pkg/a.go", 1, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.Search(ctx, f.scope, "Charge", 10); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.Blame(ctx, f.scope, "pkg/a.go", 1, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.Log(ctx, f.scope, "pkg/a.go", 5); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.ListFiles(ctx, f.scope, ""); err != nil {
		t.Fatal(err)
	}
	if f.ran() {
		t.Fatal("git executed a command configured by the repository")
	}
}

func TestReadLinesNumbersLinesAndStripsCarriageReturns(t *testing.T) {
	f := newFixture(t)
	got, err := f.repo.ReadLines(context.Background(), f.scope, "b.txt", 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if got != "b.txt lines 1-2 of 2\n1\t-x needle\n2\tsecond\n" {
		t.Fatalf("read = %q", got)
	}
	if _, err := f.repo.ReadLines(context.Background(), f.scope, "b.txt", 3, 4); err == nil || !strings.Contains(err.Error(), "only 2 lines") {
		t.Fatalf("past end: %v", err)
	}
}

func TestReadLinesRefusesUnsafeOrUnreadableFiles(t *testing.T) {
	f := newFixture(t)
	for path, want := range map[string]string{
		"bin.dat":        "binary",
		"big.txt":        "cannot be read",
		"link.go":        "symbolic link",
		"pkg":            "directory",
		"missing.go":     "not found",
		"../outside.txt": "repository-relative",
	} {
		if _, err := f.repo.ReadLines(context.Background(), f.scope, path, 1, 2); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: %v, want %q", path, err, want)
		}
	}
}

func TestReadsArePinnedToTheRevision(t *testing.T) {
	f := newFixture(t)
	old := f.scope
	old.HeadSHA = f.first
	got, err := f.repo.ReadLines(context.Background(), old, "pkg/a.go", 3, 3)
	if err != nil || !strings.Contains(got, "func Charge() {}") || strings.Contains(got, "refund") {
		t.Fatalf("old revision = %q %v", got, err)
	}
	if _, err := f.repo.ReadLines(context.Background(), Scope{HeadSHA: strings.Repeat("0", 40)}, "pkg/a.go", 1, 1); err == nil {
		t.Fatal("unknown revision read")
	}
}

func TestSearchIsLiteralBoundedAndDistinguishesNoMatchFromFailure(t *testing.T) {
	f := newFixture(t)
	hits, err := f.repo.Search(context.Background(), f.scope, "-x needle", 10)
	if err != nil || len(hits) != 1 || hits[0].Path != "b.txt" || hits[0].StartLine != 1 || hits[0].Source != "git-grep" {
		t.Fatalf("option-like query: %#v %v", hits, err)
	}
	odd, err := f.repo.Search(context.Background(), f.scope, "odd path", 10)
	if err != nil || len(odd) != 1 || odd[0].Path != "d:ir/we:ird.txt" {
		t.Fatalf("colon path: %#v %v", odd, err)
	}
	if none, err := f.repo.Search(context.Background(), f.scope, "absent-token", 10); err != nil || len(none) != 0 {
		t.Fatalf("no match: %#v %v", none, err)
	}
	if _, err := f.repo.Search(context.Background(), Scope{HeadSHA: strings.Repeat("0", 40)}, "x", 10); err == nil {
		t.Fatal("a failing grep must not look like an empty result")
	}
	if _, err := f.repo.Search(context.Background(), f.scope, "two\nlines", 10); err == nil {
		t.Fatal("multi-line query accepted")
	}
	if regexp, err := f.repo.Search(context.Background(), f.scope, ".*", 10); err != nil || len(regexp) != 0 {
		t.Fatalf("query must be literal, not a pattern: %#v %v", regexp, err)
	}
}

func TestSearchCapsHitsPerFileAndTotal(t *testing.T) {
	f := newFixture(t)
	write(t, f.repo.Root, "many.txt", strings.Repeat("token\n", 50))
	for i := 0; i < 5; i++ {
		write(t, f.repo.Root, "f"+string(rune('a'+i))+".txt", "token\ntoken\n")
	}
	runGit(t, f.repo.Root, "add", "-A")
	runGit(t, f.repo.Root, "commit", "-qm", "many")
	scope := f.scope
	scope.HeadSHA = runGit(t, f.repo.Root, "rev-parse", "HEAD")
	hits, err := f.repo.Search(context.Background(), scope, "token", 7)
	if err != nil || len(hits) != 7 {
		t.Fatalf("hits=%d err=%v", len(hits), err)
	}
	perFile := map[string]int{}
	for _, hit := range hits {
		if perFile[hit.Path]++; perFile[hit.Path] > maxHitsPerFile {
			t.Fatalf("%s returned more than %d hits", hit.Path, maxHitsPerFile)
		}
	}
}

func TestBlameAndLogReportCommitsWithoutEmail(t *testing.T) {
	f := newFixture(t)
	blame, err := f.repo.Blame(context.Background(), f.scope, "pkg/a.go", 1, 3)
	if err != nil || len(blame) != 3 {
		t.Fatalf("blame = %#v %v", blame, err)
	}
	if blame[0].Summary != "first commit" || blame[2].Summary != "second: call refund" || blame[2].Author != "Tess Tester" || blame[2].Line != 3 {
		t.Fatalf("blame lines: %#v", blame)
	}
	commits, err := f.repo.Log(context.Background(), f.scope, "pkg/a.go", 10)
	if err != nil || len(commits) != 2 || commits[0].Summary != "second: call refund" || commits[1].SHA != f.first {
		t.Fatalf("log = %#v %v", commits, err)
	}
	for _, line := range blame {
		if strings.Contains(line.Author+line.Summary, "example.com") {
			t.Fatal("author email leaked")
		}
	}
	if clamped, err := f.repo.Blame(context.Background(), f.scope, "pkg/a.go", 1, 99); err != nil || len(clamped) != 3 {
		t.Fatalf("an end past the file should clamp: %d lines, %v", len(clamped), err)
	}
	if _, err := f.repo.Blame(context.Background(), f.scope, "pkg/a.go", 10, 12); err == nil {
		t.Fatal("a start past the end of the file was accepted")
	}
}

func TestListFilesMarksDirectoriesAndRejectsMissingOnes(t *testing.T) {
	f := newFixture(t)
	root, err := f.repo.ListFiles(context.Background(), f.scope, "")
	if err != nil || strings.Join(root, ",") != ".gitattributes,b.txt,big.txt,bin.dat,d:ir/,link.go,pkg/" {
		t.Fatalf("root = %v %v", root, err)
	}
	pkg, err := f.repo.ListFiles(context.Background(), f.scope, "pkg")
	if err != nil || len(pkg) != 1 || pkg[0] != "a.go" {
		t.Fatalf("pkg = %v %v", pkg, err)
	}
	if _, err := f.repo.ListFiles(context.Background(), f.scope, "nope"); err == nil {
		t.Fatal("missing directory listed")
	}
	if _, err := f.repo.ListFiles(context.Background(), f.scope, "../.."); err == nil {
		t.Fatal("escaping directory listed")
	}
}

func TestGitRepoPlugsIntoTheToolbox(t *testing.T) {
	f := newFixture(t)
	box := Toolbox{Files: f.repo, Lister: f.repo, Search: f.repo, History: f.repo}
	names := []string{}
	for _, spec := range box.Specs() {
		names = append(names, spec.Name)
	}
	if strings.Join(names, ",") != "read_file,list_files,search_code,git_blame,git_log" {
		t.Fatalf("specs = %v", names)
	}
	result, err := box.Dispatch(context.Background(), f.scope, Call{Name: ListFiles, Arguments: []byte(`{"directory":"pkg"}`)})
	if err != nil || !strings.Contains(result.Content, "a.go") {
		t.Fatalf("list_files: %#v %v", result, err)
	}
	if _, err := box.Dispatch(context.Background(), f.scope, Call{Name: ListFiles}); err != nil {
		t.Fatalf("list_files with no arguments: %v", err)
	}
	if f.ran() {
		t.Fatal("a tool call ran repository-configured code")
	}
}
