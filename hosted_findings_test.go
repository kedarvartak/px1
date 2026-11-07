package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostedFindingsRequireExactRevisionAndChangedPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "findings.json")
	sha := strings.Repeat("a", 40)
	body := `{"revision":"` + sha + `","findings":[{"path":"api.go","start_line":4,"end_line":4,"severity":"P1","type":"logic","title":"Ignored error","body":"The error is discarded.","evidence":"result is unused","confidence":0.8}]}`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	findings, err := hostedFindingsAtFile(path, sha, map[string]bool{"api.go": true})
	if err != nil || len(findings) != 1 {
		t.Fatalf("findings = %#v, err = %v", findings, err)
	}
	if _, err := hostedFindingsAtFile(path, strings.Repeat("b", 40), map[string]bool{"api.go": true}); err == nil {
		t.Fatal("accepted stale findings")
	}
	if _, err := hostedFindingsAtFile(path, sha, map[string]bool{"other.go": true}); err == nil {
		t.Fatal("accepted unchanged path")
	}
}

func TestHostedFindingsRenderAsSeparateAIItems(t *testing.T) {
	body, err := renderStaticReviewHTML(staticReviewReport{Findings: nil, Verification: verificationResponse{Checks: []verificationCheck{}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"AI finding", "verified AI finding", "chip-finding"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("report missing %q", want)
		}
	}
}

func TestExportAttachesOnlyATraceForTheHeadRevision(t *testing.T) {
	root := t.TempDir()
	runGitTest(t, root, "init")
	runGitTest(t, root, "config", "user.email", "test@example.com")
	runGitTest(t, root, "config", "user.name", "px1 test")
	writeExportTestFile(t, root, "api.go", "package api\n")
	runGitTest(t, root, "add", ".")
	runGitTest(t, root, "commit", "-m", "base")
	base := strings.TrimSpace(runGitTest(t, root, "rev-parse", "HEAD"))
	writeExportTestFile(t, root, "api.go", "package api\n\nvar Ready = true\n")
	runGitTest(t, root, "commit", "-am", "head")
	head := strings.TrimSpace(runGitTest(t, root, "rev-parse", "HEAD"))
	write := func(revision string) string {
		path := filepath.Join(t.TempDir(), "trace.json")
		body := `{"revision":"` + revision + `","tier":"plus","spend":{"input_tokens":0,"output_tokens":0,"cost_microusd":0,"model_calls":1,"tool_calls":0},"events":[{"seq":1,"agent":"planner","kind":"model","name":"planner","elapsed_ms":1}]}`
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	report, err := generateStaticReviewWithOptions(root, base, head, staticReviewOptions{TraceFile: write(head)})
	if err != nil || report.Trace == nil || len(report.Trace.Events) != 1 {
		t.Fatalf("trace not attached: %#v %v", report.Trace, err)
	}
	if _, err := generateStaticReviewWithOptions(root, base, head, staticReviewOptions{TraceFile: write(base)}); err == nil || !strings.Contains(err.Error(), "does not match report head") {
		t.Fatalf("stale trace accepted: %v", err)
	}
	plain, err := generateStaticReview(root, base, head)
	if err != nil || plain.Trace != nil {
		t.Fatalf("reports without a trace must omit it: %#v %v", plain.Trace, err)
	}
}
