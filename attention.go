package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
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

func (m *reviewManager) Attention() (reviewAttention, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil {
		return reviewAttention{}, errors.New("no active review session")
	}
	current, _, err := snapshotTree(m.root, "")
	if err != nil {
		return reviewAttention{}, err
	}
	baseline := make(map[string]reviewFile, len(m.active.Files))
	now := make(map[string]reviewFile, len(current))
	for _, file := range m.active.Files {
		baseline[file.Path] = file
	}
	for _, file := range current {
		now[file.Path] = file
	}
	paths := make([]string, 0)
	for path, old := range baseline {
		if next, ok := now[path]; !ok || old.Hash != next.Hash || old.Mode != next.Mode {
			paths = append(paths, path)
		}
	}
	for path := range now {
		if _, ok := baseline[path]; !ok {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	truncated := len(paths) > attentionMaxFiles
	if truncated {
		paths = paths[:attentionMaxFiles]
	}
	candidates := make([]attentionCandidate, 0, len(paths))
	remaining := int64(attentionMaxContentBytes)
	contentTruncated := false
	baselineRoot := filepath.Join(m.sessionDir(m.active.ID), "files")
	for _, path := range paths {
		old, hadOld := baseline[path]
		next, hasNext := now[path]
		candidate := attentionCandidate{Path: path, Deleted: !hasNext}
		if hadOld {
			candidate.BaselineMode = old.Mode
		}
		if hasNext {
			candidate.CurrentMode = next.Mode
		}
		bytesNeeded := old.Size + next.Size
		if old.Size <= attentionMaxFileBytes && next.Size <= attentionMaxFileBytes && bytesNeeded <= remaining {
			var readErr error
			if hadOld {
				candidate.Baseline, readErr = os.ReadFile(filepath.Join(baselineRoot, filepath.FromSlash(path)))
			}
			if readErr == nil && hasNext {
				candidate.Current, readErr = os.ReadFile(filepath.Join(m.root, filepath.FromSlash(path)))
			}
			if readErr == nil && !containsNUL(candidate.Baseline) && !containsNUL(candidate.Current) {
				candidate.ContentAvailable = true
				remaining -= bytesNeeded
			}
		} else {
			contentTruncated = true
		}
		candidates = append(candidates, candidate)
	}
	dismissed := make(map[string]bool, len(m.active.DismissedAttention))
	for _, id := range m.active.DismissedAttention {
		dismissed[id] = true
	}
	attention := detectReviewAttention(candidates, dismissed)
	attention.Truncated = attention.Truncated || truncated || contentTruncated
	return attention, nil
}

func (m *reviewManager) DismissAttention(id string) (reviewAttention, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return reviewAttention{}, errors.New("attention flag id is required")
	}
	current, err := m.Attention()
	if err != nil {
		return reviewAttention{}, err
	}
	found := false
	for _, flag := range current.Flags {
		if flag.ID == id {
			found = true
			break
		}
	}
	if !found {
		return reviewAttention{}, errors.New("attention flag is no longer current")
	}
	m.mu.Lock()
	if m.active == nil {
		m.mu.Unlock()
		return reviewAttention{}, errors.New("no active review session")
	}
	m.active.DismissedAttention = append(m.active.DismissedAttention, id)
	if err := m.saveLocked(); err != nil {
		m.mu.Unlock()
		return reviewAttention{}, err
	}
	m.mu.Unlock()
	return m.Attention()
}

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

func (s *Server) handleReviewAttention(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		fail(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	attention, err := s.review.Attention()
	if err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, attention)
}

func (s *Server) handleReviewAttentionDismiss(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	attention, err := s.review.DismissAttention(r.URL.Query().Get("id"))
	if err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, attention)
}
