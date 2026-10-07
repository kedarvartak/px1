package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	attentionMaxFiles        = 500
	attentionMaxFileBytes    = 512 << 10
	attentionMaxContentBytes = 8 << 20
	attentionLargeLineDelta  = 400
	attentionMaxFlags        = 250
)

type reviewAttentionFlag struct {
	ID       string `json:"id"`
	Rule     string `json:"rule"`
	Title    string `json:"title"`
	Reason   string `json:"reason"`
	Path     string `json:"path"`
	Line     int    `json:"line,omitempty"`
	Evidence string `json:"evidence"`
}

type reviewAttention struct {
	Flags     []reviewAttentionFlag `json:"flags"`
	Truncated bool                  `json:"truncated"`
}

type attentionCandidate struct {
	Path              string
	Deleted           bool
	BaselineMode      fs.FileMode
	CurrentMode       fs.FileMode
	Baseline, Current []byte
	ContentAvailable  bool
}

var publicAPIAddedLine = regexp.MustCompile(`(?i)(HandleFunc\s*\(|\.(get|post|put|patch|delete)\s*\(|@(app|router)\.(get|post|put|patch|delete)\b|\bexport\s+(async\s+)?(function|class|const|let|var)\b|^\s*func\s+[A-Z][A-Za-z0-9_]*\s*\()`)

func detectReviewAttention(candidates []attentionCandidate, dismissed map[string]bool) reviewAttention {
	flags := make([]reviewAttentionFlag, 0)
	truncated := false
	add := func(rule, title, reason, path string, line int, evidence string) {
		if len(flags) >= attentionMaxFlags {
			truncated = true
			return
		}
		sum := sha256.Sum256([]byte(rule + "\x00" + path + "\x00" + evidence))
		flag := reviewAttentionFlag{ID: "attention-" + hex.EncodeToString(sum[:8]), Rule: rule, Title: title, Reason: reason, Path: path, Line: line, Evidence: evidence}
		if !dismissed[flag.ID] {
			flags = append(flags, flag)
		}
	}
	for _, candidate := range candidates {
		path := filepath.ToSlash(candidate.Path)
		lower := strings.ToLower(path)
		base := strings.ToLower(filepath.Base(path))
		if isDependencyFile(base) {
			add("dependency", "Dependency change", "Dependency manifests and lockfiles can alter code throughout the build.", path, 0, base)
		}
		if hasPathWord(lower, "auth", "authentication", "authorization", "login", "session", "permission") {
			add("authentication", "Authentication path", "This path looks related to authentication or authorization.", path, 0, path)
		}
		if strings.Contains(lower, "/migrations/") || strings.HasPrefix(lower, "migrations/") || hasPathWord(lower, "schema") || strings.HasSuffix(base, ".sql") {
			add("schema", "Schema or migration", "Schema and migration changes can affect stored data and deployment order.", path, 0, path)
		}
		if isConfigurationFile(lower, base) {
			add("configuration", "Configuration change", "Configuration and environment files can change runtime behavior outside this diff.", path, 0, path)
		}
		if candidate.Deleted && isTestFile(lower, base) {
			add("deleted-test", "Deleted test", "A test file was removed, which may reduce verification coverage.", path, 0, path)
		}
		if isGeneratedFile(lower, base) {
			add("generated", "Generated file", "Generated output changed; review the source and regeneration path as well.", path, 0, path)
		}
		if candidate.BaselineMode.Perm()&0o111 != candidate.CurrentMode.Perm()&0o111 {
			evidence := fmt.Sprintf("mode %04o → %04o", candidate.BaselineMode.Perm(), candidate.CurrentMode.Perm())
			add("executable-mode", "Executable permission changed", "Executable-bit changes affect how the file can be run.", path, 0, evidence)
		}
		if !candidate.ContentAvailable {
			continue
		}
		added, removed := changedTextLines(candidate.Baseline, candidate.Current)
		if len(added)+removed >= attentionLargeLineDelta {
			add("large-diff", "Large diff", "A large textual change deserves an explicit review pass.", path, 0, fmt.Sprintf("%d added, %d removed lines", len(added), removed))
		}
		for _, line := range added {
			text := strings.TrimSpace(line.Text)
			if publicAPIAddedLine.MatchString(text) {
				add("public-api", "New endpoint or public API", "An added declaration looks externally callable or route-facing.", path, line.Number, clip(text, 180))
				break
			}
		}
	}
	sort.Slice(flags, func(i, j int) bool {
		if flags[i].Path != flags[j].Path {
			return flags[i].Path < flags[j].Path
		}
		if flags[i].Line != flags[j].Line {
			return flags[i].Line < flags[j].Line
		}
		return flags[i].Rule < flags[j].Rule
	})
	return reviewAttention{Flags: flags, Truncated: truncated}
}

type attentionLine struct {
	Number int
	Text   string
}

func changedTextLines(before, after []byte) ([]attentionLine, int) {
	oldLines := attentionTextLines(before)
	newLines := attentionTextLines(after)
	counts := map[string]int{}
	for _, line := range oldLines {
		counts[line]++
	}
	added := make([]attentionLine, 0)
	for i, line := range newLines {
		if counts[line] > 0 {
			counts[line]--
			continue
		}
		added = append(added, attentionLine{Number: i + 1, Text: line})
	}
	removed := 0
	for _, count := range counts {
		removed += count
	}
	return added, removed
}

func attentionTextLines(contents []byte) []string {
	text := strings.TrimSuffix(strings.ReplaceAll(string(contents), "\r\n", "\n"), "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func containsNUL(b []byte) bool { return bytes.IndexByte(b, 0) >= 0 }

func hasPathWord(path string, words ...string) bool {
	parts := strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '.' || r == '_' || r == '-' })
	for _, part := range parts {
		for _, word := range words {
			if part == word {
				return true
			}
		}
	}
	return false
}

func isDependencyFile(base string) bool {
	switch base {
	case "go.mod", "go.sum", "package.json", "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "cargo.toml", "cargo.lock", "pyproject.toml", "poetry.lock", "requirements.txt", "gemfile", "gemfile.lock", "composer.json", "composer.lock":
		return true
	}
	return strings.HasPrefix(base, "requirements-") && strings.HasSuffix(base, ".txt")
}

func isConfigurationFile(path, base string) bool {
	if base == ".env" || strings.HasPrefix(base, ".env.") || strings.HasSuffix(base, ".env") {
		return true
	}
	if hasPathWord(path, "config", "configuration") {
		return true
	}
	switch base {
	case "dockerfile", "docker-compose.yml", "docker-compose.yaml", "makefile", "procfile":
		return true
	}
	return false
}

func isTestFile(path, base string) bool {
	return strings.Contains(path, "/test/") || strings.Contains(path, "/tests/") || strings.HasPrefix(path, "test/") || strings.HasPrefix(path, "tests/") || strings.HasSuffix(base, "_test.go") || strings.Contains(base, ".test.") || strings.Contains(base, ".spec.")
}

func isGeneratedFile(path, base string) bool {
	return strings.Contains(path, "/generated/") || strings.HasPrefix(path, "generated/") || strings.Contains(base, ".generated.") || strings.Contains(base, ".gen.") || strings.HasSuffix(base, ".pb.go") || strings.HasSuffix(base, "_generated.go")
}
