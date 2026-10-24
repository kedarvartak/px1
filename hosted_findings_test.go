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
