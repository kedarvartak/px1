package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	explanationMaxItems = 200
	explanationMaxTitle = 80
	explanationMaxText  = 800
)

// staticExplanation is provider output rendered as a small, expandable chip.
// The exporter treats it as untrusted display data and pins it to one commit.
type staticExplanation struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	LineStart int    `json:"lineStart"`
	LineEnd   int    `json:"lineEnd"`
	Title     string `json:"title"`
	Summary   string `json:"summary"`
}

type staticExplanationFile struct {
	Version      int                 `json:"version"`
	Revision     string              `json:"revision"`
	Explanations []staticExplanation `json:"explanations"`
}

func explanationsAtFile(file, head string, changed map[string]bool) ([]staticExplanation, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("read explanations file %s: %w", file, err)
	}
	return parseStaticExplanations(b, head, changed, filepath.ToSlash(file))
}

func parseStaticExplanations(b []byte, head string, changed map[string]bool, label string) ([]staticExplanation, error) {
	var file staticExplanationFile
	if err := json.Unmarshal(b, &file); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", label, err)
	}
	if file.Version != 1 {
		return nil, fmt.Errorf("%s: unsupported version %d", label, file.Version)
	}
	if !validRevision(file.Revision) || file.Revision != head {
		return nil, fmt.Errorf("%s: revision must match review head %s", label, shortRevision(head))
	}
	return validateStaticExplanations(file.Explanations, changed, label)
}

func validateStaticExplanations(explanations []staticExplanation, changed map[string]bool, label string) ([]staticExplanation, error) {
	if len(explanations) > explanationMaxItems {
		return nil, fmt.Errorf("%s: at most %d explanations", label, explanationMaxItems)
	}
	seen := map[string]bool{}
	for i := range explanations {
		explanation := &explanations[i]
		explanation.ID = strings.TrimSpace(explanation.ID)
		explanation.Path = filepath.ToSlash(strings.TrimSpace(explanation.Path))
		explanation.Title = strings.TrimSpace(explanation.Title)
		explanation.Summary = strings.TrimSpace(explanation.Summary)
		if explanation.ID == "" || seen[explanation.ID] {
			return nil, fmt.Errorf("%s explanation %d: id must be non-empty and unique", label, i+1)
		}
		seen[explanation.ID] = true
		if !safeExplanationPath(explanation.Path) || !changed[explanation.Path] {
			return nil, fmt.Errorf("%s explanation %q: path must name a changed file", label, explanation.ID)
		}
		if explanation.LineStart < 1 || explanation.LineEnd < explanation.LineStart || explanation.LineEnd-explanation.LineStart > 200 {
			return nil, fmt.Errorf("%s explanation %q: invalid line range", label, explanation.ID)
		}
		if explanation.Title == "" || len(explanation.Title) > explanationMaxTitle {
			return nil, fmt.Errorf("%s explanation %q: title must be between 1 and %d bytes", label, explanation.ID, explanationMaxTitle)
		}
		if explanation.Summary == "" || len(explanation.Summary) > explanationMaxText {
			return nil, fmt.Errorf("%s explanation %q: summary must be between 1 and %d bytes", label, explanation.ID, explanationMaxText)
		}
	}
	sort.Slice(explanations, func(i, j int) bool {
		left, right := explanations[i], explanations[j]
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		if left.LineStart != right.LineStart {
			return left.LineStart < right.LineStart
		}
		return left.ID < right.ID
	})
	if explanations == nil {
		return []staticExplanation{}, nil
	}
	return explanations, nil
}

func safeExplanationPath(path string) bool {
	if path == "" || filepath.IsAbs(path) || strings.Contains(path, `\`) {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	return clean == path && clean != "." && !strings.HasPrefix(clean, "../")
}
