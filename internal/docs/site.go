package docs

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
)

//go:embed webassets/docs.css webassets/docs.js
var siteAssets embed.FS

// SiteOptions configures the static documentation site.
type SiteOptions struct {
	// SourceURL is the web address of the repository tree the pages were
	// built from, such as https://github.com/owner/name/blob/<commit>. Links
	// to files outside docs/ point there. Empty leaves them unlinked.
	SourceURL string
	// Commit is the commit the site was built from, shown in the footer.
	Commit string
	// Fonts are woff2 files by name, written under fonts/. The stylesheet
	// falls back to system fonts without them.
	Fonts map[string][]byte
}

type navSection struct {
	Title string
	Pages []navPage
}

type navPage struct {
	Title, URL, Status string
	Kind               string
	Current            bool
}

type tocEntry struct{ ID, Text string }

type sitePage struct {
	Title    string
	Home     bool
	Nav      []navSection
	Page     Page
	Body     template.HTML
	TOC      []tocEntry
	Covers   []coverLink
	Markdown string
	Verified string
	Commit   string
	FontCSS  template.CSS
}

type coverLink struct{ Path, URL string }

type searchEntry struct {
	Title    string   `json:"title"`
	URL      string   `json:"url"`
	Section  string   `json:"section"`
	Headings []string `json:"headings"`
	Text     string   `json:"text"`
}

// pageURL is a page's address within the site: docs/NAME.md is NAME.html.
func pageURL(p Page) string {
	return strings.TrimSuffix(path.Base(p.Path), ".md") + ".html"
}

var markdown = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
)

var (
	hrefRE    = regexp.MustCompile(`href="([^"]*)"`)
	h2RE      = regexp.MustCompile(`(?s)<h2 id="([^"]+)">(.*?)</h2>`)
	headingRE = regexp.MustCompile(`(?s)<h[23][^>]*>(.*?)</h[23]>`)
	tagRE     = regexp.MustCompile(`(?s)<[^>]*>`)
	tableRE   = regexp.MustCompile(`(?s)<table>.*?</table>`)
)

func plain(fragment string) string {
	return strings.Join(strings.Fields(html.UnescapeString(tagRE.ReplaceAllString(fragment, " "))), " ")
}

// BuildSite renders every page to HTML and returns the site's files by path.
// Pages must already pass the check: BuildSite refuses front matter problems.
func BuildSite(root string, opts SiteOptions) (map[string][]byte, error) {
	pages, problems, err := LoadPages(root)
	if err != nil {
		return nil, err
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("%s (and %d more): run px1 docs check", problems[0], len(problems)-1)
	}
	isPage := map[string]bool{}
	for _, p := range pages {
		isPage[p.Path] = true
	}
	sourceURL := strings.TrimSuffix(opts.SourceURL, "/")

	files := map[string][]byte{}
	for _, name := range []string{"docs.css", "docs.js"} {
		data, err := siteAssets.ReadFile("webassets/" + name)
		if err != nil {
			return nil, err
		}
		files[name] = data
	}
	var fontCSS strings.Builder
	fontNames := make([]string, 0, len(opts.Fonts))
	for name := range opts.Fonts {
		fontNames = append(fontNames, name)
	}
	sort.Strings(fontNames)
	for _, name := range fontNames {
		files["fonts/"+name] = opts.Fonts[name]
		family, weight := "Report Sans", "400 600"
		if strings.Contains(name, "mono") {
			family, weight = "Report Mono", "400"
			if strings.Contains(name, "500") {
				weight = "500"
			}
		}
		fmt.Fprintf(&fontCSS, "@font-face{font-family:'%s';src:url(fonts/%s) format('woff2');font-weight:%s;font-display:swap}", family, name, weight)
	}

	nav := func(current string) []navSection {
		var sections []navSection
		for _, section := range Sections {
			s := navSection{Title: sectionTitles[section]}
			for _, p := range pages {
				if p.Section == section {
					s.Pages = append(s.Pages, navPage{Title: p.Title, URL: pageURL(p), Status: p.Status, Kind: p.Kind, Current: p.Path == current})
				}
			}
			if len(s.Pages) > 0 {
				sections = append(sections, s)
			}
		}
		return sections
	}

	var index []searchEntry
	for _, p := range pages {
		var buf bytes.Buffer
		if err := markdown.Convert([]byte(p.body), &buf); err != nil {
			return nil, fmt.Errorf("%s: %w", p.Path, err)
		}
		body := buf.String()

		// Links to other pages become links within the site; links to other
		// repository files go to the source when its address is known.
		body = hrefRE.ReplaceAllStringFunc(body, func(match string) string {
			target := html.UnescapeString(hrefRE.FindStringSubmatch(match)[1])
			if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "#") || strings.HasPrefix(target, "mailto:") {
				return match
			}
			file, fragment, _ := strings.Cut(target, "#")
			if fragment != "" {
				fragment = "#" + fragment
			}
			rel := path.Join(path.Dir(p.Path), file)
			switch {
			case isPage[rel]:
				target = strings.TrimSuffix(path.Base(rel), ".md") + ".html" + fragment
			case rel == Dir+"/"+IndexFile:
				target = "index.html"
			case sourceURL != "":
				target = sourceURL + "/" + rel + fragment
			default:
				return match
			}
			return `href="` + html.EscapeString(target) + `"`
		})
		// Wide tables scroll inside their own box instead of the page.
		body = tableRE.ReplaceAllStringFunc(body, func(table string) string {
			return `<div class="table">` + table + `</div>`
		})

		var toc []tocEntry
		for _, m := range h2RE.FindAllStringSubmatch(body, -1) {
			toc = append(toc, tocEntry{ID: m[1], Text: plain(m[2])})
		}
		var headings []string
		for _, m := range headingRE.FindAllStringSubmatch(body, -1) {
			headings = append(headings, plain(m[1]))
		}
		var covers []coverLink
		for _, entry := range p.Covers {
			link := coverLink{Path: entry}
			if sourceURL != "" && !strings.ContainsAny(entry, "*?[") {
				link.URL = sourceURL + "/" + entry
			}
			covers = append(covers, link)
		}
		verified := "Not yet verified against the code"
		if p.VerifiedAt != "" {
			verified = "Verified at " + p.VerifiedAt[:min(7, len(p.VerifiedAt))]
		}
		if p.Kind == "reference" && strings.Contains(p.body, generatedNote) {
			verified = "Generated from the code"
		}

		name := path.Base(p.Path)
		out, err := render(sitePage{
			Title: p.Title, Nav: nav(p.Path), Page: p, Body: template.HTML(body), TOC: toc,
			Covers: covers, Markdown: "md/" + name, Verified: verified, Commit: opts.Commit,
			FontCSS: template.CSS(fontCSS.String()),
		})
		if err != nil {
			return nil, err
		}
		files[pageURL(p)] = out

		// The Markdown is published next to the HTML, front matter included,
		// so the site is also a copy of the source.
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p.Path)))
		if err != nil {
			return nil, err
		}
		files["md/"+name] = raw

		index = append(index, searchEntry{
			Title: p.Title, URL: pageURL(p), Section: sectionTitles[p.Section],
			Headings: headings, Text: plain(body),
		})
	}

	home, err := render(sitePage{Title: "px1 documentation", Home: true, Nav: nav(""), Commit: opts.Commit, FontCSS: template.CSS(fontCSS.String())})
	if err != nil {
		return nil, err
	}
	files["index.html"] = home

	search, err := json.Marshal(index)
	if err != nil {
		return nil, err
	}
	files["search.json"] = search
	files["manifest.json"] = []byte(Generated(pages)[ManifestFile])
	return files, nil
}

func render(data sitePage) ([]byte, error) {
	var buf bytes.Buffer
	if err := siteTemplate.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// WriteSite builds the site into dir, which must be empty or not exist, so a
// stale page from an earlier build can never be published.
func WriteSite(root, dir string, opts SiteOptions) (int, error) {
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return 0, fmt.Errorf("%s is not empty", dir)
	}
	files, err := BuildSite(root, opts)
	if err != nil {
		return 0, err
	}
	for rel, data := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return 0, err
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			return 0, err
		}
	}
	return len(files), nil
}

var siteTemplate = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{if .Home}}px1 documentation{{else}}{{.Title}} · px1 docs{{end}}</title>
<style>{{.FontCSS}}</style>
<link rel="stylesheet" href="docs.css">
<script>try{var t=localStorage.getItem('px1-docs-theme');if(t==='light'||t==='dark')document.documentElement.dataset.theme=t}catch(e){}</script>
</head>
<body>
<header class="bar">
  <button class="icon-btn" id="menu" aria-label="Pages" aria-expanded="false">☰</button>
  <a class="logo" href="index.html">px1<span>documentation</span></a>
  <div class="search" role="search">
    <input id="q" type="search" placeholder="Search the docs  ( / )" aria-label="Search the documentation" autocomplete="off">
    <ul class="results" id="results" hidden></ul>
  </div>
  <button class="icon-btn" id="theme" aria-label="Switch between dark and light">◐</button>
</header>
<div class="shell">
  <nav class="rail" aria-label="Pages">
    {{range .Nav}}<h2>{{.Title}}</h2>
    {{range .Pages}}<a href="{{.URL}}"{{if .Current}} aria-current="page"{{end}}><span class="dot {{.Status}}" title="{{.Status}}"></span>{{.Title}}</a>
    {{end}}{{end}}
    {{if .Commit}}<p class="built-from">Built from <code>{{.Commit}}</code></p>{{end}}
  </nav>
  <main>
    {{if .Home}}<div class="doc">
      <h1>px1 documentation</h1>
      <p class="lede">How px1 reviews code, how to run it, and exactly what each command and input does. Every page says whether what it describes can be used today.</p>
      <p class="legend"><span><i class="dot shipped"></i>Shipped: usable now</span><span><i class="dot built"></i>Built: code and tests exist, not deployed</span><span><i class="dot"></i>Planned</span></p>
      {{range .Nav}}<h2>{{.Title}}</h2>
      <ul class="cards">
        {{range .Pages}}<li><a href="{{.URL}}"><strong><span class="dot {{.Status}}"></span>{{.Title}}</strong><small>{{.Status}} · {{.Kind}}</small></a></li>
        {{end}}
      </ul>
      {{end}}
    </div>{{else}}<article class="doc">
      <div class="meta">
        <span class="chip {{.Page.Status}}">{{.Page.Status}}</span>
        <span>{{.Page.Kind}}</span>
        <span>{{.Verified}}</span>
        <span class="grow"></span>
        <a href="{{.Markdown}}">Markdown</a>
      </div>
      {{.Body}}
      {{if .Covers}}<section class="covers">
        <h2>Code this page covers</h2>
        <p>When these files change, this page is checked.</p>
        <ul>{{range .Covers}}<li>{{if .URL}}<a href="{{.URL}}"><code>{{.Path}}</code></a>{{else}}<code>{{.Path}}</code>{{end}}</li>{{end}}</ul>
      </section>{{end}}
    </article>{{end}}
  </main>
  {{if .TOC}}<aside class="toc" aria-label="On this page">
    <h2>On this page</h2>
    {{range .TOC}}<a href="#{{.ID}}">{{.Text}}</a>
    {{end}}
  </aside>{{end}}
</div>
<script src="docs.js"></script>
</body>
</html>
`))
