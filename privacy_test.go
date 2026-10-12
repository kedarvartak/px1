package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivacyCheckBlocksSensitivePathsAndSecretSignatures(t *testing.T) {
	fakeKey := "sk-proj-" + strings.Repeat("a", 32)
	report := staticReviewReport{
		Files: []staticReviewFile{
			{Path: "config/.env.production", Diff: "+OPENAI_API_KEY=" + fakeKey},
			{Path: "src/app.go", Diff: "+package app"},
		},
	}
	b, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	risks := findPrivacyRisks(report, b)
	if len(risks) != 2 {
		t.Fatalf("risks = %#v", risks)
	}
	joined := risks[0].Reason + " " + risks[0].Path + " " + risks[1].Reason + " " + risks[1].Path
	if !strings.Contains(joined, "environment-secret path") || !strings.Contains(joined, "OpenAI API key signature") {
		t.Fatalf("risks = %#v", risks)
	}
	if strings.Contains(joined, "sk-proj-") {
		t.Fatal("privacy findings must not echo a detected secret")
	}
	file := filepath.Join(t.TempDir(), "review.json")
	if err := os.WriteFile(file, b, 0o644); err != nil {
		t.Fatal(err)
	}
	err = runPrivacyCheck([]string{"--review-file", file}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "privacy check blocked report") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), fakeKey) {
		t.Fatal("privacy-check error must not echo a detected secret")
	}
}

func TestPrivacyCheckAllowsTemplatesAndCleanReports(t *testing.T) {
	report := staticReviewReport{Files: []staticReviewFile{{Path: ".env.example", Diff: "+API_KEY=replace-me"}, {Path: "src/app.go", Diff: "+package app"}}}
	b, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if risks := findPrivacyRisks(report, b); len(risks) != 0 {
		t.Fatalf("risks = %#v", risks)
	}
	file := filepath.Join(t.TempDir(), "review.json")
	if err := os.WriteFile(file, b, 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := runPrivacyCheck([]string{"--review-file", file}, &stdout); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "not redaction or access control") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}
