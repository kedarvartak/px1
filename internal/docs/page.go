// Package docs reads the Markdown pages under docs/, checks them against the
// repository, and builds the manifest the documentation UI is rendered from.
// It reads files only; it never runs anything from the repository.
package docs

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Dir is where pages live, relative to the repository root.
const Dir = "docs"

// IndexFile is generated from the manifest; it is not a page.
const IndexFile = "README.md"

// Sections in the order the index and the UI show them.
var Sections = []string{"start", "guides", "concepts", "reference", "operations", "policies"}

var (
	kinds    = []string{"narrative", "reference"}
	statuses = []string{"shipped", "built", "planned"}
	commitRE = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
)

// Page is one documentation page and its front matter.
type Page struct {
	// Path is relative to the repository root, with forward slashes.
	Path    string `json:"path"`
	Title   string `json:"title"`
	Section string `json:"section"`
	// Kind is "narrative" (written prose) or "reference" (exact facts).
	Kind string `json:"kind"`
	// Status says whether a reader can use what the page describes:
	// shipped, built (code and tests exist, not deployed) or planned.
	Status string `json:"status"`
	// Covers lists the code the page describes: files, directories (ending
	// in "/") or globs, relative to the repository root.
	Covers []string `json:"covers"`
	// VerifiedAt is the commit at which a person or the docs agent last
	// checked the page against Covers. Empty means never.
	VerifiedAt string `json:"verified_at"`

	body      string
	bodyStart int // line number of the first body line
}

// Problem is one thing wrong with the documentation.
type Problem struct {
	Path    string
	Line    int
	Message string
}

func (p Problem) String() string {
	if p.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", p.Path, p.Line, p.Message)
	}
	return fmt.Sprintf("%s: %s", p.Path, p.Message)
}

// LoadPages reads every page under docs/. Problems are front matter errors;
// a page with problems is still returned so later checks can run on it.
func LoadPages(root string) ([]Page, []Problem, error) {
	entries, err := readRepoDir(root, filepath.Join(root, Dir))
	if err != nil {
		return nil, nil, err
	}
	var pages []Page
	var problems []Problem
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".md") || name == IndexFile {
			continue
		}
		rel := Dir + "/" + name
		data, err := readRepo(root, filepath.Join(root, Dir, name))
		if err != nil {
			return nil, nil, err
		}
		page, found := parsePage(rel, string(data))
		problems = append(problems, found...)
		pages = append(pages, page)
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i].Path < pages[j].Path })
	return pages, problems, nil
}

// parsePage splits front matter from the body and validates the fields. The
// front matter is a small fixed subset of YAML: "key: value" lines and one
// list, "covers:", whose items are "  - value" lines.
func parsePage(rel, text string) (Page, []Problem) {
	page := Page{Path: rel, Covers: []string{}, body: text, bodyStart: 1}
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r") != "---" {
		return page, []Problem{{rel, 1, "missing front matter (the page must start with ---)"}}
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\r") == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return page, []Problem{{rel, 1, "front matter is not closed with ---"}}
	}
	page.body = strings.Join(lines[end+1:], "\n")
	page.bodyStart = end + 2

	var problems []Problem
	seen := map[string]bool{}
	inCovers := false
	for i := 1; i < end; i++ {
		line := strings.TrimRight(lines[i], "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if item, ok := strings.CutPrefix(strings.TrimSpace(line), "- "); ok && line != strings.TrimLeft(line, " ") {
			if !inCovers {
				problems = append(problems, Problem{rel, i + 1, "list item outside covers"})
				continue
			}
			page.Covers = append(page.Covers, unquote(item))
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok || key != strings.TrimSpace(key) {
			problems = append(problems, Problem{rel, i + 1, "expected \"key: value\""})
			inCovers = false
			continue
		}
		value = unquote(strings.TrimSpace(value))
		inCovers = false
		if seen[key] {
			problems = append(problems, Problem{rel, i + 1, "duplicate key " + key})
			continue
		}
		seen[key] = true
		switch key {
		case "title":
			page.Title = value
		case "section":
			page.Section = value
		case "kind":
			page.Kind = value
		case "status":
			page.Status = value
		case "verified_at":
			page.VerifiedAt = value
		case "covers":
			inCovers = true
			if value != "" && value != "[]" {
				problems = append(problems, Problem{rel, i + 1, "covers must be a list of \"  - path\" lines, or []"})
			}
		default:
			problems = append(problems, Problem{rel, i + 1, "unknown key " + key})
		}
	}

	for _, key := range []string{"title", "section", "kind", "status", "covers", "verified_at"} {
		if !seen[key] {
			problems = append(problems, Problem{rel, 1, "front matter is missing " + key})
		}
	}
	if seen["title"] && page.Title == "" {
		problems = append(problems, Problem{rel, 1, "title is empty"})
	}
	problems = append(problems, oneOf(rel, "section", page.Section, Sections, seen)...)
	problems = append(problems, oneOf(rel, "kind", page.Kind, kinds, seen)...)
	problems = append(problems, oneOf(rel, "status", page.Status, statuses, seen)...)
	if page.VerifiedAt != "" && !commitRE.MatchString(page.VerifiedAt) {
		problems = append(problems, Problem{rel, 1, "verified_at must be empty or a commit SHA (7 to 40 hex characters)"})
	}
	return page, problems
}

func oneOf(rel, key, value string, allowed []string, seen map[string]bool) []Problem {
	if !seen[key] {
		return nil
	}
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	return []Problem{{rel, 1, fmt.Sprintf("%s is %q; it must be one of %s", key, value, strings.Join(allowed, ", "))}}
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
