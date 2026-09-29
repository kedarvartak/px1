package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerificationReportLoad(t *testing.T) {
	root := t.TempDir()
	m := newVerificationManager(root)

	empty, err := m.Load(strings.Repeat("a", 40))
	if err != nil {
		t.Fatalf("missing report should not fail: %v", err)
	}
	if empty.Available || len(empty.Checks) != 0 {
		t.Fatalf("unexpected missing-report response: %+v", empty)
	}

	head := strings.Repeat("a", 40)
	report := verificationReport{
		Source:   "github-actions",
		Revision: head,
		URL:      "https://github.com/example/project/actions/runs/42",
		Checks: []verificationCheck{
			{Name: "go test", Status: "passed", URL: "https://github.com/example/project/actions/runs/42/job/7"},
		},
	}
	b, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".px1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".px1", "verification.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := m.Load(head)
	if err != nil {
		t.Fatalf("valid report failed: %v", err)
	}
	if !loaded.Available || loaded.Source != "github-actions" || len(loaded.Checks) != 1 || loaded.Checks[0].Status != "passed" {
		t.Fatalf("unexpected valid-report response: %+v", loaded)
	}

	stale, err := m.Load(strings.Repeat("b", 40))
	if err != nil {
		t.Fatalf("mismatched report should be a displayable error: %v", err)
	}
	if stale.Available || !strings.Contains(stale.Error, "this review is for") {
		t.Fatalf("expected revision mismatch, got %+v", stale)
	}
}

func TestVerificationReportRejectsUntrustedValues(t *testing.T) {
	head := strings.Repeat("a", 40)
	checks := []verificationCheck{{Name: "tests", Status: "passed", URL: "http://localhost/job"}}
	err := validateVerificationReport(verificationReport{Source: "local", Revision: head, Checks: checks}, head)
	if err == nil || !strings.Contains(err.Error(), "github-actions") {
		t.Fatalf("expected source validation error, got %v", err)
	}
	err = validateVerificationReport(verificationReport{Source: "github-actions", Revision: head, Checks: checks}, head)
	if err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("expected URL validation error, got %v", err)
	}
}
