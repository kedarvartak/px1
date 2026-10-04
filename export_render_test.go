package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateStaticReviewIncludesAttentionFlags(t *testing.T) {
	root := t.TempDir()
	runGitTest(t, root, "init")
	runGitTest(t, root, "config", "user.email", "test@example.com")
	runGitTest(t, root, "config", "user.name", "test")
	writeExportTestFile(t, root, "main.go", "package main\n")
	writeExportTestFile(t, root, "gone_test.go", "package main\n")
	runGitTest(t, root, "add", ".")
	runGitTest(t, root, "commit", "-m", "base")
	base := strings.TrimSpace(runGitTest(t, root, "rev-parse", "HEAD"))

	writeExportTestFile(t, root, "main.go", "package main\n\nfunc Exported() {}\n")
	writeExportTestFile(t, root, "go.mod", "module example\n")
	runGitTest(t, root, "rm", "-q", "gone_test.go")
	runGitTest(t, root, "add", ".")
	runGitTest(t, root, "commit", "-m", "head")
	head := strings.TrimSpace(runGitTest(t, root, "rev-parse", "HEAD"))

	report, err := generateStaticReview(root, base, head)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]reviewAttentionFlag{}
	for _, flag := range report.Attention {
		got[flag.Rule+" "+flag.Path] = flag
	}
	for _, want := range []string{"public-api main.go", "dependency go.mod", "deleted-test gone_test.go"} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing flag %q in %#v", want, report.Attention)
		}
	}
	if api := got["public-api main.go"]; api.Line != 3 {
		t.Errorf("public-api line = %d, want 3", api.Line)
	}
}

func TestStaticAttentionHandlesRenameAndModeChange(t *testing.T) {
	root := t.TempDir()
	runGitTest(t, root, "init")
	runGitTest(t, root, "config", "user.email", "test@example.com")
	runGitTest(t, root, "config", "user.name", "test")
	writeExportTestFile(t, root, "run.sh", "#!/bin/sh\necho one\necho two\necho three\n")
	runGitTest(t, root, "add", ".")
	runGitTest(t, root, "commit", "-m", "base")
	base := strings.TrimSpace(runGitTest(t, root, "rev-parse", "HEAD"))

	if err := os.MkdirAll(filepath.Join(root, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "mv", "run.sh", "config/run.sh")
	if err := os.Chmod(filepath.Join(root, "config/run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "commit", "-am", "head")
	head := strings.TrimSpace(runGitTest(t, root, "rev-parse", "HEAD"))

	flags, err := staticAttention(root, base, head)
	if err != nil {
		t.Fatal(err)
	}
	rules := map[string]bool{}
	for _, flag := range flags {
		rules[flag.Rule] = true
		if flag.Path != "config/run.sh" {
			t.Errorf("flag on %q, want the renamed path", flag.Path)
		}
	}
	if !rules["configuration"] || !rules["executable-mode"] {
		t.Fatalf("flags = %#v", flags)
	}
}

func TestStaticAttentionIsEmptyNotNull(t *testing.T) {
	root := t.TempDir()
	runGitTest(t, root, "init")
	runGitTest(t, root, "config", "user.email", "test@example.com")
	runGitTest(t, root, "config", "user.name", "test")
	writeExportTestFile(t, root, "notes.txt", "a\n")
	runGitTest(t, root, "add", ".")
	runGitTest(t, root, "commit", "-m", "base")
	base := strings.TrimSpace(runGitTest(t, root, "rev-parse", "HEAD"))
	writeExportTestFile(t, root, "notes.txt", "b\n")
	runGitTest(t, root, "commit", "-am", "head")
	head := strings.TrimSpace(runGitTest(t, root, "rev-parse", "HEAD"))
	flags, err := staticAttention(root, base, head)
	if err != nil || flags == nil || len(flags) != 0 {
		t.Fatalf("flags = %#v, err = %v", flags, err)
	}
}

func TestStaticReviewHTMLIsSelfContained(t *testing.T) {
	report := staticReviewReport{
		Repository:   "demo",
		Files:        []staticReviewFile{},
		RuleHits:     []staticReviewHit{},
		Explanations: []staticExplanation{},
		Attention:    []reviewAttentionFlag{{ID: "attention-1", Rule: "public-api", Title: "</script><b>x", Path: "a.go"}},
		Verification: verificationResponse{Checks: []verificationCheck{}},
	}
	body, err := renderStaticReviewHTML(report)
	if err != nil {
		t.Fatal(err)
	}
	html := string(body)
	for _, banned := range []string{"http://", "https://", "<link", "src=\"", "@import"} {
		if strings.Contains(html, banned) {
			t.Errorf("report references an external resource via %q", banned)
		}
	}
	for _, want := range []string{"font/woff2;base64,", "Report Sans", "Report Mono", `id="report-data"`, `"attention":[{"id":"attention-1"`} {
		if !strings.Contains(html, want) {
			t.Errorf("report missing %q", want)
		}
	}
	// Data containing a closing script tag must not end the data block early.
	if strings.Count(html, "</script>") != 2 {
		t.Errorf("script tags = %d, want 2", strings.Count(html, "</script>"))
	}
}

func TestStaticReviewHTMLToleratesSnapshotWithoutAttention(t *testing.T) {
	report := staticReviewReport{Verification: verificationResponse{Checks: []verificationCheck{}}}
	body, err := renderStaticReviewHTML(report)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"attention":null`) || !strings.Contains(string(body), "Array.isArray(report.attention)") {
		t.Fatal("page must render a report whose attention field is null")
	}
}
