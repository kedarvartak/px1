package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"px1/internal/hosted/agent"
)

func entryOf(path string, size int) reviewEntry {
	return reviewEntry{file: staticReviewFile{Path: path}, review: agent.ReviewFile{Path: path, Diff: strings.Repeat("x", size)}}
}

func TestFilePriorityRanksRiskFirstAndDocsLast(t *testing.T) {
	cases := map[string]int{
		".github/workflows/ci.yml": 3, "db/migrations/001.sql": 3, "src/auth/login.ts": 3,
		"src/refund.ts": 2, "src/refund.test.ts": 1, "README.md": 0, "eval/cases.jsonl": 0,
	}
	for path, want := range cases {
		if got := filePriority(path); got != want {
			t.Errorf("filePriority(%q) = %d, want %d", path, got, want)
		}
	}
}

func TestSelectWithinLimitsKeepsRiskyFilesAndPreservesOrder(t *testing.T) {
	entries := []reviewEntry{entryOf("README.md", 10), entryOf("src/a.ts", 10), entryOf("src/auth/login.ts", 10), entryOf("src/a.test.ts", 10), entryOf("src/b.ts", 10)}
	kept, skipped := selectWithinLimits(entries, reviewLimits{maxFiles: 3})
	var names []string
	for _, e := range kept {
		names = append(names, e.file.Path)
	}
	if got := strings.Join(names, ","); got != "src/a.ts,src/auth/login.ts,src/b.ts" {
		t.Fatalf("kept %s (risky first, then source, original order kept)", got)
	}
	if len(skipped) != 2 || skipped[0].Path != "README.md" || skipped[1].Path != "src/a.test.ts" {
		t.Fatalf("skipped %+v", skipped)
	}
}

func TestSelectWithinLimitsHonoursByteBudgetAndNoLimit(t *testing.T) {
	entries := []reviewEntry{entryOf("src/big.ts", 100), entryOf("src/small.ts", 10), entryOf("src/mid.ts", 30)}
	kept, skipped := selectWithinLimits(entries, reviewLimits{maxDiffBytes: 50})
	if len(kept) != 2 || len(skipped) != 1 || skipped[0].Path != "src/big.ts" {
		t.Fatalf("kept=%d skipped=%+v: smaller diffs fill the budget first", len(kept), skipped)
	}
	if kept, skipped := selectWithinLimits(entries, reviewLimits{}); len(kept) != 3 || skipped != nil {
		t.Fatal("zero limits mean no limit")
	}
	if kept, _ := selectWithinLimits(entries, reviewLimits{maxFiles: 3, maxDiffBytes: 1000}); len(kept) != 3 {
		t.Fatal("a change within both limits is reviewed whole")
	}
}

func TestLargeChangeIsReviewedInPartAndSaysSo(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", fakeKey)
	f := newFakeOpenAI(t)
	root, base, _ := newAgenticRepo(t, cleanHead)
	for i := 0; i < 6; i++ {
		writeExportTestFile(t, root, fmt.Sprintf("src/extra%d.ts", i), fmt.Sprintf("export const v%d = %d;\n", i, i))
	}
	writeExportTestFile(t, root, "docs/notes.md", "# notes\n")
	runGitTest(t, root, "add", ".")
	runGitTest(t, root, "commit", "-m", "More files")

	var dry bytes.Buffer
	if err := runAgenticReview(agenticArgs(f, root, base, t.TempDir(), "--dry-run", "--max-files", "3"), &dry); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Large change: reviewing 3 of 8 files", "docs/notes.md", "highest-risk first"} {
		if !strings.Contains(dry.String(), want) {
			t.Fatalf("dry run missing %q:\n%s", want, dry.String())
		}
	}

	out := t.TempDir()
	if err := runAgenticReview(agenticArgs(f, root, base, out, "--yes", "--max-files", "3"), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, "scope.json"))
	if err != nil {
		t.Fatal(err)
	}
	var scope reviewScope
	if err := json.Unmarshal(data, &scope); err != nil || scope.Reviewed != 3 || scope.Total != 8 || len(scope.Skipped) != 5 {
		t.Fatalf("scope=%+v err=%v", scope, err)
	}
	if strings.Contains(f.allPrompts(), "docs/notes.md") && strings.Contains(f.allPrompts(), "# notes") {
		t.Fatal("a skipped file's diff was sent to the model")
	}
}

func TestPartialReviewNoteLeadsTheSummary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scope.json")
	os.WriteFile(path, []byte(`{"reviewed":3,"total":8,"skipped":["a.ts","b.ts"]}`), 0o600)
	note, err := readScopeNote(path)
	if err != nil || !strings.Contains(note, "reviewed 3 of 8 files") || !strings.Contains(note, "`a.ts`, `b.ts`") {
		t.Fatalf("note=%q err=%v", note, err)
	}
	summary := renderAgenticSummary(postPlan{inlined: map[string]bool{}}, testHead, note, "")
	if i, j := strings.Index(summary, "Partial review"), strings.Index(summary, "No verified findings"); i < 0 || j < 0 || i > j {
		t.Fatalf("the partial-review warning must come before the result:\n%s", summary)
	}
	full := filepath.Join(t.TempDir(), "full.json")
	os.WriteFile(full, []byte(`{"reviewed":3,"total":3}`), 0o600)
	if note, _ := readScopeNote(full); note != "" {
		t.Fatalf("a full review needs no note, got %q", note)
	}
}
