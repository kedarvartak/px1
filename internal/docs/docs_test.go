package docs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const goodFront = "---\ntitle: A page\nsection: guides\nkind: narrative\nstatus: shipped\ncovers:\n  - tool.go\n  - pkg/\nverified_at:\n---\n"

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// repo is a small repository whose documentation passes the check.
func repo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write(t, root, "tool.go", "package main\n")
	write(t, root, "tool_test.go", "package main\n")
	write(t, root, "pkg/a.go", "package pkg\n")
	write(t, root, "docs/PAGE.md", goodFront+"\n# A page\n\nSee [the other page](OTHER.md#part) and [the site](https://example.com).\n")
	write(t, root, "docs/OTHER.md", strings.Replace(goodFront, "covers:\n  - tool.go\n  - pkg/\n", "covers: []\n", 1)+"\n# Other\n")
	if _, err := WriteGenerated(root); err != nil {
		t.Fatal(err)
	}
	return root
}

func messages(t *testing.T, root string) string {
	t.Helper()
	problems, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, problem := range problems {
		lines = append(lines, problem.String())
	}
	return strings.Join(lines, "\n")
}

func TestCleanRepositoryPasses(t *testing.T) {
	if got := messages(t, repo(t)); got != "" {
		t.Fatalf("unexpected problems:\n%s", got)
	}
}

func TestParsePageReadsFrontMatter(t *testing.T) {
	page, problems := parsePage("docs/PAGE.md", strings.Replace(goodFront, "verified_at:", "verified_at: 7905993", 1)+"body\n")
	if len(problems) != 0 {
		t.Fatalf("problems: %v", problems)
	}
	if page.Title != "A page" || page.Section != "guides" || page.Kind != "narrative" || page.Status != "shipped" || page.VerifiedAt != "7905993" {
		t.Fatalf("page = %+v", page)
	}
	if len(page.Covers) != 2 || page.Covers[0] != "tool.go" || page.Covers[1] != "pkg/" {
		t.Fatalf("covers = %v", page.Covers)
	}
	if page.body != "body\n" || page.bodyStart != 11 {
		t.Fatalf("body = %q at line %d", page.body, page.bodyStart)
	}
}

func TestParsePageRejectsBadFrontMatter(t *testing.T) {
	cases := map[string]struct{ text, want string }{
		"none":       {"# Title\n", "missing front matter"},
		"unclosed":   {"---\ntitle: x\n", "not closed"},
		"missing":    {strings.Replace(goodFront, "status: shipped\n", "", 1), "missing status"},
		"bad status": {strings.Replace(goodFront, "shipped", "live", 1), `status is "live"`},
		"bad kind":   {strings.Replace(goodFront, "narrative", "essay", 1), `kind is "essay"`},
		"unknown":    {strings.Replace(goodFront, "status:", "owner: me\nstatus:", 1), "unknown key owner"},
		"duplicate":  {strings.Replace(goodFront, "status:", "title: again\nstatus:", 1), "duplicate key title"},
		"bad commit": {strings.Replace(goodFront, "verified_at:", "verified_at: yesterday", 1), "verified_at must be"},
		"stray item": {strings.Replace(goodFront, "status: shipped\n", "status: shipped\n  - stray\n", 1), "list item outside covers"},
		"no title":   {strings.Replace(goodFront, "title: A page", "title:", 1), "title is empty"},
	}
	for name, c := range cases {
		_, problems := parsePage("docs/PAGE.md", c.text)
		found := false
		for _, problem := range problems {
			found = found || strings.Contains(problem.Message, c.want)
		}
		if !found {
			t.Errorf("%s: want %q in %v", name, c.want, problems)
		}
	}
}

func TestBrokenLinkIsReportedWithItsLine(t *testing.T) {
	root := repo(t)
	write(t, root, "docs/PAGE.md", goodFront+"\n# A page\n\n[gone](MISSING.md)\n\n```\n[example](ALSO_MISSING.md)\n```\n[out](../../elsewhere.md)\n")
	got := messages(t, root)
	if !strings.Contains(got, "docs/PAGE.md:14: broken link: MISSING.md") {
		t.Errorf("missing broken link:\n%s", got)
	}
	if strings.Contains(got, "ALSO_MISSING") {
		t.Errorf("a link inside a code fence was checked:\n%s", got)
	}
	if !strings.Contains(got, "link leaves the repository") {
		t.Errorf("missing escape report:\n%s", got)
	}
}

func TestCoversMustNameRealPaths(t *testing.T) {
	root := repo(t)
	front := strings.Replace(goodFront, "  - pkg/\n", "  - pkg\n  - gone.go\n  - nothing/*.go\n  - ../outside\n", 1)
	write(t, root, "docs/PAGE.md", front+"\n# A page\n")
	got := messages(t, root)
	for _, want := range []string{
		"must end in / exactly when it is a directory: pkg",
		"does not exist: gone.go",
		"matches nothing: nothing/*.go",
		`"../outside" must be a path inside the repository`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in:\n%s", want, got)
		}
	}
}

func TestNewCodeNeedsAPageOrAKnownGap(t *testing.T) {
	root := repo(t)
	write(t, root, "newcmd.go", "package main\n")
	write(t, root, "internal/fresh/x.go", "package fresh\n")
	write(t, root, "node_modules/dep/x.go", "package dep\n")
	got := messages(t, root)
	if !strings.Contains(got, "newcmd.go has no page") || !strings.Contains(got, "internal/fresh/ has no page") {
		t.Fatalf("uncovered code was not reported:\n%s", got)
	}
	if strings.Contains(got, "node_modules") || strings.Contains(got, "tool_test.go") {
		t.Fatalf("dependencies or tests were treated as code units:\n%s", got)
	}

	write(t, root, ConfigFile, `{"known_gaps":["newcmd.go","internal/fresh/"]}`)
	if got := messages(t, root); got != "" {
		t.Fatalf("known gaps still reported:\n%s", got)
	}
}

func TestKnownGapsCannotGoStale(t *testing.T) {
	root := repo(t)
	write(t, root, ConfigFile, `{"known_gaps":["pkg/","removed.go"],"extra_units":["action.yml"]}`)
	got := messages(t, root)
	for _, want := range []string{
		"known gap pkg/ now has a page",
		"known gap removed.go is not a code unit any more",
		"action.yml has no page",
		"extra unit does not exist: action.yml",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in:\n%s", want, got)
		}
	}
}

func TestCoveredMatching(t *testing.T) {
	cases := []struct {
		unit, entry string
		want        bool
	}{
		{"tool.go", "tool.go", true},
		{"internal/a/", "internal/", true},
		{"internal/a/", "internal/a/file.go", true},
		{"eval_run.go", "eval_*.go", true},
		{"internal/ab/", "internal/a/", false},
		{"tool.go", "tool.go.bak", false},
		{"internal/a/", "internal/a", false},
	}
	for _, c := range cases {
		if got := covered(c.unit, c.entry); got != c.want {
			t.Errorf("covered(%q, %q) = %v", c.unit, c.entry, got)
		}
	}
}

func TestStaleManifestAndIndexAreReported(t *testing.T) {
	root := repo(t)
	write(t, root, "docs/OTHER.md", strings.Replace(goodFront, "covers:\n  - tool.go\n  - pkg/\n", "covers: []\n", 1)+"\n# Renamed\n")
	if got := messages(t, root); got != "" {
		t.Fatalf("a body change must not make the manifest stale:\n%s", got)
	}
	write(t, root, "docs/THIRD.md", strings.Replace(goodFront, "A page", "Third", 1)+"\n# Third\n")
	got := messages(t, root)
	if !strings.Contains(got, "docs/manifest.json: out of date") || !strings.Contains(got, "docs/README.md: out of date") {
		t.Fatalf("stale generated files not reported:\n%s", got)
	}
	changed, err := WriteGenerated(root)
	if err != nil || len(changed) != 2 {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if again, _ := WriteGenerated(root); len(again) != 0 {
		t.Fatalf("second write changed %v", again)
	}
	index, _ := os.ReadFile(filepath.Join(root, "docs", "README.md"))
	if !strings.Contains(string(index), "- [Third](THIRD.md) (shipped)") {
		t.Fatalf("index:\n%s", index)
	}
}

func TestWriteGeneratedRefusesBadFrontMatter(t *testing.T) {
	root := repo(t)
	write(t, root, "docs/BAD.md", "# no front matter\n")
	if _, err := WriteGenerated(root); err == nil || !strings.Contains(err.Error(), "docs/BAD.md") {
		t.Fatalf("err = %v", err)
	}
}
