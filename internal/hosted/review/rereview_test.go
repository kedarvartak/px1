package review

import (
	"strings"
	"testing"
)

func TestPlanReReviewCarriesDismissalsAndResolvesFixedChangedThreads(t *testing.T) {
	prior := []PriorFinding{
		{Fingerprint: "dismissed", Path: "a.go", StartLine: 4, EndLine: 4, Status: "dismissed", ThreadID: "D"},
		{Fingerprint: "fixed", Path: "a.go", StartLine: 8, EndLine: 9, Status: "open", ThreadID: "T2"},
		{Fingerprint: "unchanged", Path: "b.go", StartLine: 2, EndLine: 2, Status: "open", ThreadID: "T3"},
	}
	proposed := []FingerprintedFinding{{Fingerprint: "dismissed"}, {Fingerprint: "new"}}
	plan, err := PlanReReview(strings.Repeat("a", 40), strings.Repeat("b", 40), []DiffRange{{Path: "a.go", StartLine: 7, EndLine: 10}}, prior, proposed)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Findings) != 1 || plan.Findings[0].Fingerprint != "new" {
		t.Fatalf("findings = %#v", plan.Findings)
	}
	if len(plan.ResolveThreadIDs) != 1 || plan.ResolveThreadIDs[0] != "T2" {
		t.Fatalf("resolve = %#v", plan.ResolveThreadIDs)
	}
	if len(plan.DismissedFingerprints) != 1 || plan.DismissedFingerprints[0] != "dismissed" {
		t.Fatalf("dismissed = %#v", plan.DismissedFingerprints)
	}
}

func TestPlanReReviewKeepsRecurringThreadOpen(t *testing.T) {
	prior := []PriorFinding{{Fingerprint: "same", Path: "a.go", StartLine: 8, EndLine: 9, Status: "open", ThreadID: "T2"}}
	plan, err := PlanReReview(strings.Repeat("a", 40), strings.Repeat("b", 40), []DiffRange{{Path: "a.go", StartLine: 8, EndLine: 9}}, prior, []FingerprintedFinding{{Fingerprint: "same"}})
	if err != nil || len(plan.ResolveThreadIDs) != 0 {
		t.Fatalf("plan = %#v, err = %v", plan, err)
	}
}
