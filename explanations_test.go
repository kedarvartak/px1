package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseStaticExplanationsValidatesAndSorts(t *testing.T) {
	head := strings.Repeat("a", 40)
	input := `{
  "version": 1,
  "revision": "` + head + `",
  "explanations": [
    {"id":"second","path":"web/app.js","lineStart":20,"lineEnd":22,"title":"Second change","summary":"Explains the later block."},
    {"id":"first","path":"api/users.go","lineStart":4,"lineEnd":4,"title":"First change","summary":"Explains the earlier block."}
  ]
}`
	got, err := parseStaticExplanations([]byte(input), head, map[string]bool{
		"api/users.go": true,
		"web/app.js":   true,
	}, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "first" || got[1].ID != "second" {
		t.Fatalf("explanations = %#v", got)
	}
}

func TestParseStaticExplanationsRejectsStaleRevision(t *testing.T) {
	head := strings.Repeat("a", 40)
	input := `{"version":1,"revision":"` + strings.Repeat("b", 40) + `","explanations":[]}`
	if _, err := parseStaticExplanations([]byte(input), head, nil, "fixture"); err == nil || !strings.Contains(err.Error(), "revision must match") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseStaticExplanationsRejectsUnknownAndUnsafePaths(t *testing.T) {
	head := strings.Repeat("a", 40)
	for _, path := range []string{"unchanged.go", "../secret", `/absolute.go`, `dir\\file.go`} {
		input := staticExplanationFile{
			Version:  1,
			Revision: head,
			Explanations: []staticExplanation{{
				ID: "item", Path: path, LineStart: 1, LineEnd: 1, Title: "Change", Summary: "Summary",
			}},
		}
		body, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := parseStaticExplanations(body, head, map[string]bool{"known.go": true}, "fixture"); err == nil || !strings.Contains(err.Error(), "path must name a changed file") {
			t.Errorf("path %q: error = %v", path, err)
		}
	}
}

func TestGenerateStaticReviewIncludesExternalExplanations(t *testing.T) {
	root := t.TempDir()
	runGitTest(t, root, "init")
	runGitTest(t, root, "config", "user.email", "test@example.com")
	runGitTest(t, root, "config", "user.name", "px1 test")
	writeExportTestFile(t, root, "api/users.ts", "export const load = () => true\n")
	runGitTest(t, root, "add", "api/users.ts")
	runGitTest(t, root, "commit", "-m", "base")
	base := strings.TrimSpace(runGitTest(t, root, "rev-parse", "HEAD"))

	writeExportTestFile(t, root, "api/users.ts", "export const load = () => true\nexport const get = () => fetch('/users')\n")
	runGitTest(t, root, "add", "api/users.ts")
	runGitTest(t, root, "commit", "-m", "head")
	head := strings.TrimSpace(runGitTest(t, root, "rev-parse", "HEAD"))

	explanationsPath := filepath.Join(t.TempDir(), "explanations.json")
	body, err := json.Marshal(staticExplanationFile{
		Version:  1,
		Revision: head,
		Explanations: []staticExplanation{{
			ID: "network-call", Path: "api/users.ts", LineStart: 2, LineEnd: 2,
			Title: "Adds a user request", Summary: "This line fetches user data from the API.",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(explanationsPath, body, 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := generateStaticReviewWithOptions(root, base, head, staticReviewOptions{ExplanationsFile: explanationsPath})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Explanations) != 1 || report.Explanations[0].ID != "network-call" {
		t.Fatalf("explanations = %#v", report.Explanations)
	}

	out := t.TempDir()
	if err := writeStaticReviewReport(out, report); err != nil {
		t.Fatal(err)
	}
	html, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"AI explanations", "Adds a user request", "This line fetches user data from the API.", "aria-expanded"} {
		if !strings.Contains(string(html), want) {
			t.Errorf("report missing %q", want)
		}
	}
}
