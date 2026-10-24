package review

import "testing"

func TestMapFindingsToDiffKeepsOnlyFullyAnchoredRanges(t *testing.T) {
	findings := []Finding{
		{Path: "api/user.go", StartLine: 10, EndLine: 12, Title: "inside"},
		{Path: "api/user.go", StartLine: 12, EndLine: 14, Title: "partial"},
		{Path: "deleted.go", StartLine: 2, EndLine: 2, Title: "deleted"},
	}
	inline, summary, err := MapFindingsToDiff(findings, []DiffRange{{Path: "api/user.go", StartLine: 10, EndLine: 13}})
	if err != nil {
		t.Fatal(err)
	}
	if len(inline) != 1 || inline[0].Title != "inside" {
		t.Fatalf("inline = %#v", inline)
	}
	if len(summary) != 2 || summary[0].Reason != "line range is not part of the current diff" || summary[1].Reason != "file has no commentable lines in the current diff" {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestMapFindingsToDiffNormalizesPaths(t *testing.T) {
	inline, _, err := MapFindingsToDiff([]Finding{{Path: "api/user.go", StartLine: 3, EndLine: 3}}, []DiffRange{{Path: "api/./user.go", StartLine: 3, EndLine: 3}})
	if err != nil || len(inline) != 1 {
		t.Fatalf("inline = %#v, err = %v", inline, err)
	}
}

func TestMapFindingsToDiffRejectsInvalidRanges(t *testing.T) {
	_, _, err := MapFindingsToDiff(nil, []DiffRange{{Path: "api.go", StartLine: 4, EndLine: 3}})
	if err == nil {
		t.Fatal("expected invalid range to fail")
	}
}
