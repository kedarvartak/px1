package main

import (
	"bytes"
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
