package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"px1/internal/hosted/eval"
)

func writeResults2(t *testing.T, caught []bool) string {
	t.Helper()
	var results []eval.RunResult
	for run, hit := range caught {
		r := eval.RunResult{CaseID: "found", Run: run + 1}
		if hit {
			r.Findings = []eval.Finding{{Path: "src/refund.ts", StartLine: 3, EndLine: 3}}
		}
		results = append(results, r, eval.RunResult{CaseID: "never-found", Run: run + 1}, eval.RunResult{CaseID: "clean", Run: run + 1})
	}
	data, _ := json.Marshal(results)
	path := filepath.Join(t.TempDir(), "results.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEvalCompareReportsRealAndNoiseDifferences(t *testing.T) {
	cases := writeEvalCases(t, strings.Repeat("a", 40), strings.Repeat("b", 40))
	before := writeResults2(t, []bool{false, false, false})
	after := writeResults2(t, []bool{true, true, true})
	out := filepath.Join(t.TempDir(), "cmp.md")
	var stdout bytes.Buffer
	if err := runEvalCompare([]string{"--cases", cases, "--before", before, "--after", after, "--out", out}, &stdout); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"| found | 0/3 | 3/3 |", "better", "| never-found | 0/3 | 0/3 |", "same"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("missing %q:\n%s", want, stdout.String())
		}
	}
	if saved, err := os.ReadFile(out); err != nil || string(saved) != stdout.String() {
		t.Fatalf("saved report differs: %v", err)
	}
	// A one-run difference is noise.
	var noise bytes.Buffer
	if err := runEvalCompare([]string{"--cases", cases, "--before", writeResults2(t, []bool{true, true, false}), "--after", writeResults2(t, []bool{true, true, true})}, &noise); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(noise.String(), "| found | 2/3 | 3/3 |") || strings.Contains(noise.String(), "| better |") {
		t.Fatalf("a single extra find must not count as better:\n%s", noise.String())
	}
}

func TestEvalCompareValidatesInput(t *testing.T) {
	cases := writeEvalCases(t, strings.Repeat("a", 40), strings.Repeat("b", 40))
	if err := runEvalCompare([]string{"--cases", cases}, &bytes.Buffer{}); err == nil {
		t.Fatal("missing results must be rejected")
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(bad, []byte("not json"), 0o600)
	if err := runEvalCompare([]string{"--cases", cases, "--before", bad, "--after", bad}, &bytes.Buffer{}); err == nil {
		t.Fatal("malformed results must be rejected")
	}
	unknown := writeResults2(t, []bool{true})
	data, _ := os.ReadFile(unknown)
	os.WriteFile(unknown, bytes.Replace(data, []byte(`"found"`), []byte(`"nope"`), 1), 0o600)
	if err := runEvalCompare([]string{"--cases", cases, "--before", unknown, "--after", unknown}, &bytes.Buffer{}); err == nil {
		t.Fatal("results for an unknown case must be rejected")
	}
}
