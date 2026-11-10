package docs

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// ImpactMarker identifies the pull-request comment so it is updated, not
// repeated.
const ImpactMarker = "<!-- px1-docs-impact -->"

// Page states in an impact report.
const (
	NeedsReview = "needs-review" // covered code changed, the page did not
	Updated     = "updated"      // covered code changed and so did the page
	Regenerated = "generated"    // a generated page; the check keeps it current
)

// PageImpact is one page whose covered code a diff touches.
type PageImpact struct {
	Path    string   `json:"path"`
	Title   string   `json:"title"`
	State   string   `json:"state"`
	Changed []string `json:"changed"`
}

// Impact is what a diff means for the documentation.
type Impact struct {
	Base  string       `json:"base"`
	Head  string       `json:"head"`
	Pages []PageImpact `json:"pages"`
	// Uncovered are changed Go files that no page covers.
	Uncovered []string `json:"uncovered"`
}

// touches reports whether a changed file falls under a covers entry.
func touches(file, entry string) bool {
	if file == entry {
		return true
	}
	if strings.HasSuffix(entry, "/") {
		return strings.HasPrefix(file, entry)
	}
	if strings.ContainsAny(entry, "*?[") {
		ok, _ := path.Match(entry, file)
		return ok
	}
	return false
}

// ComputeImpact maps changed files to the pages that cover them. Test files
// never count: they change without the documented behaviour changing.
func ComputeImpact(pages []Page, changed []string) Impact {
	impact := Impact{Pages: []PageImpact{}, Uncovered: []string{}}
	changedSet := map[string]bool{}
	var code []string
	for _, file := range changed {
		changedSet[file] = true
		if strings.HasPrefix(file, Dir+"/") || strings.HasSuffix(file, "_test.go") {
			continue
		}
		code = append(code, file)
	}
	sort.Strings(code)
	isCovered := map[string]bool{}
	for _, page := range pages {
		var hit []string
		for _, file := range code {
			for _, entry := range page.Covers {
				if touches(file, entry) {
					hit = append(hit, file)
					isCovered[file] = true
					break
				}
			}
		}
		if len(hit) == 0 {
			continue
		}
		state := NeedsReview
		switch {
		case page.Kind == "reference" && strings.Contains(page.body, generatedNote):
			state = Regenerated
		case changedSet[page.Path]:
			state = Updated
		}
		impact.Pages = append(impact.Pages, PageImpact{Path: page.Path, Title: page.Title, State: state, Changed: hit})
	}
	order := map[string]int{NeedsReview: 0, Updated: 1, Regenerated: 2}
	sort.SliceStable(impact.Pages, func(i, j int) bool {
		return order[impact.Pages[i].State] < order[impact.Pages[j].State]
	})
	for _, file := range code {
		if strings.HasSuffix(file, ".go") && !isCovered[file] {
			impact.Uncovered = append(impact.Uncovered, file)
		}
	}
	return impact
}

// Count returns how many pages are in a state.
func (i Impact) Count(state string) int {
	n := 0
	for _, page := range i.Pages {
		if page.State == state {
			n++
		}
	}
	return n
}

func fileList(files []string) string {
	const most = 6
	var parts []string
	for n, file := range files {
		if n == most {
			parts = append(parts, fmt.Sprintf("and %d more", len(files)-most))
			break
		}
		parts = append(parts, "`"+file+"`")
	}
	return strings.Join(parts, ", ")
}

// Markdown renders the report as a pull-request comment.
func (i Impact) Markdown() string {
	var b strings.Builder
	b.WriteString(ImpactMarker + "\n## Documentation impact\n\n")
	review := i.Count(NeedsReview)
	switch {
	case len(i.Pages) == 0 && len(i.Uncovered) == 0:
		b.WriteString("This change touches no code that a documentation page covers.\n")
		return b.String()
	case review == 0:
		b.WriteString("No page needs review: every page that covers the changed code was updated or is generated.\n")
	case review == 1:
		b.WriteString("**1 page covers code this change touches and was not updated.** Check that it is still true.\n")
	default:
		fmt.Fprintf(&b, "**%d pages cover code this change touches and were not updated.** Check that they are still true.\n", review)
	}
	labels := map[string]string{NeedsReview: "Needs review", Updated: "Updated in this change", Regenerated: "Generated"}
	if len(i.Pages) > 0 {
		b.WriteString("\n| Page | State | Changed code it covers |\n| --- | --- | --- |\n")
		for _, page := range i.Pages {
			fmt.Fprintf(&b, "| %s (`%s`) | %s | %s |\n", cell(page.Title), page.Path, labels[page.State], fileList(page.Changed))
		}
	}
	if len(i.Uncovered) > 0 {
		fmt.Fprintf(&b, "\nChanged Go files that no page covers: %s.\n", fileList(i.Uncovered))
	}
	b.WriteString("\nThis is a map from `covers` in each page's front matter, not a judgment that a page is wrong.\n")
	return b.String()
}
