package eval

import (
	"strings"
	"testing"
	"time"
)

var (
	shaA = strings.Repeat("a", 40)
	shaB = strings.Repeat("b", 40)
)

func testRepeatDataset() RepeatDataset {
	return RepeatDataset{Version: 1, Cases: []RepeatCase{
		{ID: "seed-1", Kind: "seeded", BaseSHA: shaA, HeadSHA: shaB, Labels: []Label{{Path: "a.ts", BugClass: "logic", StartLine: 10, EndLine: 12}}},
		{ID: "seed-2", Kind: "seeded", BaseSHA: shaA, HeadSHA: shaB, Labels: []Label{{Path: "b.ts", BugClass: "logic", StartLine: 5, EndLine: 5}}},
		{ID: "clean-1", Kind: "clean", BaseSHA: shaA, HeadSHA: shaB},
	}}
}

func find(path string, start, end int) Finding {
	return Finding{Path: path, StartLine: start, EndLine: end}
}

func TestScoreRepeatMeasuresRecallPrecisionNoiseAndVariance(t *testing.T) {
	results := []RunResult{
		// run 1: finds both bugs, one extra finding on seed-1, noise on the clean case
		{CaseID: "seed-1", Run: 1, Findings: []Finding{find("a.ts", 11, 11), find("a.ts", 90, 90)}, CostMicrousd: 1000, Latency: 10 * time.Second},
		{CaseID: "seed-2", Run: 1, Findings: []Finding{find("b.ts", 5, 5)}, CostMicrousd: 1000, Latency: 20 * time.Second},
		{CaseID: "clean-1", Run: 1, Findings: []Finding{find("c.ts", 1, 1)}, CostMicrousd: 1000, Latency: 30 * time.Second},
		// run 2: misses seed-2 entirely
		{CaseID: "seed-1", Run: 2, Findings: []Finding{find("a.ts", 10, 10)}, CostMicrousd: 1000, Latency: 10 * time.Second},
		{CaseID: "seed-2", Run: 2, CostMicrousd: 1000, Latency: 20 * time.Second},
		{CaseID: "clean-1", Run: 2, CostMicrousd: 1000, Latency: 30 * time.Second},
	}
	report, err := ScoreRepeat(testRepeatDataset(), results, MatchOptions{LineSlack: 0})
	if err != nil {
		t.Fatal(err)
	}
	if report.ScoredRuns != 6 || report.Recall != 0.75 || report.RecallMax != 1 || report.RecallMin != 0.5 {
		t.Fatalf("recall=%v min=%v max=%v runs=%d", report.Recall, report.RecallMin, report.RecallMax, report.ScoredRuns)
	}
	// bug-case findings: 4 (a.ts:11, a.ts:90, b.ts:5, a.ts:10); 3 matched.
	if report.Precision != 0.75 {
		t.Fatalf("precision=%v", report.Precision)
	}
	if report.NoisePerCleanRun != 0.5 {
		t.Fatalf("clean noise=%v", report.NoisePerCleanRun)
	}
	if report.FlakyLabels != 1 || report.MissedLabels != 0 || report.Labels != 2 {
		t.Fatalf("flaky=%d missed=%d labels=%d", report.FlakyLabels, report.MissedLabels, report.Labels)
	}
	if report.CostMicrousd != 6000 || report.MedianLatency != 20*time.Second {
		t.Fatalf("cost=%d latency=%v", report.CostMicrousd, report.MedianLatency)
	}
	if got := report.Cases[1].Labels[0]; got.Caught != 1 {
		t.Fatalf("seed-2 caught %d of 2 runs", got.Caught)
	}
	if report.Cases[0].Flaky() != 0 || report.Cases[1].Flaky() != 1 {
		t.Fatal("seed-1 is stable, seed-2 is flaky")
	}
}

func TestScoreRepeatLeavesFailedRunsOutOfEveryScore(t *testing.T) {
	results := []RunResult{
		{CaseID: "seed-1", Run: 1, Findings: []Finding{find("a.ts", 10, 10)}},
		{CaseID: "seed-1", Run: 2, Error: "provider outage"},
		{CaseID: "seed-2", Run: 1, Error: "provider outage"},
	}
	report, err := ScoreRepeat(testRepeatDataset(), results, MatchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if report.FailedRuns != 2 || report.ScoredRuns != 1 {
		t.Fatalf("failed=%d scored=%d", report.FailedRuns, report.ScoredRuns)
	}
	// seed-2 had no scored run, so it is neither missed nor flaky.
	if report.MissedLabels != 0 || report.FlakyLabels != 0 || report.Recall != 1 {
		t.Fatalf("an outage must not look like a miss: %+v", report)
	}
}

func TestMatchOptionsLineSlackAndBugClass(t *testing.T) {
	label := Label{Path: "a.ts", BugClass: "logic", StartLine: 10, EndLine: 12}
	if (MatchOptions{}).matches(find("a.ts", 14, 14), label) {
		t.Fatal("no slack: line 14 is outside 10-12")
	}
	if !(MatchOptions{LineSlack: 3}).matches(find("a.ts", 14, 14), label) {
		t.Fatal("slack 3 should reach line 14")
	}
	if (MatchOptions{LineSlack: 3}).matches(find("a.ts", 16, 16), label) {
		t.Fatal("slack 3 must not reach line 16")
	}
	if (MatchOptions{LineSlack: 3}).matches(find("other.ts", 11, 11), label) {
		t.Fatal("a different file never matches")
	}
	f := Finding{Path: "a.ts", BugClass: "security", StartLine: 11, EndLine: 11}
	if (MatchOptions{RequireClass: true}).matches(f, label) || !(MatchOptions{}).matches(f, label) {
		t.Fatal("class is checked only when required")
	}
}

func TestScoreRepeatReportsNeverFoundBugs(t *testing.T) {
	report, err := ScoreRepeat(testRepeatDataset(), []RunResult{
		{CaseID: "seed-1", Run: 1}, {CaseID: "seed-1", Run: 2},
	}, MatchOptions{})
	if err != nil || report.MissedLabels != 1 || report.Recall != 0 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	if _, err := ScoreRepeat(testRepeatDataset(), []RunResult{{CaseID: "nope", Run: 1}}, MatchOptions{}); err == nil {
		t.Fatal("a result for an unknown case must be rejected")
	}
}

func TestRepeatDatasetValidation(t *testing.T) {
	good := `{"version":1,"cases":[{"id":"c","kind":"clean","base_sha":"` + shaA + `","head_sha":"` + shaB + `"}]}`
	if _, err := DecodeRepeat([]byte(good)); err != nil {
		t.Fatal(err)
	}
	bad := map[string]string{
		"clean with labels": `{"version":1,"cases":[{"id":"c","kind":"clean","base_sha":"` + shaA + `","head_sha":"` + shaB + `","labels":[{"path":"a","bug_class":"x","start_line":1,"end_line":1}]}]}`,
		"seeded no labels":  `{"version":1,"cases":[{"id":"c","kind":"seeded","base_sha":"` + shaA + `","head_sha":"` + shaB + `"}]}`,
		"short sha":         `{"version":1,"cases":[{"id":"c","kind":"clean","base_sha":"abc","head_sha":"` + shaB + `"}]}`,
		"same sha":          `{"version":1,"cases":[{"id":"c","kind":"clean","base_sha":"` + shaA + `","head_sha":"` + shaA + `"}]}`,
		"unknown kind":      `{"version":1,"cases":[{"id":"c","kind":"weird","base_sha":"` + shaA + `","head_sha":"` + shaB + `"}]}`,
		"unknown field":     `{"version":1,"cases":[{"id":"c","kind":"clean","base_sha":"` + shaA + `","head_sha":"` + shaB + `","source":"code"}]}`,
		"no cases":          `{"version":1,"cases":[]}`,
		"duplicate id":      `{"version":1,"cases":[{"id":"c","kind":"clean","base_sha":"` + shaA + `","head_sha":"` + shaB + `"},{"id":"c","kind":"clean","base_sha":"` + shaA + `","head_sha":"` + shaB + `"}]}`,
		"bad label range":   `{"version":1,"cases":[{"id":"c","kind":"seeded","base_sha":"` + shaA + `","head_sha":"` + shaB + `","labels":[{"path":"a","bug_class":"x","start_line":5,"end_line":2}]}]}`,
	}
	for name, input := range bad {
		if _, err := DecodeRepeat([]byte(input)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestRepeatMarkdownStatesSampleSizeAndFlakiness(t *testing.T) {
	report, _ := ScoreRepeat(testRepeatDataset(), []RunResult{
		{CaseID: "seed-1", Run: 1, Findings: []Finding{find("a.ts", 10, 10)}},
		{CaseID: "seed-1", Run: 2},
	}, MatchOptions{})
	md := report.Markdown()
	for _, want := range []string{"2 scored runs", "1/2", "Unstable bugs", "Recall"} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown lacks %q:\n%s", want, md)
		}
	}
	if empty := (RepeatReport{}).Markdown(); !strings.Contains(empty, "Nothing was scored") {
		t.Fatal(empty)
	}
}
