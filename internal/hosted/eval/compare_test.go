package eval

import (
	"math"
	"strings"
	"testing"
)

func TestFisherExactKnownValues(t *testing.T) {
	cases := []struct {
		a, b, c, d int
		want       float64
	}{
		{3, 0, 0, 3, 0.1}, // 3/3 against 0/3: the best a three-run test can show
		{1, 2, 1, 2, 1},   // identical rates
		{3, 0, 3, 0, 1},   // identical
		{2, 1, 0, 3, 0.4}, // 2/3 against 0/3 is still noise
		{10, 0, 0, 10, 1.08e-5},
		{0, 0, 0, 0, 1},
	}
	for _, c := range cases {
		got := fisherExact(c.a, c.b, c.c, c.d)
		if math.Abs(got-c.want) > math.Max(1e-9, c.want*0.01) {
			t.Errorf("fisherExact(%d,%d,%d,%d) = %v, want %v", c.a, c.b, c.c, c.d, got, c.want)
		}
	}
}

func reportFor(caught []int, runs int, noise float64) RepeatReport {
	var r RepeatReport
	r.NoisePerCleanRun = noise
	for i, n := range caught {
		r.Cases = append(r.Cases, CaseScore{ID: string(rune('a' + i)), Kind: "seeded", Runs: runs,
			Labels: []LabelScore{{Label: Label{Path: "f.ts", StartLine: i + 1, EndLine: i + 1}, Caught: n}}})
		r.ScoredRuns += runs
	}
	return r
}

func TestCompareRepeatCallsOnlyClearDifferencesReal(t *testing.T) {
	before := reportFor([]int{3, 0, 2, 1}, 3, 0)
	after := reportFor([]int{3, 3, 0, 1}, 3, 0)
	c, err := CompareRepeat(before, after)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"a": "same", "b": "better", "c": "same", "d": "same"}
	for _, l := range c.Labels {
		if l.Verdict != want[l.CaseID] {
			t.Errorf("%s: %d/%d -> %d/%d verdict %s (p=%.2f), want %s", l.CaseID, l.BeforeCaught, l.BeforeRuns, l.AfterCaught, l.AfterRuns, l.Verdict, l.P, want[l.CaseID])
		}
	}
	if c.Improved != 1 || c.Regressed != 0 || c.Same != 3 {
		t.Fatalf("%+v", c)
	}
}

func TestCompareRepeatPooledAndRegression(t *testing.T) {
	before := reportFor([]int{3, 3, 3, 3, 3, 3}, 3, 0)
	after := reportFor([]int{0, 1, 0, 1, 0, 1}, 3, 0)
	c, err := CompareRepeat(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if c.PooledVerdict != "worse" || c.Regressed < 1 {
		t.Fatalf("pooled=%s regressed=%d", c.PooledVerdict, c.Regressed)
	}
	md := c.Markdown()
	for _, want := range []string{"Overall: worse", "clearly worse", "Fisher"} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown lacks %q:\n%s", want, md)
		}
	}
}

func TestCompareRepeatSkipsUnscoredLabelsAndRejectsMismatches(t *testing.T) {
	before := reportFor([]int{3, 3}, 3, 0)
	after := reportFor([]int{3, 3}, 3, 0)
	after.Cases[1].Runs = 0 // every run failed
	c, err := CompareRepeat(before, after)
	if err != nil || len(c.Labels) != 1 {
		t.Fatalf("labels=%d err=%v", len(c.Labels), err)
	}
	if _, err := CompareRepeat(reportFor([]int{1}, 3, 0), reportFor([]int{1, 1}, 3, 0)); err == nil {
		t.Fatal("different datasets must be rejected")
	}
	renamed := reportFor([]int{1}, 3, 0)
	renamed.Cases[0].ID = "other"
	if _, err := CompareRepeat(reportFor([]int{1}, 3, 0), renamed); err == nil {
		t.Fatal("different case IDs must be rejected")
	}
}

func TestComparisonWarnsWhenCleanNoiseRises(t *testing.T) {
	c, _ := CompareRepeat(reportFor([]int{1}, 3, 0), reportFor([]int{3}, 3, 1.5))
	if !strings.Contains(c.Markdown(), "more false alarms") {
		t.Fatal(c.Markdown())
	}
}
