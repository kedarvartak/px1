package main

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"

	"px1/internal/hosted/agent"
	hostedreview "px1/internal/hosted/review"
)

const maxFileDiffBytes = 40_000

// defaultNoiseGlobs are files whose contents are generated or vendored, so
// reading them costs money and finds nothing a person would act on.
var defaultNoiseGlobs = []string{
	"package-lock.json", "yarn.lock", "pnpm-lock.yaml", "*.lock", "go.sum",
	"*.min.js", "*.min.css", "*.map", "dist/**", "node_modules/**", "vendor/**",
}

var privateKeyBlock = regexp.MustCompile(`-----BEGIN (?:[A-Z0-9 ]+ )?PRIVATE KEY-----[\s\S]*?-----END (?:[A-Z0-9 ]+ )?PRIVATE KEY-----`)

// privacyPolicy decides which paths may be shown to a model, for the diff and
// for every tool alike.
type privacyPolicy struct {
	globs   []*regexp.Regexp
	sources []string
	noise   int // globs[:noise] are the default noise globs
}

func newPrivacyPolicy(userGlobs []string) (privacyPolicy, error) {
	policy := privacyPolicy{noise: len(defaultNoiseGlobs)}
	for _, pattern := range append(append([]string{}, defaultNoiseGlobs...), userGlobs...) {
		glob, err := globRegexp(pattern)
		if err != nil {
			return privacyPolicy{}, fmt.Errorf("exclude glob %q: %w", pattern, err)
		}
		if glob == nil {
			continue
		}
		policy.globs = append(policy.globs, glob)
		policy.sources = append(policy.sources, pattern)
	}
	return policy, nil
}

// Allow reports whether a path may be shown to the model and, if not, why.
func (p privacyPolicy) Allow(path string) (bool, string) {
	if reason := sensitiveReportPath(path); reason != "" {
		return false, reason
	}
	for i, glob := range p.globs {
		if matchesAnyGlob([]*regexp.Regexp{glob}, path) {
			if i < p.noise {
				return false, "generated or dependency file"
			}
			return false, "matches --exclude-glob " + p.sources[i]
		}
	}
	return true, ""
}

// secretRedactor replaces secret-like values with a marker and counts them.
type secretRedactor struct{ count atomic.Int64 }

func (r *secretRedactor) Redact(text string) string {
	redact := func(pattern *regexp.Regexp, label string) {
		text = pattern.ReplaceAllStringFunc(text, func(string) string {
			r.count.Add(1)
			return "[redacted " + label + "]"
		})
	}
	redact(privateKeyBlock, "private key")
	for _, candidate := range highConfidenceSecretPatterns {
		redact(candidate.pattern, candidate.name)
	}
	return text
}

type excludedFile struct{ Path, Reason string }

// numberedDiff renders one file's diff the way the models are told to read it:
// added and context lines carry their new-file line number, removed lines are
// marked "-". It also returns the new-side ranges that were added.
func numberedDiff(file staticReviewFile) (string, []hostedreview.DiffRange) {
	var out strings.Builder
	var ranges []hostedreview.DiffRange
	for _, hunk := range file.Hunks {
		out.WriteString(hunk.Header + "\n")
		for _, line := range hunk.Lines {
			switch line.Kind {
			case "added":
				fmt.Fprintf(&out, "%d\t+%s\n", line.NewLine, line.Text)
				if n := len(ranges); n > 0 && ranges[n-1].EndLine == line.NewLine-1 {
					ranges[n-1].EndLine = line.NewLine
				} else {
					ranges = append(ranges, hostedreview.DiffRange{Path: file.Path, StartLine: line.NewLine, EndLine: line.NewLine})
				}
			case "context":
				fmt.Fprintf(&out, "%d\t %s\n", line.NewLine, line.Text)
			case "removed":
				fmt.Fprintf(&out, "-\t%s\n", line.Text)
			}
		}
	}
	text := strings.TrimRight(out.String(), "\n")
	if len(text) > maxFileDiffBytes {
		cut := maxFileDiffBytes
		for cut > 0 && text[cut]&0xC0 == 0x80 {
			cut--
		}
		text = text[:cut] + "\n[diff truncated by px1: use read_file for the rest]"
	}
	return text, ranges
}

// reviewLimits bound how much of a large change goes to the model. A change
// that exceeds them is reviewed in part, highest-risk files first, and says so.
type reviewLimits struct{ maxFiles, maxDiffBytes int }

const (
	defaultMaxReviewFiles     = 40
	defaultMaxReviewDiffBytes = 120_000
)

type reviewEntry struct {
	file   staticReviewFile
	review agent.ReviewFile
}

// filePriority ranks a path by how much a missed bug there would cost:
// pipelines, migrations and security-sensitive code first, then other source,
// then tests, then documentation and data.
func filePriority(path string) int {
	lower := strings.ToLower(path)
	for _, risky := range []string{".github/workflows", "dockerfile", "migration", "auth", "security", "secret", "crypto", "permission", "payment", "billing", "token", "password", ".sql"} {
		if strings.Contains(lower, risky) {
			return 3
		}
	}
	switch {
	case strings.HasSuffix(lower, ".md"), strings.HasSuffix(lower, ".txt"), strings.HasSuffix(lower, ".rst"), strings.HasPrefix(lower, "docs/"), strings.HasSuffix(lower, ".jsonl"), strings.HasSuffix(lower, ".json"):
		return 0
	case strings.Contains(lower, "test"), strings.Contains(lower, "spec."), strings.Contains(lower, "__tests__"):
		return 1
	}
	return 2
}

// selectWithinLimits keeps the highest-priority files that fit, preferring
// smaller diffs inside a priority so more files are covered. The kept files
// retain their original order. Limits of zero or less mean no limit.
func selectWithinLimits(entries []reviewEntry, limits reviewLimits) (kept []reviewEntry, skipped []excludedFile) {
	totalBytes := 0
	for _, e := range entries {
		totalBytes += len(e.review.Diff)
	}
	if (limits.maxFiles <= 0 || len(entries) <= limits.maxFiles) && (limits.maxDiffBytes <= 0 || totalBytes <= limits.maxDiffBytes) {
		return entries, nil
	}
	order := make([]int, len(entries))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		ea, eb := entries[order[a]], entries[order[b]]
		if pa, pb := filePriority(ea.file.Path), filePriority(eb.file.Path); pa != pb {
			return pa > pb
		}
		return len(ea.review.Diff) < len(eb.review.Diff)
	})
	keep := map[int]bool{}
	count, bytes := 0, 0
	for _, i := range order {
		size := len(entries[i].review.Diff)
		if (limits.maxFiles > 0 && count >= limits.maxFiles) || (limits.maxDiffBytes > 0 && bytes+size > limits.maxDiffBytes) {
			continue
		}
		keep[i] = true
		count++
		bytes += size
	}
	for i, e := range entries {
		if keep[i] {
			kept = append(kept, e)
		} else {
			skipped = append(skipped, excludedFile{Path: e.file.Path, Reason: "over the size limit"})
		}
	}
	return kept, skipped
}

type agenticPrep struct {
	BaseSHA, HeadSHA string
	Files            []agent.ReviewFile
	Blocks           []hostedreview.Block
	Excluded         []excludedFile
	Skipped          []excludedFile // reviewable, but left out of the diff for size
	TotalReviewable  int
	DiffBytes        int
	TruncatedFiles   []string
	Policy           privacyPolicy
}

// prepareAgenticReview reads the diff from git and decides what the models
// may see. It refuses to continue if the text bound for the model contains a
// high-confidence secret: excluding the file is the operator's decision.
func prepareAgenticReview(root, base, head string, policy privacyPolicy, limits reviewLimits) (agenticPrep, error) {
	baseSHA, err := resolveCommit(root, base)
	if err != nil {
		return agenticPrep{}, err
	}
	headSHA, err := resolveCommit(root, head)
	if err != nil {
		return agenticPrep{}, err
	}
	diff, err := gitDiffBetween(root, baseSHA, headSHA)
	if err != nil {
		return agenticPrep{}, err
	}
	all, err := parseStaticReviewFiles(root, baseSHA, headSHA, diff)
	if err != nil {
		return agenticPrep{}, err
	}
	prep := agenticPrep{BaseSHA: baseSHA, HeadSHA: headSHA, Policy: policy}
	var entries []reviewEntry
	for _, file := range all {
		if ok, reason := policy.Allow(file.Path); !ok {
			prep.Excluded = append(prep.Excluded, excludedFile{Path: file.Path, Reason: reason})
			continue
		}
		if len(file.Hunks) == 0 {
			prep.Excluded = append(prep.Excluded, excludedFile{Path: file.Path, Reason: "no text changes (binary, mode, or rename only)"})
			continue
		}
		text, ranges := numberedDiff(file)
		entries = append(entries, reviewEntry{file: file, review: agent.ReviewFile{Path: file.Path, Diff: text, Changed: ranges}})
	}
	prep.TotalReviewable = len(entries)
	// Limits apply before the secret scan: a file that is never sent cannot
	// block the run.
	entries, prep.Skipped = selectWithinLimits(entries, limits)
	var allowed []staticReviewFile
	secrets := map[string][]string{}
	for _, entry := range entries {
		file, text := entry.file, entry.review.Diff
		if strings.Contains(text, "[diff truncated by px1") {
			prep.TruncatedFiles = append(prep.TruncatedFiles, file.Path)
		}
		for _, candidate := range highConfidenceSecretPatterns {
			if candidate.pattern.MatchString(text) {
				secrets[file.Path] = append(secrets[file.Path], candidate.name)
			}
		}
		if privateKeyBlock.MatchString(text) {
			secrets[file.Path] = append(secrets[file.Path], "private key")
		}
		prep.Files = append(prep.Files, entry.review)
		prep.DiffBytes += len(text)
		allowed = append(allowed, file)
	}
	if len(secrets) > 0 {
		paths := make([]string, 0, len(secrets))
		for path := range secrets {
			paths = append(paths, fmt.Sprintf("%s (%s)", path, strings.Join(uniqueStrings(secrets[path]), ", ")))
		}
		sort.Strings(paths)
		return agenticPrep{}, fmt.Errorf("refusing to send likely secrets to a model: %s. Remove them or add --exclude-glob for those files; this check detects known secret shapes and is not redaction", strings.Join(paths, "; "))
	}
	if len(prep.Files) == 0 {
		return agenticPrep{}, errors.New("no reviewable text changes remain after exclusions")
	}
	covered := map[string]bool{}
	for _, block := range relatedReviewBlocks(allowed) {
		prep.Blocks = append(prep.Blocks, hostedreview.Block{ID: block.ID, Title: block.Title, Summary: block.Summary, Paths: block.Paths})
		for _, path := range block.Paths {
			covered[path] = true
		}
	}
	// Grouping only links related files, so a lone file belongs to no group.
	// Every reviewable file must be in some group or it would never be looked at.
	for i, file := range prep.Files {
		if !covered[file.Path] {
			prep.Blocks = append(prep.Blocks, hostedreview.Block{ID: fmt.Sprintf("file-%d", i+1), Title: file.Path, Summary: "Changes to " + file.Path, Paths: []string{file.Path}})
		}
	}
	return prep, nil
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := values[:0:0]
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
