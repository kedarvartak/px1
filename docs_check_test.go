package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"px1/internal/docs"
)

// The repository's own documentation must pass its own check.
func TestRepositoryDocsPassCheck(t *testing.T) {
	problems, err := docs.Check(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range problems {
		t.Error(problem)
	}
}

func TestDocsCommandRejectsUnknownSubcommand(t *testing.T) {
	var out bytes.Buffer
	if err := runDocs(nil, &out); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("no arguments: %v", err)
	}
	if err := runDocs([]string{"publish"}, &out); err == nil || !strings.Contains(err.Error(), "unknown docs command") {
		t.Fatalf("unknown subcommand: %v", err)
	}
}

func TestDocsCheckCommandReportsProblems(t *testing.T) {
	root := t.TempDir()
	writeExportTestFile(t, root, "docs/PAGE.md", "# no front matter\n")
	var out bytes.Buffer
	err := runDocs([]string{"check", "--root", root}, &out)
	if err == nil || !strings.Contains(out.String(), "docs/PAGE.md:1: missing front matter") {
		t.Fatalf("err=%v out=%q", err, out.String())
	}
}

// Every subcommand's description in the reference is the doc comment of its
// run function, so a command without one would be listed with no explanation.
func TestEveryCommandHasASummary(t *testing.T) {
	commands, err := docs.Commands(".")
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) < 12 {
		t.Fatalf("found only %d commands", len(commands))
	}
	for _, command := range commands {
		if command.Summary == "" {
			t.Errorf("px1 %s has no doc comment on its run function", command.Name)
		}
	}
}

func TestDocsSiteCommandWritesTheSite(t *testing.T) {
	root := t.TempDir()
	writeExportTestFile(t, root, "docs/PAGE.md", "---\ntitle: A page\nsection: guides\nkind: narrative\nstatus: shipped\ncovers: []\nverified_at:\n---\n\n# A page\n")
	out := filepath.Join(t.TempDir(), "site")
	var buf bytes.Buffer
	if err := runDocs([]string{"site", "--root", root}, &buf); err == nil || !strings.Contains(err.Error(), "--out") {
		t.Fatalf("missing --out: %v", err)
	}
	if err := runDocs([]string{"site", "--root", root, "--out", out}, &buf); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"index.html", "PAGE.html", "fonts/plex-sans-var.woff2"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(name))); err != nil {
			t.Error(err)
		}
	}
}

func TestDocsImpactCommand(t *testing.T) {
	root := t.TempDir()
	runGitTest(t, root, "init", "-q")
	runGitTest(t, root, "config", "user.email", "test@example.com")
	runGitTest(t, root, "config", "user.name", "Test")
	writeExportTestFile(t, root, "tool.go", "package main\n")
	writeExportTestFile(t, root, "docs/PAGE.md", "---\ntitle: A page\nsection: guides\nkind: narrative\nstatus: shipped\ncovers:\n  - tool.go\nverified_at:\n---\n\n# A page\n")
	runGitTest(t, root, "add", "-A")
	runGitTest(t, root, "commit", "-qm", "base")
	base := strings.TrimSpace(runGitTest(t, root, "rev-parse", "HEAD"))
	writeExportTestFile(t, root, "tool.go", "package main\n\nvar changed = true\n")
	runGitTest(t, root, "commit", "-qam", "change the tool")

	var out bytes.Buffer
	if err := runDocs([]string{"impact", "--root", root}, &out); err == nil || !strings.Contains(err.Error(), "--base") {
		t.Fatalf("missing --base: %v", err)
	}
	if err := runDocs([]string{"impact", "--root", root, "--base", base}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "| A page (`docs/PAGE.md`) | Needs review | `tool.go` |") {
		t.Fatalf("markdown:\n%s", out.String())
	}

	file := filepath.Join(t.TempDir(), "impact.json")
	if err := runDocs([]string{"impact", "--root", root, "--base", base, "--json", "--out", file}, &out); err != nil {
		t.Fatal(err)
	}
	site := filepath.Join(t.TempDir(), "site")
	if err := runDocs([]string{"site", "--root", root, "--out", site, "--impact", file}, &out); err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(filepath.Join(site, "PAGE.html"))
	if err != nil || !strings.Contains(string(page), `class="impact needs-review"`) {
		t.Fatalf("err=%v; the site does not mark the affected page", err)
	}
	if err := runDocs([]string{"impact", "--root", root, "--base", "no-such-ref"}, &out); err == nil {
		t.Fatal("an unknown base was accepted")
	}
}

// The Pages workflow runs with write access on pull_request_target. Its docs
// step must use the base checkout as the tool, read the pull request only as
// files, and never be able to stop the review report from publishing.
func TestReviewWorkflowBuildsDocsFromTheTrustedBase(t *testing.T) {
	body, err := os.ReadFile(".github/workflows/px1-review-report.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	start := strings.Index(text, "- name: Build the documentation site")
	end := strings.Index(text, "- name: Configure GitHub Pages")
	if start < 0 || end < start {
		t.Fatal("the docs step must come before the Pages upload")
	}
	if strings.Index(text, "- name: Preserve historical review reports") > start {
		t.Fatal("the docs step must come after the report history is validated")
	}
	step := text[start:end]
	for _, want := range []string{
		"continue-on-error: true",
		`git worktree add --detach "$head_tree" "$HEAD_SHA"`,
		`git merge-base "$BASE_SHA" "$HEAD_SHA"`,
		`go build -o "${RUNNER_TEMP}/px1-docs-tool" .`,
		`docs site --root . --out "${build}/docs"`,
		`docs site --root "$head_tree" --out "${build}/preview" --impact "$impact"`,
		`mv "${build}/preview" "site/docs-preview/${HEAD_SHA}"`,
	} {
		if !strings.Contains(step, want) {
			t.Errorf("docs step missing %q", want)
		}
	}
	// Nothing may be built or run from the pull request's tree.
	for _, banned := range []string{`cd "$head_tree"`, `-C "$head_tree"`, "go run", "npm ", "make "} {
		if strings.Contains(step, banned) {
			t.Errorf("docs step contains %q", banned)
		}
	}
	comment := text[strings.Index(text, "- name: Update the documentation impact comment"):]
	for _, want := range []string{"steps.docs.outputs.built == 'true'", "continue-on-error: true", "'<!-- px1-docs-impact -->'", "body.startsWith(marker)"} {
		if !strings.Contains(comment, want) {
			t.Errorf("comment step missing %q", want)
		}
	}
}

func TestDocsApplyCommand(t *testing.T) {
	root := t.TempDir()
	runGitTest(t, root, "init", "-q")
	runGitTest(t, root, "config", "user.email", "test@example.com")
	runGitTest(t, root, "config", "user.name", "Test")
	writeExportTestFile(t, root, "tool.go", "package main\n\nconst maxFiles = 10\n")
	page := "---\ntitle: A page\nsection: guides\nkind: narrative\nstatus: shipped\ncovers:\n  - tool.go\nverified_at:\n---\n\n# A page\n\n## Limits\n\nAt most 10 files.\n"
	writeExportTestFile(t, root, "docs/PAGE.md", page)
	runGitTest(t, root, "add", "-A")
	runGitTest(t, root, "commit", "-qm", "base")
	base := strings.TrimSpace(runGitTest(t, root, "rev-parse", "HEAD"))
	writeExportTestFile(t, root, "tool.go", "package main\n\nconst maxFiles = 25\n")
	runGitTest(t, root, "commit", "-qam", "raise the limit")

	edits := filepath.Join(t.TempDir(), "edits.json")
	good := `{"page":"docs/PAGE.md","heading":"Limits","body":"At most 25 files.","reason":"The limit rose.","citations":[{"file":"tool.go","quote":"const maxFiles = 25"}]}`
	bad := `{"page":"docs/PAGE.md","heading":"A page","body":"It reads 99 files.","reason":"Invented.","citations":[{"file":"tool.go","quote":"const maxFiles = 99"}]}`
	if err := os.WriteFile(edits, []byte("["+good+","+bad+"]"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err := runDocs([]string{"apply", "--root", root, "--base", base, "--edits", edits, "--dry-run"}, &out)
	if err == nil || !strings.Contains(err.Error(), "1 of 2 edit(s) refused") {
		t.Fatalf("dry run: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "docs/PAGE.md")); string(data) != page {
		t.Fatal("a dry run changed the page")
	}
	out.Reset()
	if err := runDocs([]string{"apply", "--root", root, "--base", base, "--edits", edits}, &out); err == nil {
		t.Fatal("a refused edit did not fail the command")
	}
	for _, want := range []string{"**docs/PAGE.md**, section \"Limits\": The limit rose.", "`tool.go:3`", "the quote is not in tool.go"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	if data, _ := os.ReadFile(filepath.Join(root, "docs/PAGE.md")); string(data) != strings.Replace(page, "10 files", "25 files", 1) {
		t.Fatalf("page after apply:\n%s", data)
	}
	if err := os.WriteFile(edits, []byte(`[{"page":"docs/PAGE.md","surprise":1}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runDocs([]string{"apply", "--root", root, "--base", base, "--edits", edits}, &out); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown field: %v", err)
	}
}
