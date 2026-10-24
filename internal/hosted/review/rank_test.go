package review

import "testing"

func TestRankAndCapFindingsDeduplicatesAndKeepsStrongest(t *testing.T) {
	findings := []Finding{
		{Path: "api/user.go", StartLine: 10, EndLine: 12, Severity: "P2", Confidence: .9, Title: "Missing authorization check"},
		{Path: "api/user.go", StartLine: 11, EndLine: 13, Severity: "P0", Confidence: .7, Title: "Authorization check is missing"},
		{Path: "api/user.go", StartLine: 30, EndLine: 30, Severity: "P1", Confidence: .8, Title: "Missing authorization check"},
	}
	got := RankAndCapFindings(findings, 10)
	if len(got) != 2 || got[0].Severity != "P0" || got[1].StartLine != 30 {
		t.Fatalf("ranked findings = %#v", got)
	}
}

func TestRankAndCapFindingsOrdersAndCapsDeterministically(t *testing.T) {
	findings := []Finding{
		{Path: "z.go", StartLine: 2, EndLine: 2, Severity: "P2", Confidence: .9, Title: "Z"},
		{Path: "b.go", StartLine: 2, EndLine: 2, Severity: "P1", Confidence: .7, Title: "B"},
		{Path: "a.go", StartLine: 2, EndLine: 2, Severity: "P1", Confidence: .8, Title: "A"},
	}
	got := RankAndCapFindings(findings, 2)
	if len(got) != 2 || got[0].Path != "a.go" || got[1].Path != "b.go" {
		t.Fatalf("ranked findings = %#v", got)
	}
}

func TestRankAndCapFindingsUsesDefaultCap(t *testing.T) {
	findings := make([]Finding, DefaultMaxInlineComments+3)
	for i := range findings {
		findings[i] = Finding{Path: "file.go", StartLine: i + 1, EndLine: i + 1, Severity: "P2", Confidence: .7, Title: "Distinct location"}
	}
	if got := len(RankAndCapFindings(findings, 0)); got != DefaultMaxInlineComments {
		t.Fatalf("count = %d, want %d", got, DefaultMaxInlineComments)
	}
}
