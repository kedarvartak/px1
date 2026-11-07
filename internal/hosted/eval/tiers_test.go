package eval

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func seededBenchmark() Dataset {
	cases := make([]Case, 100)
	for i := range cases {
		labels := []Label{{Path: "a.go", BugClass: "logic", StartLine: 5, EndLine: 5}}
		crossFile := i < 40
		if crossFile {
			labels = append(labels, Label{Path: "b.go", BugClass: "logic", StartLine: 9, EndLine: 9})
		}
		cases[i] = Case{ID: fmt.Sprintf("seed-%03d", i), Kind: "seeded", Repository: "https://github.com/acme/repo", PullRequestURL: "https://github.com/acme/repo/pull/1",
			BaseSHA: strings.Repeat("a", 40), HeadSHA: strings.Repeat("b", 40), MutationID: "m", CrossFile: crossFile, Labels: labels}
	}
	return Dataset{Version: 1, Cases: cases}
}

// reviewer finds the a.go label on the first `hits` cases, adds `noise`
// unmatched findings per case, and charges `cost` per case.
func reviewer(hits, noise int, cost int64) Reviewer {
	return reviewerFunc(func(_ context.Context, c Case) (ReviewResult, error) {
		var n int
		fmt.Sscanf(c.ID, "seed-%d", &n)
		var findings []Finding
		if n < hits {
			findings = append(findings, Finding{Path: "a.go", BugClass: "logic", StartLine: 5, EndLine: 5})
		}
		for i := 0; i < noise; i++ {
			findings = append(findings, Finding{Path: "z.go", BugClass: "style", StartLine: i + 1, EndLine: i + 1})
		}
		return ReviewResult{Findings: findings, CostMicrousd: cost}, nil
	})
}

func TestTierGatePassesAtExactlyTheThresholds(t *testing.T) {
	result, err := GateTierLaunch(context.Background(), seededBenchmark(), reviewer(50, 0, 100), reviewer(60, 0, 300))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed || result.CostMultiple != 3 || result.RecallLift < 0.199 {
		t.Fatalf("result = %#v", result)
	}
}

func TestTierGateRejectsEachShortfall(t *testing.T) {
	cases := map[string]struct {
		candidate Reviewer
		reason    string
	}{
		"small lift":    {reviewer(59, 0, 200), "recall lift"},
		"too expensive": {reviewer(80, 0, 301), "cost is 3.01×"},
		"noisy recall":  {reviewer(80, 2, 200), "precision dropped"},
	}
	for name, tc := range cases {
		result, err := GateTierLaunch(context.Background(), seededBenchmark(), reviewer(50, 0, 100), tc.candidate)
		if err != nil {
			t.Fatal(err)
		}
		if result.Passed || !strings.Contains(strings.Join(result.Reasons, "; "), tc.reason) {
			t.Fatalf("%s: %#v", name, result)
		}
	}
}

func TestTierGateFailsClosedWhenItCannotMeasure(t *testing.T) {
	if result, _ := GateTierLaunch(context.Background(), seededBenchmark(), reviewer(0, 0, 100), reviewer(90, 0, 100)); result.Passed {
		t.Fatal("zero base recall passed")
	}
	if result, _ := GateTierLaunch(context.Background(), seededBenchmark(), reviewer(50, 0, 0), reviewer(90, 0, 100)); result.Passed {
		t.Fatal("zero base cost passed")
	}
	if result := CompareTiers(Metrics{Cases: 100, Recall: .5, CostMicrousd: 1}, Metrics{Cases: 99, Recall: .9, CostMicrousd: 1}); result.Passed {
		t.Fatal("different case sets passed")
	}
	small := seededBenchmark()
	small.Cases = small.Cases[:20]
	if _, err := GateTierLaunch(context.Background(), small, reviewer(10, 0, 1), reviewer(20, 0, 1)); err == nil {
		t.Fatal("gate ran on a partial benchmark")
	}
}
