package docs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const editPage = goodFront + `
# A page

Intro.

## Limits

The tool reads at most 10 files.

` + "```" + `
# not a heading
` + "```" + `

### Detail

Kept as it is.

## Other

See [the other page](OTHER.md).
`

// editRepo is a repository whose page covers tool.go, which the diff changed.
func editRepo(t *testing.T) (string, Impact) {
	t.Helper()
	root := repo(t)
	write(t, root, "tool.go", "package main\n\n// maxFiles is the most files one run reads.\nconst maxFiles = 25\n\nfunc   run()   {}\n")
	write(t, root, "docs/PAGE.md", editPage)
	pages, problems, err := LoadPages(root)
	if err != nil || len(problems) > 0 {
		t.Fatal(err, problems)
	}
	return root, ComputeImpact(pages, []string{"tool.go"})
}

func goodEdit() Edit {
	return Edit{
		Page: "docs/PAGE.md", Heading: "Limits", Body: "The tool reads at most 25 files.\n\n```\n# not a heading\n```",
		Reason:    "The file limit rose from 10 to 25.",
		Citations: []Citation{{File: "tool.go", Start: 99, End: 99, Quote: "const   maxFiles = 25"}},
	}
}

func TestSplitSections(t *testing.T) {
	page, _ := parsePage("docs/PAGE.md", editPage)
	var got []string
	for _, section := range page.Sections() {
		got = append(got, strings.Repeat("#", section.Level)+section.Heading)
	}
	if want := "#A page ##Limits ###Detail ##Other"; strings.Join(got, " ") != want {
		t.Fatalf("sections = %v, want %s", got, want)
	}
	if text := page.Sections()[1].Text; !strings.Contains(text, "# not a heading") || strings.Contains(text, "Kept as it is") {
		t.Fatalf("section text = %q", text)
	}
}

func TestApplyEditReplacesOnlyTheSection(t *testing.T) {
	root, impact := editRepo(t)
	results, err := ApplyEdits(root, impact, []Edit{goodEdit()})
	if err != nil {
		t.Fatal(err)
	}
	if !results[0].Applied {
		t.Fatalf("rejected: %s", results[0].Rejected)
	}
	if c := results[0].Edit.Citations[0]; c.Start != 4 || c.End != 4 {
		t.Fatalf("citation lines = %d-%d, want 4-4", c.Start, c.End)
	}
	data, _ := os.ReadFile(filepath.Join(root, "docs/PAGE.md"))
	want := strings.Replace(editPage, "at most 10 files", "at most 25 files", 1)
	if string(data) != want {
		t.Fatalf("page after edit:\n%s\nwant:\n%s", data, want)
	}
	if got := messages(t, root); got != "" {
		t.Fatalf("the edited repository fails the check:\n%s", got)
	}
	md := EditsMarkdown(results, "https://example.com/blob/abc")
	if !strings.Contains(md, "[`tool.go:4`](https://example.com/blob/abc/tool.go#L4-L4)") {
		t.Fatalf("markdown = %s", md)
	}
}

func TestEditOfTheLastSection(t *testing.T) {
	root, impact := editRepo(t)
	edit := goodEdit()
	edit.Heading, edit.Body = "Other", "Nothing else, since `maxFiles` is 25."
	results, err := ApplyEdits(root, impact, []Edit{edit, goodEdit()})
	if err != nil || !results[0].Applied || !results[1].Applied {
		t.Fatal(err, results)
	}
	data, _ := os.ReadFile(filepath.Join(root, "docs/PAGE.md"))
	if !strings.HasSuffix(string(data), "## Other\n\nNothing else, since `maxFiles` is 25.\n") || !strings.Contains(string(data), "at most 25 files.\n\n```") {
		t.Fatalf("page after edits:\n%s", data)
	}
}

func TestEditsAreRefused(t *testing.T) {
	root, impact := editRepo(t)
	write(t, root, "other.go", "package main\n\nconst somethingElse = 1\n")
	write(t, root, "pkg/a.go", "package pkg\n\nconst unchangedValue = 2\n")
	cases := []struct {
		name   string
		change func(*Edit)
		want   string
	}{
		{"unknown page", func(e *Edit) { e.Page = "docs/NOPE.md" }, "not a documentation page"},
		{"page the diff does not affect", func(e *Edit) { e.Page = "docs/OTHER.md"; e.Heading = "Other" }, "touches no code this page covers"},
		{"unknown section", func(e *Edit) { e.Heading = "Limit" }, "no section headed"},
		{"empty body", func(e *Edit) { e.Body = " \n" }, "cannot delete a section"},
		{"same text", func(e *Edit) { e.Body = "The tool reads   at most 10 files.\n\n```\n# not a heading\n```" }, "says the same"},
		{"heading in body", func(e *Edit) { e.Body = "Text.\n\n## New part\n\nMore." }, "contains a heading"},
		{"open fence", func(e *Edit) { e.Body = "Text.\n\n```\ncode" }, "code fence open"},
		{"fence closed by the other marker", func(e *Edit) { e.Body = "Text.\n\n```\ncode\n~~~" }, "code fence open"},
		{"marker", func(e *Edit) { e.Body = "<!-- px1:generated x -->" }, "px1 marker"},
		{"broken link", func(e *Edit) { e.Body = "See [this](MISSING.md)." }, "broken link"},
		{"no reason", func(e *Edit) { e.Reason = "" }, "gives no reason"},
		{"no citation", func(e *Edit) { e.Citations = nil }, "cites no code"},
		{"quote not in file", func(e *Edit) { e.Citations[0].Quote = "const maxFiles = 30" }, "the quote is not in tool.go"},
		{"short quote", func(e *Edit) { e.Citations[0].Quote = "= 25" }, "shorter than"},
		{"file the page does not cover", func(e *Edit) { e.Citations[0] = Citation{File: "other.go", Quote: "const somethingElse = 1"} }, "not code this page covers"},
		{"file outside the repository", func(e *Edit) { e.Citations[0].File = "../../etc/passwd" }, "not code this page covers"},
		{"only unchanged files cited", func(e *Edit) { e.Citations[0] = Citation{File: "pkg/a.go", Quote: "const unchangedValue = 2"} }, "no citation is from a file the change touches"},
		{"too big", func(e *Edit) { e.Body = strings.Repeat("word ", 2000) }, "over 8192 bytes"},
	}
	before, _ := os.ReadFile(filepath.Join(root, "docs/PAGE.md"))
	for _, tc := range cases {
		edit := goodEdit()
		tc.change(&edit)
		results, err := ApplyEdits(root, impact, []Edit{edit})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if results[0].Applied || !strings.Contains(results[0].Rejected, tc.want) {
			t.Errorf("%s: applied=%v rejected=%q, want %q", tc.name, results[0].Applied, results[0].Rejected, tc.want)
		}
	}
	after, _ := os.ReadFile(filepath.Join(root, "docs/PAGE.md"))
	if string(before) != string(after) {
		t.Fatal("a refused edit changed the page")
	}
}

func TestOneEditPerSectionAndGeneratedPagesAreLeftAlone(t *testing.T) {
	root, impact := editRepo(t)
	second := goodEdit()
	second.Body = "The tool reads at most 25 files per run."
	results, _, err := PlanEdits(root, impact, []Edit{goodEdit(), second})
	if err != nil || !results[0].Applied || results[1].Rejected != "another edit already replaces this section" {
		t.Fatal(err, results)
	}

	write(t, root, "docs/REF.md", strings.Replace(goodFront, "narrative", "reference", 1)+generatedNote+"\n# Ref\n\n## Limits\n\nOld.\n")
	pages, _, _ := LoadPages(root)
	impact = ComputeImpact(pages, []string{"tool.go"})
	edit := goodEdit()
	edit.Page = "docs/REF.md"
	results, _, err = PlanEdits(root, impact, []Edit{edit})
	if err != nil || !strings.Contains(results[0].Rejected, "px1 docs gen writes it") {
		t.Fatal(err, results)
	}
}

func TestNoPageIsWrittenWhenOneCannotBe(t *testing.T) {
	root, _ := editRepo(t)
	write(t, root, "docs/SECOND.md", strings.Replace(editPage, "# A page", "# Second", 1))
	pages, _, _ := LoadPages(root)
	impact := ComputeImpact(pages, []string{"tool.go"})
	second := goodEdit()
	second.Page = "docs/SECOND.md"
	results, texts, err := PlanEdits(root, impact, []Edit{goodEdit(), second})
	if err != nil || len(texts) != 2 || !results[1].Applied {
		t.Fatal(err, results)
	}
	// Between planning and writing, one page becomes a directory.
	for _, victim := range []string{"docs/PAGE.md", "docs/SECOND.md"} {
		other := map[string]string{"docs/PAGE.md": "docs/SECOND.md", "docs/SECOND.md": "docs/PAGE.md"}[victim]
		before, _ := os.ReadFile(filepath.Join(root, other))
		if err := writeTexts(root, map[string]string{victim: "x", other: "y", "docs/MISSING.md": "z"}); err == nil {
			t.Fatal("a missing destination was accepted")
		}
		if after, _ := os.ReadFile(filepath.Join(root, other)); string(after) != string(before) {
			t.Fatalf("%s was written although another destination failed", other)
		}
	}
}

func TestEditNeverWritesThroughALink(t *testing.T) {
	root, impact := editRepo(t)
	outside := filepath.Join(t.TempDir(), "secret.md")
	if err := os.WriteFile(outside, []byte(editPage), 0o644); err != nil {
		t.Fatal(err)
	}
	page := filepath.Join(root, "docs/PAGE.md")
	if err := os.Remove(page); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, page); err != nil {
		t.Skip("symbolic links are not available")
	}
	if _, err := ApplyEdits(root, impact, []Edit{goodEdit()}); err == nil {
		t.Fatal("an edit was applied through a link that leaves the repository")
	}
	if data, _ := os.ReadFile(outside); string(data) != editPage {
		t.Fatal("the file outside the repository was changed")
	}
}
