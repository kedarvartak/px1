package docs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ConfigFile holds what the checks cannot derive: code that is knowingly
// undocumented, and non-Go paths that need a page.
const ConfigFile = Dir + "/docs.config.json"

// Config is docs/docs.config.json.
type Config struct {
	// KnownGaps are code units with no page yet. The check fails when new
	// code has no page and is not listed here, and when a listed gap has
	// been closed or removed, so the list can only shrink honestly.
	KnownGaps []string `json:"known_gaps"`
	// ExtraUnits are paths outside Go packages that need a page, such as an
	// action definition or the dashboard.
	ExtraUnits []string `json:"extra_units"`
	// Exclude are directories that are not px1 code.
	Exclude []string `json:"exclude"`
}

// LoadConfig reads the config. A missing file is an empty config.
func LoadConfig(root string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ConfigFile)))
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	// A misspelt key would silently drop the units it was meant to add.
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("%s: %w", ConfigFile, err)
	}
	return cfg, nil
}

// Check runs every check and returns the problems sorted by path and line.
func Check(root string) ([]Problem, error) {
	cfg, err := LoadConfig(root)
	if err != nil {
		return nil, err
	}
	pages, problems, err := LoadPages(root)
	if err != nil {
		return nil, err
	}
	for _, page := range pages {
		problems = append(problems, checkLinks(root, page)...)
		problems = append(problems, checkCovers(root, page)...)
	}
	units, err := CodeUnits(root, cfg)
	if err != nil {
		return nil, err
	}
	problems = append(problems, checkCoverage(root, pages, units, cfg)...)
	problems = append(problems, checkGenerated(root, pages)...)
	sort.SliceStable(problems, func(i, j int) bool {
		if problems[i].Path != problems[j].Path {
			return problems[i].Path < problems[j].Path
		}
		return problems[i].Line < problems[j].Line
	})
	return problems, nil
}

var linkRE = regexp.MustCompile(`\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)

// checkLinks reports relative links that do not resolve to a file in the
// repository. Links inside fenced code are examples, not links.
func checkLinks(root string, page Page) []Problem {
	var problems []Problem
	inFence := false
	for i, line := range strings.Split(page.body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		for _, m := range linkRE.FindAllStringSubmatch(line, -1) {
			target := m[1]
			if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") || strings.HasPrefix(target, "#") {
				continue
			}
			target, _, _ = strings.Cut(target, "#")
			var rel string
			if strings.HasPrefix(target, "/") {
				rel = strings.TrimPrefix(target, "/")
			} else {
				rel = path.Join(path.Dir(page.Path), target)
			}
			if rel == ".." || strings.HasPrefix(rel, "../") {
				problems = append(problems, Problem{page.Path, page.bodyStart + i, "link leaves the repository: " + m[1]})
				continue
			}
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
				problems = append(problems, Problem{page.Path, page.bodyStart + i, "broken link: " + m[1]})
			}
		}
	}
	return problems
}

// checkCovers reports covers entries that name nothing in the repository.
func checkCovers(root string, page Page) []Problem {
	var problems []Problem
	for _, entry := range page.Covers {
		if entry == "" || strings.HasPrefix(entry, "/") || strings.Contains(entry, "..") {
			problems = append(problems, Problem{page.Path, 1, fmt.Sprintf("covers entry %q must be a path inside the repository", entry)})
			continue
		}
		if strings.ContainsAny(entry, "*?[") {
			matches, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(entry)))
			if err != nil || len(matches) == 0 {
				problems = append(problems, Problem{page.Path, 1, "covers pattern matches nothing: " + entry})
			}
			continue
		}
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(entry)))
		switch {
		case err != nil:
			problems = append(problems, Problem{page.Path, 1, "covers a path that does not exist: " + entry})
		case info.IsDir() != strings.HasSuffix(entry, "/"):
			problems = append(problems, Problem{page.Path, 1, "covers entry must end in / exactly when it is a directory: " + entry})
		}
	}
	return problems
}

// CodeUnits lists what needs documentation: every Go package directory (as
// "dir/"), every non-test Go file of the root package (one command each, more
// or less), and the config's extra units.
func CodeUnits(root string, cfg Config) ([]string, error) {
	skip := map[string]bool{Dir: true, "node_modules": true, "testdata": true, "vendor": true}
	for _, dir := range cfg.Exclude {
		skip[strings.TrimSuffix(dir, "/")] = true
	}
	set := map[string]bool{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && (strings.HasPrefix(d.Name(), ".") || skip[d.Name()] || skip[rel]) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		if dir := path.Dir(rel); dir == "." {
			set[rel] = true
		} else {
			set[dir+"/"] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, unit := range cfg.ExtraUnits {
		set[unit] = true
	}
	units := make([]string, 0, len(set))
	for unit := range set {
		units = append(units, unit)
	}
	sort.Strings(units)
	return units, nil
}

// covered reports whether a covers entry describes a unit: the same path, a
// directory that contains the unit, a path inside the unit, or a glob that
// matches it.
func covered(unit, entry string) bool {
	if unit == entry {
		return true
	}
	if strings.HasSuffix(entry, "/") && strings.HasPrefix(unit, entry) {
		return true
	}
	if strings.HasSuffix(unit, "/") && strings.HasPrefix(entry, unit) {
		return true
	}
	if strings.ContainsAny(entry, "*?[") {
		if ok, _ := path.Match(entry, unit); ok {
			return true
		}
	}
	return false
}

// checkCoverage enforces the gap policy: code needs a page or an entry in
// known_gaps, and known_gaps holds nothing that is covered or gone.
func checkCoverage(root string, pages []Page, units []string, cfg Config) []Problem {
	isCovered := func(unit string) bool {
		for _, page := range pages {
			for _, entry := range page.Covers {
				if covered(unit, entry) {
					return true
				}
			}
		}
		return false
	}
	known := map[string]bool{}
	for _, gap := range cfg.KnownGaps {
		known[gap] = true
	}
	isUnit := map[string]bool{}
	var problems []Problem
	for _, unit := range units {
		isUnit[unit] = true
		if !isCovered(unit) && !known[unit] {
			problems = append(problems, Problem{ConfigFile, 0, fmt.Sprintf("%s has no page: add it to a page's covers, or to known_gaps", unit)})
		}
	}
	for _, gap := range cfg.KnownGaps {
		switch {
		case !isUnit[gap]:
			problems = append(problems, Problem{ConfigFile, 0, fmt.Sprintf("known gap %s is not a code unit any more: remove it", gap)})
		case isCovered(gap):
			problems = append(problems, Problem{ConfigFile, 0, fmt.Sprintf("known gap %s now has a page: remove it", gap)})
		}
	}
	for _, unit := range cfg.ExtraUnits {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(unit))); err != nil {
			problems = append(problems, Problem{ConfigFile, 0, "extra unit does not exist: " + unit})
		}
	}
	return problems
}

// checkGenerated reports a manifest or index that no longer matches the pages.
func checkGenerated(root string, pages []Page) []Problem {
	var problems []Problem
	for rel, want := range Generated(pages) {
		got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil || string(got) != want {
			problems = append(problems, Problem{rel, 0, "out of date: run px1 docs manifest"})
		}
	}
	return problems
}
