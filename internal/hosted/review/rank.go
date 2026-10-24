package review

import (
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

const DefaultMaxInlineComments = 15

// RankAndCapFindings removes findings that describe the same overlapping
// location, keeps the strongest representative, and returns a deterministic
// severity-first ordering suitable for GitHub delivery.
func RankAndCapFindings(findings []Finding, maxInlineComments int) []Finding {
	if maxInlineComments <= 0 {
		maxInlineComments = DefaultMaxInlineComments
	}
	ranked := append([]Finding(nil), findings...)
	sort.SliceStable(ranked, func(i, j int) bool { return stronger(ranked[i], ranked[j]) })

	kept := make([]Finding, 0, min(len(ranked), maxInlineComments))
	for _, candidate := range ranked {
		duplicate := false
		for _, existing := range kept {
			if sameFinding(existing, candidate) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			kept = append(kept, candidate)
			if len(kept) == maxInlineComments {
				break
			}
		}
	}
	return kept
}

func stronger(a, b Finding) bool {
	rank := map[string]int{"P0": 0, "P1": 1, "P2": 2}
	if rank[a.Severity] != rank[b.Severity] {
		return rank[a.Severity] < rank[b.Severity]
	}
	if a.Confidence != b.Confidence {
		return a.Confidence > b.Confidence
	}
	ap, bp := filepath.ToSlash(filepath.Clean(a.Path)), filepath.ToSlash(filepath.Clean(b.Path))
	if ap != bp {
		return ap < bp
	}
	if a.StartLine != b.StartLine {
		return a.StartLine < b.StartLine
	}
	if a.EndLine != b.EndLine {
		return a.EndLine < b.EndLine
	}
	return a.Title < b.Title
}

func sameFinding(a, b Finding) bool {
	if filepath.ToSlash(filepath.Clean(a.Path)) != filepath.ToSlash(filepath.Clean(b.Path)) {
		return false
	}
	if a.EndLine < b.StartLine || b.EndLine < a.StartLine {
		return false
	}
	return titleSimilarity(a.Title, b.Title) >= 0.6
}

func titleSimilarity(a, b string) float64 {
	left, right := titleWords(a), titleWords(b)
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	intersection := 0
	union := make(map[string]bool, len(left)+len(right))
	for word := range left {
		union[word] = true
		if right[word] {
			intersection++
		}
	}
	for word := range right {
		union[word] = true
	}
	return float64(intersection) / float64(len(union))
}

func titleWords(value string) map[string]bool {
	words := strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	result := make(map[string]bool, len(words))
	for _, word := range words {
		result[word] = true
	}
	return result
}
