package main

import (
	"strings"
	"testing"
)

func TestSummaryCommentIncludesFindingsRulesVerificationAndArtifact(t *testing.T) {
	report := staticReviewReport{
		Head:         strings.Repeat("a", 40),
		Files:        []staticReviewFile{{Path: "one.go"}, {Path: "two.go"}},
		RuleHits:     []staticReviewHit{{RuleID: "safe-log", Title: "Use shared *logger*", Severity: "error"}, {RuleID: "safe-log", Title: "Use shared *logger*", Severity: "error"}},
		Attention:    []reviewAttentionFlag{{ID: "flag"}},
		Explanations: []staticExplanation{{Title: "note"}},
		Verification: verificationResponse{Available: true, Checks: []verificationCheck{{Status: "passed"}, {Status: "failed"}, {Status: "running"}}},
	}
	body, err := renderSummaryComment(report, "https://example.test/reviews/"+report.Head+"/")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		summaryCommentMarker,
		"Static review for commit `" + report.Head + "`",
		"| Changed files | 2 |",
		"| Rule findings | 2 |",
		"| Attention flags | 1 |",
		"| AI notes | 1 |",
		"1 passed, 1 failed, 1 pending",
		"**Use shared \\*logger\\*** (`safe-log`, error) — 2 findings",
		"[Open the commit-pinned report](https://example.test/reviews/" + report.Head + "/)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("summary missing %q:\n%s", want, body)
		}
	}
}

func TestSummaryCommentRejectsUnpinnedIdentityAndUnsafeURL(t *testing.T) {
	valid := staticReviewReport{Head: strings.Repeat("b", 40)}
	for name, tc := range map[string]struct {
		report staticReviewReport
		url    string
	}{
		"short head":   {report: staticReviewReport{Head: "abc"}, url: "https://example.test/report"},
		"script URL":   {report: valid, url: "javascript:alert(1)"},
		"relative URL": {report: valid, url: "/report"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := renderSummaryComment(tc.report, tc.url); err == nil {
				t.Fatal("accepted invalid summary identity or URL")
			}
		})
	}
}
