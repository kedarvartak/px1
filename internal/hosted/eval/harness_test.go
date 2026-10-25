package eval

import (
	"context"
	"strings"
	"testing"
	"time"
)

type reviewerFunc func(context.Context, Case) (ReviewResult, error)

func (f reviewerFunc) Review(ctx context.Context, c Case) (ReviewResult, error) { return f(ctx, c) }

func TestRunMeasuresPrecisionRecallCostAndMedianLatency(t *testing.T) {
	cases := []Case{}
	for i := 0; i < 3; i++ {
		cases = append(cases, Case{ID: string(rune('a' + i)), Kind: "seeded", Repository: "https://github.com/acme/repo", PullRequestURL: "https://github.com/acme/repo/pull/1", BaseSHA: strings.Repeat("a", 40), HeadSHA: strings.Repeat("b", 40), MutationID: "m", Labels: []Label{{Path: "a.go", BugClass: "logic", StartLine: 5, EndLine: 5}}})
	}
	metrics, err := Run(context.Background(), Dataset{Version: 1, Cases: cases}, reviewerFunc(func(_ context.Context, c Case) (ReviewResult, error) {
		return ReviewResult{Findings: []Finding{{Path: "a.go", BugClass: "logic", StartLine: 5, EndLine: 5}, {Path: "b.go", BugClass: "logic", StartLine: 1, EndLine: 1}}, CostMicrousd: 10, Latency: time.Duration(c.ID[0]-'a'+1) * time.Second}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Precision != .5 || metrics.Recall != 1 || metrics.CostMicrousd != 30 || metrics.MedianLatency != 2*time.Second {
		t.Fatalf("metrics = %#v", metrics)
	}
}

func TestRunDoesNotMatchWrongBugClassAtSameLine(t *testing.T) {
	benchmark := Case{ID: "wrong-class", Kind: "seeded", Repository: "https://github.com/acme/repo", PullRequestURL: "https://github.com/acme/repo/pull/1", BaseSHA: strings.Repeat("a", 40), HeadSHA: strings.Repeat("b", 40), MutationID: "m", Labels: []Label{{Path: "a.go", BugClass: "auth", StartLine: 5, EndLine: 5}}}
	metrics, err := Run(context.Background(), Dataset{Version: 1, Cases: []Case{benchmark}}, reviewerFunc(func(context.Context, Case) (ReviewResult, error) {
		return ReviewResult{Findings: []Finding{{Path: "a.go", BugClass: "style", StartLine: 5, EndLine: 5}}}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Precision != 0 || metrics.Recall != 0 {
		t.Fatalf("wrong bug class matched: %#v", metrics)
	}
}

func TestGateRegressionBlocksThreePointQualityDrop(t *testing.T) {
	baseline := Metrics{Precision: .8, Recall: .7}
	if GateRegression(Metrics{Precision: .78, Recall: .68}, baseline, .03) != nil {
		t.Fatal("blocked acceptable change")
	}
	if GateRegression(Metrics{Precision: .75, Recall: .7}, baseline, .03) == nil {
		t.Fatal("accepted precision regression")
	}
}
