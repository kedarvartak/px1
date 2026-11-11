package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"px1/internal/docs"
)

func TestShippedDocsCasesAreValid(t *testing.T) {
	data, err := os.ReadFile("eval/docs-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	set, err := docs.ParseEvalSet(data)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, c := range set.Cases {
		kinds[c.Kind]++
	}
	if kinds[docs.EvalStale] < 5 || kinds[docs.EvalClean] < 5 {
		t.Fatalf("the set needs at least 5 stale and 5 clean cases, has %v", kinds)
	}
}

const docsEvalCases = `{"version":1,"cases":[
 {"id":"limit","kind":"stale","what":"limit raised","page":"# A page\n\n## Limits\n\nAt most 10 files.\n","files":{"tool.go":{"before":"package main\n\nconst maxFiles = 10\n","after":"package main\n\nconst maxFiles = 25\n"}},"expect":{"section":"Limits","must_contain":["25 files"],"must_not_contain":["10 files"]}},
 {"id":"quiet","kind":"clean","what":"comment only","page":"# A page\n\n## Limits\n\nAt most 10 files.\n","files":{"tool.go":{"before":"package main\n\nconst maxFiles = 10\n","after":"package main\n\n// maxFiles bounds a run.\nconst maxFiles = 10\n"}}}
]}`

func TestDocsEvalScoresACorrectUpdater(t *testing.T) {
	cases := filepath.Join(t.TempDir(), "cases.json")
	if err := os.WriteFile(cases, []byte(docsEvalCases), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runDocs([]string{"eval", "--cases", cases}, &out); err == nil || !strings.Contains(err.Error(), "--yes") || !strings.Contains(out.String(), "2 case(s)") {
		t.Fatalf("without --yes: %v\n%s", err, out.String())
	}

	t.Setenv("OPENAI_API_KEY", "test-key")
	// This fake drafts the same edit for every page. It is right for the
	// stale case and is refused on the clean one, where the quote is not in
	// the code.
	model := &docsModel{draft: goodDraft, verdict: `{"supported":true}`}
	endpoint := model.serve(t)
	args := []string{"eval", "--cases", cases, "--model", "test-model", "--price-input", "1", "--price-output", "4", "--endpoint", endpoint, "--yes"}
	report := filepath.Join(t.TempDir(), "eval.json")
	out.Reset()
	if err := runDocs(append(args, "--out", report, "--min-fixed", "1", "--min-quiet", "1"), &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "Stale pages corrected: 1 of 1. Unaffected pages left alone: 1 of 1.") {
		t.Fatalf("summary:\n%s", out.String())
	}
	var decoded docsEvalReport
	data, _ := os.ReadFile(report)
	if err := json.Unmarshal(data, &decoded); err != nil || len(decoded.Outcomes) != 2 || !decoded.Outcomes[0].Pass || decoded.Outcomes[1].Refused != 1 {
		t.Fatalf("report: %v %s", err, data)
	}

	// An updater that never edits fails the stale case and the bar.
	model.draft = `{"edits":[]}`
	out.Reset()
	err := runDocs(append(args, "--min-fixed", "0.5"), &out)
	if err == nil || !strings.Contains(err.Error(), "below the bar") || !strings.Contains(out.String(), "FAIL: the stale section was not edited") {
		t.Fatalf("never edits: %v\n%s", err, out.String())
	}
	out.Reset()
	if err := runDocs(append(args, "--only", "quiet", "--min-quiet", "1"), &out); err != nil || !strings.Contains(out.String(), "left alone: 1 of 1") {
		t.Fatalf("--only: %v\n%s", err, out.String())
	}
	for _, bad := range [][]string{{"--min-fixed", "NaN"}, {"--min-quiet", "1.5"}, {"--min-fixed", "-1"}, {"--max-cost", "NaN"}} {
		if err := runDocs(append(args, bad...), &out); err == nil || !strings.Contains(err.Error(), "must be") {
			t.Fatalf("%v: %v", bad, err)
		}
	}
	if err := runDocs(append(args, "--only", "nope"), &out); err == nil {
		t.Fatal("an unknown case id was accepted")
	}
}
