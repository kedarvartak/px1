package docs

import (
	"encoding/json"
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

func TestConfigRejectsUnknownKeys(t *testing.T) {
	root := repo(t)
	write(t, root, ConfigFile, `{"extra_unit":["action.yml"]}`)
	if _, err := Check(root); err == nil || !strings.Contains(err.Error(), "extra_unit") {
		t.Fatalf("err = %v", err)
	}
}

const mainSource = `package main

import (
	"encoding/json"
	"flag"
	"os"
)

const defaultOut = "report"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "review" {
		runReview(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "tools" {
		runTools(os.Args[2:])
		return
	}
	if len(os.Args) == 2 && (os.Args[1] == "version" || os.Args[1] == "--version") {
		return
	}
}

// runReview reviews two commits
// and writes a report.
func runReview(args []string) {
	o := struct{ out string; n int; yes bool }{}
	fs := flag.NewFlagSet("review", flag.ContinueOnError)
	base := fs.String("base", "", "base commit (required)")
	fs.StringVar(&o.out, "out", defaultOut, "where <out> | goes")
	fs.IntVar(&o.n, "max", 120_000, "limit")
	fs.BoolVar(&o.yes, "yes", false, "send")
	env := fs.String("repo", os.Getenv("REPO"), "owner/name")
	fs.Var(nil, "exclude", "glob (repeatable)")
	_, _ = base, env
}
`

const toolsSource = `package main

import "flag"

func runTools(args []string) {
	fs := flag.NewFlagSet("tools "+args[0], flag.ContinueOnError)
	_ = fs.String("root", ".", "repository root")
}
`

func TestCommandsAreReadFromSource(t *testing.T) {
	root := t.TempDir()
	write(t, root, "main.go", mainSource)
	write(t, root, "tools.go", toolsSource)
	write(t, root, "main_test.go", "package main\nthis is not parsed\n")
	commands, err := Commands(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 2 || commands[0].Name != "review" || commands[1].Name != "tools" {
		t.Fatalf("commands = %+v", commands)
	}
	review := commands[0]
	if review.Summary != "Reviews two commits and writes a report." {
		t.Errorf("summary = %q", review.Summary)
	}
	want := []Flag{
		{"base", "string", "", "base commit (required)"},
		{"out", "string", "`report`", "where <out> | goes"},
		{"max", "integer", "`120000`", "limit"},
		{"yes", "boolean", "", "send"},
		{"repo", "string", "computed: `os.Getenv(\"REPO\")`", "owner/name"},
		{"exclude", "value", "", "glob (repeatable)"},
	}
	if len(review.Flags) != len(want) {
		t.Fatalf("flags = %+v", review.Flags)
	}
	for i, f := range want {
		if review.Flags[i] != f {
			t.Errorf("flag %d = %+v, want %+v", i, review.Flags[i], f)
		}
	}
	tools := commands[1]
	if tools.Summary != "" || len(tools.Flags) != 1 || tools.Flags[0].Name != "root" || tools.Files[0] != "tools.go" {
		t.Errorf("tools = %+v", tools)
	}

	page := renderCLI(commands)
	for _, want := range []string{
		"  - main.go\n  - tools.go\n",
		"## px1 review\n\nReviews two commits and writes a report.\n",
		"| `--out` | string | `report` | where \\<out> \\| goes |",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("want %q in:\n%s", want, page)
		}
	}
	if _, problems := parsePage(CLIReferenceFile, page); len(problems) != 0 {
		t.Errorf("generated page has invalid front matter: %v", problems)
	}
}

func TestSchemaFields(t *testing.T) {
	fields, err := SchemaFields([]byte(`{"required":["b","a"],"properties":{
		"a":{"type":"string","minLength":1,"maxLength":20},
		"b":{"enum":["x","y"]},
		"c":{"type":"number","minimum":0,"maximum":1},
		"z":{"type":"string"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []Field{
		{"b", "enum", true, "one of `x`, `y`"},
		{"a", "string", true, "not empty; at most 20 characters"},
		{"c", "number", false, "at least 0; at most 1"},
		{"z", "string", false, ""},
	}
	if len(fields) != len(want) {
		t.Fatalf("fields = %+v", fields)
	}
	for i := range want {
		if fields[i] != want[i] {
			t.Errorf("field %d = %+v, want %+v", i, fields[i], want[i])
		}
	}
	if _, err := SchemaFields([]byte(`{"required":["gone"],"properties":{}}`)); err == nil {
		t.Error("a required property with no definition was accepted")
	}
}

func TestParseAction(t *testing.T) {
	action, err := parseAction("action.yml", "name: Review\ndescription: 'Does a review.'\n\ninputs:\n  model:\n    description: The model.\n    required: true\n  cap:\n    description: Cost cap.\n    required: false\n    default: '0.50'\n\nruns:\n  using: composite\n  steps:\n    - name: x\n      with:\n        a: b\n")
	if err != nil {
		t.Fatal(err)
	}
	if action.Name != "Review" || action.Description != "Does a review." || len(action.Inputs) != 2 {
		t.Fatalf("action = %+v", action)
	}
	if in := action.Inputs[0]; in.Name != "model" || !in.Required || in.Description != "The model." {
		t.Errorf("first input = %+v", in)
	}
	if in := action.Inputs[1]; in.Required || in.Default != "0.50" {
		t.Errorf("second input = %+v", in)
	}
	for name, text := range map[string]string{
		"multi-line": "inputs:\n  a:\n    description: |\n      long\n",
		"unknown":    "inputs:\n  a:\n    colour: red\n",
		"indent":     "inputs:\n   a:\n",
	} {
		if _, err := parseAction("action.yml", text); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

const toolsGo = `package agent

const ReadFile = "read_file"

type Toolbox struct{}

func (t Toolbox) Specs() []string {
	add := func(enabled bool, name, description, schema string) {}
	shared := ` + "`" + `{"required":["path"],"properties":{"path":{"type":"string"},"limit":{"type":"integer"}}}` + "`" + `
	add(true, ReadFile, "Read a file.", shared)
	add(true, "git_log", "List commits.", ` + "`" + `{"properties":{}}` + "`" + `)
	return nil
}
`

func TestReferenceFillsRegionsAndCheckCatchesDrift(t *testing.T) {
	root := repo(t)
	write(t, root, toolsFile, toolsGo)
	write(t, root, findingSchemaFile, `{"required":["path"],"properties":{"path":{"type":"string"}}}`)
	write(t, root, "action.yml", "name: Review\ndescription: Does a review.\ninputs:\n  model:\n    description: The model.\n    required: true\n")
	front := strings.Replace(goodFront, "covers:\n  - tool.go\n  - pkg/\n", "covers:\n  - internal/hosted/agent/\n", 1)
	write(t, root, agentToolsPage, front+"\n# Tools\n\nBefore.\n\n<!-- px1:generated agent-tools -->\nold text\n<!-- px1:end agent-tools -->\n\nAfter.\n")
	write(t, root, findingSchemaPage, strings.Replace(front, "internal/hosted/agent/", "schemas/finding.schema.json", 1)+"\n# Finding\n\n<!-- px1:generated finding-fields -->\n<!-- px1:end finding-fields -->\n")

	if got := messages(t, root); !strings.Contains(got, "docs/AGENT_TOOLS.md: out of date: run px1 docs gen") || !strings.Contains(got, "docs/ACTION_INPUTS.md: out of date") {
		t.Fatalf("stale reference not reported:\n%s", got)
	}
	if _, err := WriteGenerated(root); err != nil {
		t.Fatal(err)
	}
	if got := messages(t, root); got != "" {
		t.Fatalf("problems after gen:\n%s", got)
	}
	tools, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(agentToolsPage)))
	for _, want := range []string{
		"Before.\n", "After.\n",
		"| `read_file` | Read a file. | `path`, `limit` (optional) |",
		"| `git_log` | List commits. |  |",
	} {
		if !strings.Contains(string(tools), want) {
			t.Errorf("want %q in:\n%s", want, tools)
		}
	}
	if strings.Contains(string(tools), "old text") {
		t.Errorf("the old region text survived:\n%s", tools)
	}
	if again, _ := WriteGenerated(root); len(again) != 0 {
		t.Fatalf("second gen changed %v", again)
	}

	// The code changes; the page must be reported until it is regenerated.
	write(t, root, toolsFile, strings.Replace(toolsGo, "Read a file.", "Read one file.", 1))
	if got := messages(t, root); !strings.Contains(got, "docs/AGENT_TOOLS.md: out of date") {
		t.Fatalf("drift not reported:\n%s", got)
	}

	// A page that lost its markers is an error, not a silent skip.
	write(t, root, agentToolsPage, front+"\n# Tools\n")
	if _, err := Check(root); err == nil || !strings.Contains(err.Error(), "missing the") {
		t.Fatalf("err = %v", err)
	}
}

func TestBuildSite(t *testing.T) {
	root := repo(t)
	write(t, root, "ROADMAP.md", "# Roadmap\n")
	write(t, root, "docs/PAGE.md", goodFront+"\n# A page\n\nSee [other](OTHER.md#part), [the index](README.md), [the roadmap](../ROADMAP.md) and [a site](https://example.com).\n\n## First part\n\n| A | B |\n| --- | --- |\n| 1 | `x` |\n\n<script>alert(1)</script>\n\n## Second <part>\n")
	if _, err := WriteGenerated(root); err != nil {
		t.Fatal(err)
	}
	files, err := BuildSite(root, SiteOptions{SourceURL: "https://example.com/repo/blob/abc/", Commit: "abc1234", Fonts: map[string][]byte{"plex-mono-500.woff2": []byte("font")}})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"index.html", "PAGE.html", "OTHER.html", "md/PAGE.md", "docs.css", "docs.js", "search.json", "manifest.json", "fonts/plex-mono-500.woff2"} {
		if _, ok := files[name]; !ok {
			t.Errorf("missing %s", name)
		}
	}
	page := string(files["PAGE.html"])
	for _, want := range []string{
		`<title>A page · px1 docs</title>`,
		`href="OTHER.html#part"`,
		`href="index.html">the index`,
		`href="https://example.com/repo/blob/abc/ROADMAP.md"`,
		`href="https://example.com"`,
		`<div class="table"><table>`,
		`<span class="chip shipped">shipped</span>`,
		`Not yet verified against the code`,
		`<a href="#first-part">First part</a>`,
		`<a href="https://example.com/repo/blob/abc/pkg/"><code>pkg/</code></a>`,
		`aria-current="page"`,
		`Built from <code>abc1234</code>`,
		`font-family:'Report Mono'`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("want %q in page", want)
		}
	}
	if strings.Contains(page, "alert(1)") {
		t.Error("raw HTML from a page reached the site")
	}
	if !strings.HasPrefix(string(files["md/PAGE.md"]), "---\ntitle: A page\n") {
		t.Error("the Markdown copy lost its front matter")
	}
	var index []searchEntry
	if err := json.Unmarshal(files["search.json"], &index); err != nil || len(index) != 2 {
		t.Fatalf("search index: %v %+v", err, index)
	}
	if index[1].URL != "PAGE.html" || len(index[1].Headings) != 2 || index[1].Headings[0] != "First part" || !strings.Contains(index[1].Text, "See other") {
		t.Errorf("search entry = %+v", index[1])
	}
	home := string(files["index.html"])
	if !strings.Contains(home, `<a href="PAGE.html">`) || strings.Contains(home, "aria-current") {
		t.Errorf("home page links are wrong")
	}
}

func TestWriteSiteRefusesANonEmptyDirectoryAndBadPages(t *testing.T) {
	root := repo(t)
	out := filepath.Join(t.TempDir(), "site")
	n, err := WriteSite(root, out, SiteOptions{})
	if err != nil || n < 6 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if _, err := os.Stat(filepath.Join(out, "md", "PAGE.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteSite(root, out, SiteOptions{}); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("second write: %v", err)
	}
	write(t, root, "docs/BAD.md", "# no front matter\n")
	if _, err := WriteSite(root, filepath.Join(t.TempDir(), "x"), SiteOptions{}); err == nil || !strings.Contains(err.Error(), "docs/BAD.md") {
		t.Fatalf("bad page: %v", err)
	}
}

func impactPages(t *testing.T) []Page {
	t.Helper()
	guide, _ := parsePage("docs/GUIDE.md", strings.Replace(goodFront, "  - tool.go\n  - pkg/\n", "  - tool.go\n  - pkg/\n  - eval_*.go\n", 1)+"# Guide\n")
	other, _ := parsePage("docs/OTHER.md", strings.Replace(goodFront, "  - tool.go\n  - pkg/\n", "  - pkg/a.go\n", 1)+"# Other\n")
	ref, _ := parsePage("docs/REF.md", strings.Replace(goodFront, "narrative", "reference", 1)+generatedNote+"\n# Ref\n")
	quiet, _ := parsePage("docs/QUIET.md", strings.Replace(goodFront, "  - tool.go\n  - pkg/\n", "  - elsewhere/\n", 1)+"# Quiet\n")
	return []Page{guide, other, quiet, ref}
}

func TestComputeImpact(t *testing.T) {
	impact := ComputeImpact(impactPages(t), []string{
		"pkg/a.go", "eval_run.go", "pkg/a_test.go", "docs/OTHER.md", "docs/UNRELATED.md", "new.go", "new_test.go", "pkgs/x.txt",
	})
	if len(impact.Pages) != 3 {
		t.Fatalf("pages = %+v", impact.Pages)
	}
	guide, other, ref := impact.Pages[0], impact.Pages[1], impact.Pages[2]
	if guide.Path != "docs/GUIDE.md" || guide.State != NeedsReview || strings.Join(guide.Changed, ",") != "eval_run.go,pkg/a.go" {
		t.Errorf("guide = %+v", guide)
	}
	if other.Path != "docs/OTHER.md" || other.State != Updated || len(other.Changed) != 1 {
		t.Errorf("other = %+v", other)
	}
	if ref.Path != "docs/REF.md" || ref.State != Regenerated {
		t.Errorf("ref = %+v", ref)
	}
	if strings.Join(impact.Uncovered, ",") != "new.go" {
		t.Errorf("uncovered = %v", impact.Uncovered)
	}

	text := impact.Markdown()
	for _, want := range []string{
		ImpactMarker,
		"**1 page covers code this change touches and was not updated.**",
		"| A page (`docs/GUIDE.md`) | Needs review | `eval_run.go`, `pkg/a.go` |",
		"| Updated in this change |",
		"| Generated |",
		"Changed Go files that no page covers: `new.go`.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("want %q in:\n%s", want, text)
		}
	}
}

func TestImpactWithNothingToSay(t *testing.T) {
	// Only tests and docs changed: no page is affected.
	quiet := ComputeImpact(impactPages(t), []string{"pkg/a_test.go", "docs/GUIDE.md", "README.md"})
	if len(quiet.Pages) != 0 || len(quiet.Uncovered) != 0 {
		t.Fatalf("impact = %+v", quiet)
	}
	if text := quiet.Markdown(); !strings.Contains(text, "touches no code that a documentation page covers") || strings.Contains(text, "| Page |") {
		t.Errorf("markdown:\n%s", text)
	}
	if encoded, _ := json.Marshal(quiet); !strings.Contains(string(encoded), `"pages":[]`) {
		t.Errorf("empty lists must encode as [], got %s", encoded)
	}
	done := ComputeImpact(impactPages(t), []string{"pkg/a.go", "docs/GUIDE.md", "docs/OTHER.md"})
	if text := done.Markdown(); !strings.Contains(text, "No page needs review") {
		t.Errorf("markdown:\n%s", text)
	}
	many := ComputeImpact(impactPages(t), []string{"pkg/a.go"})
	if text := many.Markdown(); !strings.Contains(text, "**2 pages cover code") {
		t.Errorf("markdown:\n%s", text)
	}
}

func TestSiteMarksAffectedPages(t *testing.T) {
	root := repo(t)
	impact := &Impact{Pages: []PageImpact{{Path: "docs/PAGE.md", Title: "A page", State: NeedsReview, Changed: []string{"tool.go", "pkg/<a>.go"}}}}
	files, err := BuildSite(root, SiteOptions{Impact: impact})
	if err != nil {
		t.Fatal(err)
	}
	page := string(files["PAGE.html"])
	for _, want := range []string{
		`<aside class="impact needs-review">`,
		"Needs review: not updated in this pull request",
		"<code>tool.go</code>, <code>pkg/&lt;a&gt;.go</code>",
		`<span class="mark needs-review"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("want %q in page", want)
		}
	}
	if other := string(files["OTHER.html"]); strings.Contains(other, `class="impact`) {
		t.Error("an unaffected page carries the banner")
	}
	if home := string(files["index.html"]); !strings.Contains(home, "Affected by this pull request") {
		t.Error("home page does not list affected pages")
	}
	plainFiles, _ := BuildSite(root, SiteOptions{})
	if home := string(plainFiles["index.html"]); strings.Contains(home, "Affected by this pull request") || strings.Contains(home, `class="mark`) {
		t.Error("a normal build shows pull-request marks")
	}
}

// A pull request's tree is read as data and its pages are published. A link
// that leaves the repository must never be followed.
func TestLinksOutOfTheRepositoryAreRefused(t *testing.T) {
	secret := filepath.Join(t.TempDir(), "credential")
	if err := os.WriteFile(secret, []byte(goodFront+"\nTOKEN=hunter2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := func(t *testing.T, root, rel, target string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		_ = os.Remove(full)
		if err := os.Symlink(target, full); err != nil {
			t.Skip("symbolic links are not available:", err)
		}
	}

	t.Run("page", func(t *testing.T) {
		root := repo(t)
		link(t, root, "docs/LEAK.md", secret)
		if _, _, err := LoadPages(root); err == nil || !strings.Contains(err.Error(), "outside the repository") {
			t.Fatalf("LoadPages: %v", err)
		}
		files, err := BuildSite(root, SiteOptions{})
		if err == nil {
			t.Fatalf("the site was built; md/LEAK.md = %q", files["md/LEAK.md"])
		}
		if _, err := Check(root); err == nil {
			t.Fatal("Check followed the link")
		}
	})
	t.Run("docs directory", func(t *testing.T) {
		root := t.TempDir()
		link(t, root, "docs", filepath.Dir(secret))
		if _, _, err := LoadPages(root); err == nil {
			t.Fatal("LoadPages followed a linked docs directory")
		}
	})
	t.Run("source and action files", func(t *testing.T) {
		root := repo(t)
		link(t, root, "main.go", secret)
		if _, err := Commands(root); err == nil {
			t.Error("Commands followed the link")
		}
		root = repo(t)
		link(t, root, "sub/action.yml", secret)
		if _, err := Actions(root); err == nil {
			t.Error("Actions followed the link")
		}
		root = repo(t)
		link(t, root, findingSchemaFile, secret)
		if _, err := Reference(root); err == nil {
			t.Error("Reference followed the link")
		}
	})
	t.Run("a link that stays inside is allowed", func(t *testing.T) {
		root := repo(t)
		link(t, root, "docs/ALIAS.md", "OTHER.md")
		if _, _, err := LoadPages(root); err != nil {
			t.Fatalf("LoadPages: %v", err)
		}
	})
}
