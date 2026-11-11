package docs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// An edit to a page is data, whoever wrote it: a person or a model. It is
// applied only when it stays inside one section of a page that the diff
// affects and every citation quotes text that is really in the code.

// Limits on one edit.
const (
	MaxEditBodyBytes = 8 << 10
	MinQuoteBytes    = 12
	MaxCitations     = 8
)

// Citation is the code an edit rests on. Quote is the anchor: it must appear
// in File. Start and End are the lines it was found at, filled in by
// VerifyEdit; values supplied with the edit are ignored.
type Citation struct {
	File  string `json:"file"`
	Start int    `json:"start,omitempty"`
	End   int    `json:"end,omitempty"`
	Quote string `json:"quote"`
}

// Edit replaces the text of one section of one page.
type Edit struct {
	// Page is the page's path, such as docs/AGENT_TOOLS.md.
	Page string `json:"page"`
	// Heading is the text of the section's heading, without the # marks.
	Heading string `json:"heading"`
	// Body is the section's new text, without its heading. It replaces the
	// text up to the next heading of any level, so subsections are untouched.
	Body string `json:"body"`
	// Reason says, in a sentence, what changed in the code.
	Reason    string     `json:"reason"`
	Citations []Citation `json:"citations"`
}

// EditResult is what happened to one edit.
type EditResult struct {
	Edit    Edit `json:"edit"`
	Applied bool `json:"applied"`
	// Rejected is why the edit was not applied; empty when it was.
	Rejected string `json:"rejected,omitempty"`
}

// Section is one heading of a page and the text under it, up to the next
// heading of any level.
type Section struct {
	Heading string
	Level   int
	// Text is the section's own text, without the heading line.
	Text string
	// first and end are line indexes into the page body: the section's text
	// is lines[first:end]. The heading is at first-1.
	first, end int
}

// fenceMarker returns the fence a line opens or closes ("```" or "~~~").
func fenceMarker(line string) string {
	trimmed := strings.TrimSpace(line)
	for _, marker := range []string{"```", "~~~"} {
		if strings.HasPrefix(trimmed, marker) {
			return marker
		}
	}
	return ""
}

// headingOf returns the level and text of an ATX heading line.
func headingOf(line string) (int, string, bool) {
	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}
	if level == 0 || level > 6 || level >= len(line) || line[level] != ' ' {
		return 0, "", false
	}
	return level, strings.TrimSpace(strings.TrimRight(strings.TrimSpace(line[level:]), "#")), true
}

// SplitSections splits a page body at its headings. Lines inside fenced code
// are never headings.
func SplitSections(body string) []Section {
	lines := strings.Split(body, "\n")
	var sections []Section
	fence := ""
	for i, line := range lines {
		if marker := fenceMarker(line); marker != "" {
			switch fence {
			case "":
				fence = marker
			case marker:
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		level, text, ok := headingOf(line)
		if !ok {
			continue
		}
		if n := len(sections); n > 0 {
			sections[n-1].end = i
		}
		sections = append(sections, Section{Heading: text, Level: level, first: i + 1, end: len(lines)})
	}
	for i := range sections {
		sections[i].Text = strings.Join(lines[sections[i].first:sections[i].end], "\n")
	}
	return sections
}

// Sections returns the sections of the page.
func (p Page) Sections() []Section { return SplitSections(p.body) }

// Body returns the page's Markdown without its front matter.
func (p Page) Body() string { return p.body }

func squash(text string) string { return strings.Join(strings.Fields(text), " ") }

// locate finds a quote in a file, comparing with runs of white space
// collapsed, and returns the first and last line it spans.
func locate(content, quote string) (int, int, bool) {
	want := squash(quote)
	if want == "" {
		return 0, 0, false
	}
	lines := strings.Split(content, "\n")
	// Join the squashed lines with single spaces and remember where each
	// line starts, so an offset in the joined text maps back to a line.
	var joined strings.Builder
	var lineAt []int // offset in joined at which each non-empty line starts
	var lineNo []int
	for i, line := range lines {
		s := squash(line)
		if s == "" {
			continue
		}
		if joined.Len() > 0 {
			joined.WriteByte(' ')
		}
		lineAt = append(lineAt, joined.Len())
		lineNo = append(lineNo, i+1)
		joined.WriteString(s)
	}
	at := strings.Index(joined.String(), want)
	if at < 0 {
		return 0, 0, false
	}
	lineFor := func(offset int) int {
		n := sort.Search(len(lineAt), func(i int) bool { return lineAt[i] > offset }) - 1
		return lineNo[n]
	}
	return lineFor(at), lineFor(at + len(want) - 1), true
}

// VerifyEdit checks one edit against the pages and the impact of the diff.
// It returns the edit with its citations' lines filled in, or the reason the
// edit must not be applied.
func VerifyEdit(root string, pages []Page, impact Impact, edit Edit) (Edit, error) {
	var page *Page
	for i := range pages {
		if pages[i].Path == edit.Page {
			page = &pages[i]
		}
	}
	if page == nil {
		return edit, fmt.Errorf("%s is not a documentation page", edit.Page)
	}
	var affected *PageImpact
	for i := range impact.Pages {
		if impact.Pages[i].Path == edit.Page {
			affected = &impact.Pages[i]
		}
	}
	switch {
	case affected == nil:
		return edit, errors.New("the change touches no code this page covers")
	case affected.State == Regenerated || page.Kind != "narrative":
		return edit, errors.New("the page is generated or a reference page; px1 docs gen writes it")
	}

	var section *Section
	sections := page.Sections()
	for i := range sections {
		if sections[i].Heading == strings.TrimSpace(edit.Heading) {
			if section != nil {
				return edit, fmt.Errorf("the page has more than one section headed %q", edit.Heading)
			}
			section = &sections[i]
		}
	}
	if section == nil {
		return edit, fmt.Errorf("the page has no section headed %q", edit.Heading)
	}
	if strings.Contains(section.Text, "<!-- px1:") {
		return edit, errors.New("the section holds generated content")
	}

	body := strings.TrimSpace(edit.Body)
	switch {
	case body == "":
		return edit, errors.New("the new text is empty; an edit cannot delete a section")
	case len(body) > MaxEditBodyBytes:
		return edit, fmt.Errorf("the new text is over %d bytes", MaxEditBodyBytes)
	case strings.Contains(body, "<!-- px1:"):
		return edit, errors.New("the new text contains a px1 marker")
	case squash(body) == squash(section.Text):
		return edit, errors.New("the new text says the same as the old")
	case strings.TrimSpace(edit.Reason) == "":
		return edit, errors.New("the edit gives no reason")
	}
	if inner := SplitSections(body); len(inner) > 0 {
		return edit, fmt.Errorf("the new text contains a heading (%q); an edit stays inside one section", inner[0].Heading)
	}
	fences := 0
	for _, line := range strings.Split(body, "\n") {
		if fenceMarker(line) != "" {
			fences++
		}
	}
	if fences%2 != 0 {
		return edit, errors.New("the new text leaves a code fence open")
	}
	draft := Page{Path: page.Path, body: body, bodyStart: 1}
	if problems := checkLinks(root, draft); len(problems) > 0 {
		return edit, errors.New(problems[0].Message)
	}

	if len(edit.Citations) == 0 {
		return edit, errors.New("the edit cites no code")
	}
	if len(edit.Citations) > MaxCitations {
		return edit, fmt.Errorf("the edit has more than %d citations", MaxCitations)
	}
	changed := map[string]bool{}
	for _, file := range affected.Changed {
		changed[file] = true
	}
	citesChange := false
	cited := make([]Citation, len(edit.Citations))
	for i, citation := range edit.Citations {
		file := filepath.ToSlash(filepath.Clean(citation.File))
		covers := false
		for _, entry := range page.Covers {
			covers = covers || touches(file, entry)
		}
		if !covers {
			return edit, fmt.Errorf("citation %d: %s is not code this page covers", i+1, citation.File)
		}
		if len(squash(citation.Quote)) < MinQuoteBytes {
			return edit, fmt.Errorf("citation %d: the quote is shorter than %d characters", i+1, MinQuoteBytes)
		}
		data, err := readRepo(root, filepath.Join(root, filepath.FromSlash(file)))
		if err != nil {
			return edit, fmt.Errorf("citation %d: %s cannot be read", i+1, file)
		}
		start, end, ok := locate(string(data), citation.Quote)
		if !ok {
			return edit, fmt.Errorf("citation %d: the quote is not in %s", i+1, file)
		}
		cited[i] = Citation{File: file, Start: start, End: end, Quote: citation.Quote}
		citesChange = citesChange || changed[file]
	}
	if !citesChange {
		return edit, errors.New("no citation is from a file the change touches")
	}
	edit.Body, edit.Heading, edit.Citations = body, section.Heading, cited
	return edit, nil
}

// PlanEdits verifies every edit and returns what would happen to each, and
// the new text of each page that an accepted edit changes. It writes nothing.
// At most one edit is accepted for a section.
func PlanEdits(root string, impact Impact, edits []Edit) ([]EditResult, map[string]string, error) {
	pages, problems, err := LoadPages(root)
	if err != nil {
		return nil, nil, err
	}
	if len(problems) > 0 {
		return nil, nil, fmt.Errorf("%s: fix the pages before editing them (px1 docs check)", problems[0])
	}
	results := make([]EditResult, len(edits))
	accepted := map[string][]Edit{}
	taken := map[string]bool{}
	for i, edit := range edits {
		verified, err := VerifyEdit(root, pages, impact, edit)
		key := verified.Page + "\x00" + verified.Heading
		switch {
		case err != nil:
			results[i] = EditResult{Edit: edit, Rejected: err.Error()}
		case taken[key]:
			results[i] = EditResult{Edit: edit, Rejected: "another edit already replaces this section"}
		default:
			taken[key] = true
			results[i] = EditResult{Edit: verified, Applied: true}
			accepted[verified.Page] = append(accepted[verified.Page], verified)
		}
	}
	texts := map[string]string{}
	for _, page := range pages {
		pageEdits := accepted[page.Path]
		if len(pageEdits) == 0 {
			continue
		}
		data, err := readRepo(root, filepath.Join(root, filepath.FromSlash(page.Path)))
		if err != nil {
			return nil, nil, err
		}
		lines := strings.Split(string(data), "\n")
		offset := page.bodyStart - 1 // body line 0 is file line offset
		bodyLines := len(lines) - offset
		sections := page.Sections()
		byHeading := map[string]Section{}
		for _, section := range sections {
			byHeading[section.Heading] = section
		}
		// Replace from the bottom up so earlier line numbers stay valid.
		sort.Slice(pageEdits, func(i, j int) bool {
			return byHeading[pageEdits[i].Heading].first > byHeading[pageEdits[j].Heading].first
		})
		for _, edit := range pageEdits {
			section := byHeading[edit.Heading]
			replacement := append([]string{""}, strings.Split(edit.Body, "\n")...)
			if section.end < bodyLines {
				replacement = append(replacement, "") // blank line before the next heading
			}
			rebuilt := append([]string{}, lines[:offset+section.first]...)
			rebuilt = append(rebuilt, replacement...)
			lines = append(rebuilt, lines[offset+section.end:]...)
		}
		text := strings.Join(lines, "\n")
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		texts[page.Path] = text
	}
	return results, texts, nil
}

// ApplyEdits verifies the edits and writes the pages the accepted ones
// change. A page that is a symbolic link is never written.
func ApplyEdits(root string, impact Impact, edits []Edit) ([]EditResult, error) {
	results, texts, err := PlanEdits(root, impact, edits)
	if err != nil {
		return nil, err
	}
	for rel, text := range texts {
		full := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Lstat(full)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%s is not a regular file", rel)
		}
		if err := os.WriteFile(full, []byte(text), info.Mode().Perm()); err != nil {
			return nil, err
		}
	}
	return results, nil
}

// EditsMarkdown describes the results for a person: what was changed and on
// what evidence, and what was refused and why.
func EditsMarkdown(results []EditResult, sourceURL string) string {
	var applied, rejected []EditResult
	for _, result := range results {
		if result.Applied {
			applied = append(applied, result)
		} else {
			rejected = append(rejected, result)
		}
	}
	var b strings.Builder
	if len(applied) == 0 {
		b.WriteString("No documentation edit was applied.\n")
	}
	for _, result := range applied {
		edit := result.Edit
		fmt.Fprintf(&b, "- **%s**, section \"%s\": %s\n", edit.Page, edit.Heading, strings.TrimSpace(edit.Reason))
		for _, c := range edit.Citations {
			where := fmt.Sprintf("%s:%d-%d", c.File, c.Start, c.End)
			if c.Start == c.End {
				where = fmt.Sprintf("%s:%d", c.File, c.Start)
			}
			if sourceURL != "" {
				fmt.Fprintf(&b, "  - [`%s`](%s/%s#L%d-L%d)\n", where, strings.TrimRight(sourceURL, "/"), c.File, c.Start, c.End)
			} else {
				fmt.Fprintf(&b, "  - `%s`\n", where)
			}
		}
	}
	if len(rejected) > 0 {
		b.WriteString("\nRefused:\n\n")
		for _, result := range rejected {
			fmt.Fprintf(&b, "- %s, section \"%s\": %s\n", result.Edit.Page, result.Edit.Heading, result.Rejected)
		}
	}
	return b.String()
}
