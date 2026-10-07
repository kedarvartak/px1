package main

import (
	"strings"
	"testing"
)

func TestParseStaticReviewHunksTracksLineNumbers(t *testing.T) {
	diff := "diff --git a/app.go b/app.go\n" +
		"index 1111111..2222222 100644\n" +
		"--- a/app.go\n" +
		"+++ b/app.go\n" +
		"@@ -10,3 +10,4 @@ func run() {\n" +
		" \tbefore()\n" +
		"-\toldCall()\n" +
		"+\tnewCall()\n" +
		"+\taudit()\n" +
		" \tafter()\n" +
		"@@ -30 +31 @@ func stop() {\n" +
		"-\treturn\n" +
		"+\tcleanup()\n"
	hunks := parseStaticReviewHunks("app.go", diff)
	if len(hunks) != 2 {
		t.Fatalf("hunks = %#v", hunks)
	}
	if hunks[0].ID == "" || hunks[0].ID == hunks[1].ID {
		t.Fatalf("hunk IDs = %q, %q", hunks[0].ID, hunks[1].ID)
	}
	lines := hunks[0].Lines
	if len(lines) != 5 {
		t.Fatalf("lines = %#v", lines)
	}
	want := []staticReviewDiffLine{
		{OldLine: 10, NewLine: 10, Kind: "context", Text: "\tbefore()"},
		{OldLine: 11, Kind: "removed", Text: "\toldCall()"},
		{NewLine: 11, Kind: "added", Text: "\tnewCall()"},
		{NewLine: 12, Kind: "added", Text: "\taudit()"},
		{OldLine: 12, NewLine: 13, Kind: "context", Text: "\tafter()"},
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %#v, want %#v", i, lines[i], want[i])
		}
	}
}

func TestParseStaticReviewHunksReturnsEmptyForBinaryPatch(t *testing.T) {
	diff := "diff --git a/logo.png b/logo.png\nnew file mode 100644\nBinary files /dev/null and b/logo.png differ\n"
	if got := parseStaticReviewHunks("logo.png", diff); len(got) != 0 || got == nil {
		t.Fatalf("hunks = %#v", got)
	}
}

func TestStaticReviewHTMLRendersStructuredDiffAndFallback(t *testing.T) {
	report := staticReviewReport{
		Files: []staticReviewFile{
			{Path: "app.go", Status: "M", Hunks: []staticReviewHunk{{
				ID: "diff-test", Header: "@@ -1 +1 @@", Lines: []staticReviewDiffLine{{OldLine: 1, Kind: "removed", Text: "old"}, {NewLine: 1, Kind: "added", Text: "new"}},
			}}},
			{Path: "logo.png", Status: "A", Diff: "Binary files differ", Hunks: []staticReviewHunk{}},
		},
		Explanations: []staticExplanation{},
		RuleHits:     []staticReviewHit{},
		Verification: verificationResponse{Checks: []verificationCheck{}},
	}
	body, err := renderStaticReviewHTML(report)
	if err != nil {
		t.Fatal(err)
	}
	html := string(body)
	for _, want := range []string{`"hunks":[{"id":"diff-test"`, "'ln '+l.kind", "l.oldLine", "Binary files differ"} {
		if !strings.Contains(html, want) {
			t.Errorf("report missing %q", want)
		}
	}
}

func TestRelatedReviewBlocksGroupSharedBehaviorAcrossFiles(t *testing.T) {
	files := []staticReviewFile{
		{Path: "src/routes/orders.ts", Hunks: []staticReviewHunk{{Lines: []staticReviewDiffLine{{Kind: "added", Text: "return exportOrders(account, filter)"}}}}},
		{Path: "src/services/orders.ts", Hunks: []staticReviewHunk{{Lines: []staticReviewDiffLine{{Kind: "added", Text: "export function exportOrders(account, filter)"}}}}},
		{Path: "src/health.ts", Hunks: []staticReviewHunk{{Lines: []staticReviewDiffLine{{Kind: "added", Text: "return healthy"}}}}},
	}
	blocks := relatedReviewBlocks(files)
	if len(blocks) != 1 {
		t.Fatalf("blocks = %#v", blocks)
	}
	if len(blocks[0].Paths) != 2 || blocks[0].Paths[0] != "src/routes/orders.ts" || blocks[0].Paths[1] != "src/services/orders.ts" {
		t.Fatalf("grouped paths = %#v", blocks[0].Paths)
	}
	if blocks[0].ID == "" || !strings.Contains(strings.ToLower(blocks[0].Title), "orders") {
		t.Fatalf("block identity = %#v", blocks[0])
	}
}

func TestRelatedReviewBlocksDoNotGroupOnOneGenericCodeToken(t *testing.T) {
	files := []staticReviewFile{
		{Path: "src/users.ts", Hunks: []staticReviewHunk{{Lines: []staticReviewDiffLine{{Kind: "added", Text: "validate profile"}}}}},
		{Path: "src/billing.ts", Hunks: []staticReviewHunk{{Lines: []staticReviewDiffLine{{Kind: "added", Text: "validate invoice"}}}}},
	}
	if blocks := relatedReviewBlocks(files); len(blocks) != 0 || blocks == nil {
		t.Fatalf("blocks = %#v", blocks)
	}
}
