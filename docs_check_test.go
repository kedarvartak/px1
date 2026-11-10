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
