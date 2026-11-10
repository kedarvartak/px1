package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"px1/internal/hosted/agent"
	"px1/internal/hosted/eval"
	hostedreview "px1/internal/hosted/review"
)

func writeEvalCases(t *testing.T, base, head string) string {
	t.Helper()
	cases := map[string]any{"version": 1, "cases": []map[string]any{
		{"id": "found", "kind": "seeded", "base_sha": base, "head_sha": head, "labels": []map[string]any{{"path": "src/refund.ts", "bug_class": "logic", "start_line": 3, "end_line": 3}}},
		{"id": "never-found", "kind": "seeded", "base_sha": base, "head_sha": head, "labels": []map[string]any{{"path": "src/other.ts", "bug_class": "logic", "start_line": 1, "end_line": 1}}},
		{"id": "clean", "kind": "clean", "base_sha": base, "head_sha": head},
	}}
	data, _ := json.Marshal(cases)
	path := filepath.Join(t.TempDir(), "cases.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func evalArgs(f *fakeOpenAI, cases, root, out string, extra ...string) []string {
	return append([]string{"--cases", cases, "--root", root, "--out", out, "--model", "test-model", "--price-input", "1", "--price-output", "4", "--endpoint", f.server.URL + "/v1/responses"}, extra...)
}

func readEvalResults(t *testing.T, out string) []eval.RunResult {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(out, "results.json"))
	if err != nil {
		t.Fatal(err)
	}
	var results []eval.RunResult
	if err := json.Unmarshal(data, &results); err != nil {
		t.Fatal(err)
	}
	return results
}

func TestEvalRunReviewsEveryCaseEveryRunAndScoresThem(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", fakeKey)
	f := newFakeOpenAI(t)
	root, base, head := newAgenticRepo(t, cleanHead)
	cases := writeEvalCases(t, base, head)
	out := filepath.Join(t.TempDir(), "eval")
	var stdout bytes.Buffer
	if err := runEvalRun(evalArgs(f, cases, root, out, "--yes", "--runs", "2"), &stdout); err != nil {
		t.Fatalf("%v\n%s", err, stdout.String())
	}
	results := readEvalResults(t, out)
	if len(results) != 6 {
		t.Fatalf("3 cases x 2 runs = 6 reviews, got %d", len(results))
	}
	for i, r := range results {
		// run-major order: every case once before any case twice
		if want := i/3 + 1; r.Run != want {
			t.Fatalf("result %d is run %d, want %d", i, r.Run, want)
		}
		if r.Error != "" || r.CostMicrousd <= 0 || len(r.Findings) != 1 {
			t.Fatalf("result %+v", r)
		}
	}
	report, err := os.ReadFile(filepath.Join(out, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"6 scored runs", "2/2", "0/2", "never found", "Noise on clean changes"} {
		if !strings.Contains(string(report), want) {
			t.Fatalf("report lacks %q:\n%s", want, report)
		}
	}
	info, _ := os.Stat(filepath.Join(out, "results.json"))
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("results.json is %v, want owner-only", info.Mode().Perm())
	}
}

func TestEvalRunNeedsConsentAndDryRunSendsNothing(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", fakeKey)
	f := newFakeOpenAI(t)
	root, base, head := newAgenticRepo(t, cleanHead)
	cases := writeEvalCases(t, base, head)
	var dry bytes.Buffer
	if err := runEvalRun(evalArgs(f, cases, root, t.TempDir(), "--dry-run"), &dry); err != nil || !strings.Contains(dry.String(), "Dry run: nothing was sent") || !strings.Contains(dry.String(), "never-found") {
		t.Fatalf("err=%v\n%s", err, dry.String())
	}
	if err := runEvalRun(evalArgs(f, cases, root, t.TempDir()), &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("without --yes: %v", err)
	}
	t.Setenv("OPENAI_API_KEY", "")
	if err := runEvalRun(evalArgs(f, cases, root, t.TempDir(), "--yes"), &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Fatalf("without a key: %v", err)
	}
	if f.requests != 0 {
		t.Fatalf("%d requests were sent", f.requests)
	}
}

func TestEvalRunStopsAtTheTotalCapAndResumes(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", fakeKey)
	f := newFakeOpenAI(t)
	root, base, head := newAgenticRepo(t, cleanHead)
	cases := writeEvalCases(t, base, head)
	out := filepath.Join(t.TempDir(), "eval")
	// Each review may cost up to $0.50 and the whole run $0.50: only one review fits.
	var first bytes.Buffer
	if err := runEvalRun(evalArgs(f, cases, root, out, "--yes", "--runs", "1", "--max-cost", "0.5", "--max-total-cost", "0.5"), &first); err != nil {
		t.Fatal(err)
	}
	if got := len(readEvalResults(t, out)); got != 1 {
		t.Fatalf("the cap should allow one review, got %d", got)
	}
	report, _ := os.ReadFile(filepath.Join(out, "report.md"))
	if !strings.Contains(string(report), "total cap") || !strings.Contains(string(report), "--resume") {
		t.Fatalf("the report must say it stopped and how to continue:\n%s", report)
	}
	before := f.requests
	if err := runEvalRun(evalArgs(f, cases, root, out, "--yes", "--runs", "1", "--resume", "--max-cost", "0.5", "--max-total-cost", "5"), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	results := readEvalResults(t, out)
	if len(results) != 3 {
		t.Fatalf("resume should finish all 3, got %d", len(results))
	}
	seen := map[string]bool{}
	for _, r := range results {
		if seen[r.CaseID] {
			t.Fatalf("%s was reviewed twice on resume", r.CaseID)
		}
		seen[r.CaseID] = true
	}
	if f.requests == before {
		t.Fatal("resume made no requests")
	}
}

func TestEvalRunKeepsFailedReviewsOutOfTheScore(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", fakeKey)
	f := newFakeOpenAI(t)
	root, base, head := newAgenticRepo(t, cleanHead)
	cases := writeEvalCases(t, base, head)
	out := filepath.Join(t.TempDir(), "eval")
	// A per-review cap this small stops every review before its first call.
	if err := runEvalRun(evalArgs(f, cases, root, out, "--yes", "--runs", "1", "--max-cost", "0.000001", "--max-total-cost", "1"), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	for _, r := range readEvalResults(t, out) {
		if r.Error == "" {
			t.Fatalf("a review that could not run must be recorded as failed: %+v", r)
		}
	}
	report, _ := os.ReadFile(filepath.Join(out, "report.md"))
	if !strings.Contains(string(report), "Nothing was scored") && !strings.Contains(string(report), "3 failed runs") {
		t.Fatalf("report:\n%s", report)
	}
}

func TestEvalRunRejectsCasesWhoseCommitsAreMissingAndBadFlags(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", fakeKey)
	f := newFakeOpenAI(t)
	root, base, _ := newAgenticRepo(t, cleanHead)
	cases := writeEvalCases(t, base, strings.Repeat("e", 40))
	if err := runEvalRun(evalArgs(f, cases, root, t.TempDir(), "--yes"), &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "is not in") {
		t.Fatalf("missing commit: %v", err)
	}
	if f.requests != 0 {
		t.Fatal("nothing may be sent when a case cannot be resolved")
	}
	good := evalArgs(f, cases, root, t.TempDir())
	for name, extra := range map[string][]string{"runs": {"--runs", "0"}, "total below per-review": {"--max-cost", "2", "--max-total-cost", "1"}, "slack": {"--line-slack", "-1"}} {
		if _, err := parseEvalRunFlags(append(append([]string{}, good...), extra...)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestReviewScopeMarksFailedStepsIncomplete(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", fakeKey)
	f := newFakeOpenAI(t)
	root, base, _ := newAgenticRepo(t, cleanHead)
	out := filepath.Join(t.TempDir(), "run")
	if err := runAgenticReview(agenticArgs(f, root, base, out, "--yes"), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if scope, ok := readScope(out); !ok || !scope.Complete {
		t.Fatalf("a clean run is complete: %+v ok=%v", scope, ok)
	}
}

func TestReviewCompleteIsFalseWhenAStepFailed(t *testing.T) {
	var ok agent.PipelineResult
	ok.Run.Complete = true
	if !reviewComplete(ok) {
		t.Fatal("a finished run is complete")
	}
	sub := agent.PipelineResult{Run: agent.RunResult{Complete: false, Outcomes: map[string]agent.Partial{"g": {}}}}
	if reviewComplete(sub) {
		t.Fatal("a sub-agent that did not finish makes the run incomplete")
	}
	unverified := agent.PipelineResult{Run: agent.RunResult{Complete: true}, Unverified: []hostedreview.Finding{{Path: "a"}}}
	if reviewComplete(unverified) {
		t.Fatal("candidates the verifier could not check make the run incomplete")
	}
}
