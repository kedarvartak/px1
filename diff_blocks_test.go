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
	for _, want := range []string{`"hunks":[{"id":"diff-test"`, "diff-line " + "'", "line.oldLine", "Binary files differ"} {
		if !strings.Contains(html, want) {
			t.Errorf("report missing %q", want)
		}
	}
}
