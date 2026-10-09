package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// staticReviewReport is the portable, read-only representation consumed by
// the generated report. It deliberately contains commit identities so a
// published page cannot silently drift when a pull request receives another
// commit.
type staticReviewReport struct {
	Version          int                    `json:"version"`
	Repository       string                 `json:"repository"`
	Base             string                 `json:"base"`
	Head             string                 `json:"head"`
	GeneratedAt      time.Time              `json:"generatedAt"`
	Files            []staticReviewFile     `json:"files"`
	Blocks           []staticReviewBlock    `json:"blocks"`
	AffectedFiles    []staticAffectedFile   `json:"affectedFiles"`
	AffectedAnalysis staticAffectedAnalysis `json:"affectedAnalysis"`
	RuleHits         []staticReviewHit      `json:"ruleHits"`
	Explanations     []staticExplanation    `json:"explanations"`
	// Attention lists the risk signals px1 raises on the diff itself, such as
	// workflow permission changes or new public API. Snapshots written before
	// the field existed decode to nil, and the page renders that as none.
	Attention    []reviewAttentionFlag `json:"attention"`
	Verification verificationResponse  `json:"verification"`
}

type staticReviewFile struct {
	Path   string             `json:"path"`
	Status string             `json:"status"`
	Diff   string             `json:"diff"`
	Hunks  []staticReviewHunk `json:"hunks"`
}

type staticReviewHunk struct {
	ID     string                 `json:"id"`
	Header string                 `json:"header"`
	Lines  []staticReviewDiffLine `json:"lines"`
}

type staticReviewDiffLine struct {
	OldLine int    `json:"oldLine,omitempty"`
	NewLine int    `json:"newLine,omitempty"`
	Kind    string `json:"kind"` // context, added, or removed
	Text    string `json:"text"`
}

type staticReviewHit struct {
	Key      string                    `json:"key"`
	RuleID   string                    `json:"ruleId"`
	Message  string                    `json:"message"`
	Title    string                    `json:"title,omitempty"`
	Why      string                    `json:"why,omitempty"`
	Severity string                    `json:"severity,omitempty"`
	Source   string                    `json:"source"`
	Origin   string                    `json:"origin,omitempty"`
	Path     string                    `json:"path"`
	Line     int                       `json:"line"`
	Text     string                    `json:"text"`
	Context  []staticReviewContextLine `json:"context,omitempty"`
}

type staticReviewContextLine struct {
	Number int    `json:"number"`
	Kind   string `json:"kind"` // added, context, or removed
	Text   string `json:"text"`
}

type staticReviewContextBlock struct {
	Lines []staticReviewContextLine
}

func runExportReview(args []string) error {
	fs := flag.NewFlagSet("export-review", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	base := fs.String("base", "", "base commit or ref (required)")
	head := fs.String("head", "HEAD", "head commit or ref")
	root := fs.String("root", ".", "repository root")
	out := fs.String("out", ".px1-review", "directory to write the static report")
	snapshotOut := fs.String("snapshot-out", "", "optional path to write the versioned review snapshot JSON")
	verificationFile := fs.String("verification-file", "", "optional verification JSON file to validate for the head commit")
	explanationsFile := fs.String("explanations-file", "", "optional AI explanation JSON file to validate for the head commit")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: px1 export-review --base <commit> [--head <commit>] [--root <repo>] [--verification-file <path>] [--explanations-file <path>] [--snapshot-out <file>] [--out <dir>]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*base) == "" {
		return errors.New("export-review requires --base")
	}

	repoRoot, err := filepath.Abs(*root)
	if err != nil {
		return fmt.Errorf("resolve root: %w", err)
	}
	outDir, err := filepath.Abs(*out)
	if err != nil {
		return fmt.Errorf("resolve output: %w", err)
	}
	report, err := generateStaticReviewWithOptions(repoRoot, *base, *head, staticReviewOptions{VerificationFile: *verificationFile, ExplanationsFile: *explanationsFile})
	if err != nil {
		return err
	}
	if err := writeStaticReviewReport(outDir, report); err != nil {
		return err
	}
	if *snapshotOut != "" {
		if err := writeStaticReviewSnapshot(*snapshotOut, report); err != nil {
			return err
		}
	}
	fmt.Printf("static review report written to %s\n", filepath.Join(outDir, "index.html"))
	return nil
}

func writeStaticReviewSnapshot(path string, report staticReviewReport) error {
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode review snapshot: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create snapshot directory: %w", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("write review snapshot: %w", err)
	}
	return nil
}

func generateStaticReview(root, base, head string) (staticReviewReport, error) {
	return generateStaticReviewWithOptions(root, base, head, staticReviewOptions{})
}

func generateStaticReviewWithVerification(root, base, head, verificationFile string) (staticReviewReport, error) {
	return generateStaticReviewWithOptions(root, base, head, staticReviewOptions{VerificationFile: verificationFile})
}

type staticReviewOptions struct {
	VerificationFile string
	ExplanationsFile string
}

func generateStaticReviewWithOptions(root, base, head string, options staticReviewOptions) (staticReviewReport, error) {
	baseSHA, err := resolveCommit(root, base)
	if err != nil {
		return staticReviewReport{}, err
	}
	headSHA, err := resolveCommit(root, head)
	if err != nil {
		return staticReviewReport{}, err
	}
	diff, err := gitDiffBetween(root, baseSHA, headSHA)
	if err != nil {
		return staticReviewReport{}, err
	}
	files, err := parseStaticReviewFiles(root, baseSHA, headSHA, diff)
	if err != nil {
		return staticReviewReport{}, err
	}
	rules, err := teamRulesAtCommit(root, headSHA)
	if err != nil {
		return staticReviewReport{}, err
	}
	hits, err := matchStaticRuleAdditions(rules, addedLinesByPath(diff))
	if err != nil {
		return staticReviewReport{}, err
	}
	staticHits := addStaticReviewContexts(hits, diff)
	verification, err := verificationAtCommit(root, headSHA)
	if options.VerificationFile != "" {
		verification, err = verificationAtFile(options.VerificationFile, headSHA)
	}
	if err != nil {
		return staticReviewReport{}, err
	}
	changed := make(map[string]bool, len(files))
	for _, file := range files {
		changed[file.Path] = true
	}
	explanations := []staticExplanation{}
	if options.ExplanationsFile != "" {
		explanations, err = explanationsAtFile(options.ExplanationsFile, headSHA, changed)
	}
	if err != nil {
		return staticReviewReport{}, err
	}
	flags, err := staticAttention(root, baseSHA, headSHA)
	if err != nil {
		return staticReviewReport{}, err
	}
	affected, affectedAnalysis, err := affectedFileHints(root, headSHA, files)
	if err != nil {
		return staticReviewReport{}, err
	}
	return staticReviewReport{
		Version:          1,
		Repository:       filepath.Base(root),
		Base:             baseSHA,
		Head:             headSHA,
		GeneratedAt:      time.Now().UTC(),
		Files:            files,
		Blocks:           relatedReviewBlocks(files),
		AffectedFiles:    affected,
		AffectedAnalysis: affectedAnalysis,
		RuleHits:         staticHits,
		Explanations:     explanations,
		Attention:        flags,
		Verification:     verification,
	}, nil
}

func gitDiffBetween(root, base, head string) (string, error) {
	cmd := exec.Command("git", "-C", root, "diff", "--no-color", "--no-ext-diff", "--unified=3", base, head, "--")
	out, err := cmd.Output()
	if err == nil {
		return string(out), nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		// git diff uses exit status 1 to mean that differences exist when
		// --exit-code is enabled; tolerate it defensively for wrappers too.
		return string(out), nil
	}
	return "", fmt.Errorf("git diff %s..%s: %w", shortRevision(base), shortRevision(head), err)
}

func gitFileAtCommit(root, commit, rel string) ([]byte, bool, error) {
	out, err := exec.Command("git", "-C", root, "show", commit+":"+filepath.ToSlash(rel)).Output()
	if err == nil {
		return out, true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 128 {
		return nil, false, nil
	}
	return nil, false, fmt.Errorf("read %s at %s: %w", rel, shortRevision(commit), err)
}

func teamRulesAtCommit(root, head string) ([]reviewRule, error) {
	b, found, err := gitFileAtCommit(root, head, ruleTeamFile)
	if err != nil || !found {
		return nil, err
	}
	return parseTeamRules(b)
}

func verificationAtCommit(root, head string) (verificationResponse, error) {
	b, found, err := gitFileAtCommit(root, head, ".px1/verification.json")
	if err != nil {
		return verificationResponse{}, err
	}
	if !found {
		return verificationResponse{Available: false, Checks: []verificationCheck{}}, nil
	}
	return verificationFromBytes(b, head, ".px1/verification.json")
}

func verificationAtFile(path, head string) (verificationResponse, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return verificationResponse{}, fmt.Errorf("read verification file %s: %w", path, err)
	}
	return verificationFromBytes(b, head, filepath.ToSlash(path))
}

func verificationFromBytes(b []byte, head, label string) (verificationResponse, error) {
	var report verificationReport
	if err := json.Unmarshal(b, &report); err != nil {
		return verificationResponse{Available: false, Checks: []verificationCheck{}, Error: fmt.Sprintf("invalid %s: %v", label, err)}, nil
	}
	if err := validateVerificationReport(report, head); err != nil {
		return verificationResponse{Available: false, Source: report.Source, Revision: report.Revision, URL: report.URL, Checks: []verificationCheck{}, Error: err.Error()}, nil
	}
	return verificationResponse{Available: true, Source: report.Source, Revision: report.Revision, URL: report.URL, Checks: nonNilChecks(report.Checks)}, nil
}

func parseStaticReviewFiles(root, base, head, diff string) ([]staticReviewFile, error) {
	statusOut, err := exec.Command("git", "-C", root, "diff", "--name-status", "--find-renames", base, head, "--").Output()
	if err != nil {
		return nil, fmt.Errorf("list changed files: %w", err)
	}
	statuses := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(statusOut)), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 2 || fields[0] == "" {
			continue
		}
		status := string(fields[0][0])
		path := fields[len(fields)-1]
		statuses[filepath.ToSlash(path)] = status
	}

	diffs := map[string]string{}
	for _, block := range strings.Split(diff, "diff --git ") {
		if strings.TrimSpace(block) == "" {
			continue
		}
		block = "diff --git " + block
		path := staticDiffPath(block)
		if path != "" {
			diffs[path] = block
			if _, ok := statuses[path]; !ok {
				statuses[path] = staticDiffStatus(block)
			}
		}
	}
	paths := make([]string, 0, len(statuses))
	for p := range statuses {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	files := make([]staticReviewFile, 0, len(paths))
	for _, p := range paths {
		files = append(files, staticReviewFile{Path: p, Status: statuses[p], Diff: diffs[p], Hunks: parseStaticReviewHunks(p, diffs[p])})
	}
	return files, nil
}

var staticReviewHunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@.*$`)

func parseStaticReviewHunks(path, diff string) []staticReviewHunk {
	var hunks []staticReviewHunk
	var current *staticReviewHunk
	oldLine, newLine := 0, 0
	for _, raw := range strings.Split(diff, "\n") {
		if match := staticReviewHunkHeader.FindStringSubmatch(raw); match != nil {
			oldLine, _ = strconv.Atoi(match[1])
			newLine, _ = strconv.Atoi(match[2])
			sum := sha256.Sum256([]byte(path + "\x00" + raw))
			hunks = append(hunks, staticReviewHunk{ID: fmt.Sprintf("diff-%x", sum[:8]), Header: raw, Lines: []staticReviewDiffLine{}})
			current = &hunks[len(hunks)-1]
			continue
		}
		if current == nil || raw == "" || strings.HasPrefix(raw, `\`) {
			continue
		}
		switch raw[0] {
		case ' ':
			current.Lines = append(current.Lines, staticReviewDiffLine{OldLine: oldLine, NewLine: newLine, Kind: "context", Text: raw[1:]})
			oldLine++
			newLine++
		case '-':
			current.Lines = append(current.Lines, staticReviewDiffLine{OldLine: oldLine, Kind: "removed", Text: raw[1:]})
			oldLine++
		case '+':
			current.Lines = append(current.Lines, staticReviewDiffLine{NewLine: newLine, Kind: "added", Text: raw[1:]})
			newLine++
		}
	}
	if hunks == nil {
		return []staticReviewHunk{}
	}
	return hunks
}

func staticDiffPath(diff string) string {
	var deleted string
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+++ b/"):
			return filepath.ToSlash(strings.TrimPrefix(line, "+++ b/"))
		case strings.HasPrefix(line, "--- a/"):
			deleted = filepath.ToSlash(strings.TrimPrefix(line, "--- a/"))
		}
	}
	return deleted
}

func staticDiffStatus(diff string) string {
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "new file mode") {
			return "A"
		}
		if strings.HasPrefix(line, "deleted file mode") {
			return "D"
		}
	}
	return "M"
}

func addedLinesByPath(diff string) map[string]map[int]string {
	out := map[string]map[int]string{}
	for _, block := range strings.Split(diff, "diff --git ") {
		if strings.TrimSpace(block) == "" {
			continue
		}
		block = "diff --git " + block
		file := staticDiffPath(block)
		if file == "" || strings.HasPrefix(file, ".px1/") {
			continue
		}
		added := map[int]string{}
		line := 0
		for _, raw := range strings.Split(block, "\n") {
			if m := hunkHeader.FindStringSubmatch(raw); m != nil {
				line, _ = strconv.Atoi(m[1])
				continue
			}
			if line == 0 || raw == "" || strings.HasPrefix(raw, `\\`) || strings.HasPrefix(raw, "+++") || strings.HasPrefix(raw, "---") {
				continue
			}
			switch raw[0] {
			case '+':
				added[line] = raw[1:]
				line++
			case ' ':
				line++
			}
		}
		out[file] = added
	}
	return out
}

func addStaticReviewContexts(hits []ruleHit, diff string) []staticReviewHit {
	blocks := staticContextBlocksByPath(diff)
	out := make([]staticReviewHit, 0, len(hits))
	for _, hit := range hits {
		staticHit := staticReviewHit{
			Key:      hit.Key,
			RuleID:   hit.RuleID,
			Message:  hit.Message,
			Title:    hit.Title,
			Why:      hit.Why,
			Severity: hit.Severity,
			Source:   hit.Source,
			Origin:   hit.Origin,
			Path:     hit.Path,
			Line:     hit.Line,
			Text:     hit.Text,
		}
		for _, block := range blocks[hit.Path] {
			target := -1
			for i, line := range block.Lines {
				if line.Kind == "added" && line.Number == hit.Line {
					target = i
					break
				}
			}
			if target < 0 {
				continue
			}
			start, end := target-2, target+3
			if start < 0 {
				start = 0
			}
			if end > len(block.Lines) {
				end = len(block.Lines)
			}
			staticHit.Context = append(staticHit.Context, block.Lines[start:end]...)
			break
		}
		out = append(out, staticHit)
	}
	return out
}

func staticContextBlocksByPath(diff string) map[string][]staticReviewContextBlock {
	out := map[string][]staticReviewContextBlock{}
	for _, rawBlock := range strings.Split(diff, "diff --git ") {
		if strings.TrimSpace(rawBlock) == "" {
			continue
		}
		block := "diff --git " + rawBlock
		file := staticDiffPath(block)
		if file == "" || strings.HasPrefix(file, ".px1/") {
			continue
		}
		lineNumber := 0
		var lines []staticReviewContextLine
		flush := func() {
			if len(lines) > 0 {
				out[file] = append(out[file], staticReviewContextBlock{Lines: lines})
				lines = nil
			}
		}
		for _, raw := range strings.Split(block, "\n") {
			if m := hunkHeader.FindStringSubmatch(raw); m != nil {
				flush()
				lineNumber, _ = strconv.Atoi(m[1])
				continue
			}
			if lineNumber == 0 || raw == "" || strings.HasPrefix(raw, `\`) {
				continue
			}
			switch raw[0] {
			case '+':
				if strings.HasPrefix(raw, "+++") {
					continue
				}
				lines = append(lines, staticReviewContextLine{Number: lineNumber, Kind: "added", Text: raw[1:]})
				lineNumber++
			case '-':
				if strings.HasPrefix(raw, "---") {
					continue
				}
				lines = append(lines, staticReviewContextLine{Kind: "removed", Text: raw[1:]})
			case ' ':
				lines = append(lines, staticReviewContextLine{Number: lineNumber, Kind: "context", Text: raw[1:]})
				lineNumber++
			}
		}
		flush()
	}
	return out
}

func matchStaticRuleAdditions(rules []reviewRule, additions map[string]map[int]string) ([]ruleHit, error) {
	type compiledRule struct {
		rule  reviewRule
		re    *regexp.Regexp
		globs []*regexp.Regexp
	}
	active := make([]compiledRule, 0, len(rules))
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		re, err := regexp.Compile(rule.Pattern)
		if err != nil {
			continue
		}
		patterns := rule.Globs
		if len(patterns) == 0 && rule.Glob != "" {
			patterns = []string{rule.Glob}
		}
		globs := make([]*regexp.Regexp, 0, len(patterns))
		for _, pattern := range patterns {
			glob, err := globRegexp(pattern)
			if err != nil {
				continue
			}
			if glob != nil {
				globs = append(globs, glob)
			}
		}
		active = append(active, compiledRule{rule: rule, re: re, globs: globs})
	}
	paths := make([]string, 0, len(additions))
	for p := range additions {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	hits := make([]ruleHit, 0)
	for _, p := range paths {
		for _, c := range active {
			if len(c.globs) > 0 && !matchesAnyGlob(c.globs, p) {
				continue
			}
			lines := make([]int, 0, len(additions[p]))
			for line := range additions[p] {
				lines = append(lines, line)
			}
			sort.Ints(lines)
			if strings.HasPrefix(c.rule.MatchKind, "block-") {
				for _, block := range contiguousAddedBlocks(lines) {
					texts := make([]string, 0, len(block))
					for _, line := range block {
						texts = append(texts, additions[p][line])
					}
					joined := strings.Join(texts, "\n")
					match := c.re.FindStringIndex(joined)
					if match == nil {
						continue
					}
					lineOffset := strings.Count(joined[:match[0]], "\n")
					line := block[lineOffset]
					key := ruleHitKey(c.rule.ID, p, joined)
					hits = append(hits, newRuleHit(c.rule, key, p, line, clip(joined, 300)))
					if len(hits) >= ruleMaxHits {
						return hits, nil
					}
				}
				continue
			}
			perKey := map[string]int{}
			for _, line := range lines {
				text := additions[p][line]
				if !c.re.MatchString(text) {
					continue
				}
				key := ruleHitKey(c.rule.ID, p, text)
				if perKey[key] >= ruleMaxHitsPerKey {
					continue
				}
				perKey[key]++
				hits = append(hits, newRuleHit(c.rule, key, p, line, clip(text, 300)))
				if len(hits) >= ruleMaxHits {
					return hits, nil
				}
			}
		}
	}
	return hits, nil
}

func contiguousAddedBlocks(lines []int) [][]int {
	blocks := [][]int{}
	for _, line := range lines {
		if len(blocks) == 0 || line > blocks[len(blocks)-1][len(blocks[len(blocks)-1])-1]+1 {
			blocks = append(blocks, []int{line})
			continue
		}
		blocks[len(blocks)-1] = append(blocks[len(blocks)-1], line)
	}
	return blocks
}

func newRuleHit(rule reviewRule, key, path string, line int, text string) ruleHit {
	return ruleHit{Key: key, RuleID: rule.ID, Message: rule.Message, Title: rule.Title, Why: rule.Why, Severity: rule.Severity, Source: rule.Source, Origin: rule.Origin, Path: path, Line: line, Text: text}
}

func matchesAnyGlob(globs []*regexp.Regexp, path string) bool {
	for _, glob := range globs {
		if glob.MatchString(path) || glob.MatchString(filepath.Base(path)) {
			return true
		}
	}
	return false
}

func writeStaticReviewReport(outDir string, report staticReviewReport) error {
	html, err := renderStaticReviewHTML(report)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode machine-readable report: %w", err)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "index.html"), html, 0o644); err != nil {
		return fmt.Errorf("write static report: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "review.json"), append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write machine-readable report: %w", err)
	}
	return nil
}
