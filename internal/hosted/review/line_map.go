package review

import (
	"errors"
	"path/filepath"
	"strings"
)

type DiffRange struct {
	Path      string
	StartLine int
	EndLine   int
}

type SummaryFinding struct {
	Finding Finding
	Reason  string
}

// MapFindingsToDiff separates findings that GitHub can anchor on the new side
// of the current diff from findings that must remain visible in the summary.
// A multiline finding is inline only when its complete range is in one changed
// range; this avoids GitHub rejecting a review after analysis has completed.
func MapFindingsToDiff(findings []Finding, changed []DiffRange) ([]Finding, []SummaryFinding, error) {
	byPath := make(map[string][]DiffRange)
	for _, span := range changed {
		path := filepath.ToSlash(filepath.Clean(span.Path))
		if span.Path == "" || filepath.IsAbs(span.Path) || path == ".." || strings.HasPrefix(path, "../") || span.StartLine <= 0 || span.EndLine < span.StartLine {
			return nil, nil, errors.New("diff range is invalid")
		}
		span.Path = path
		byPath[path] = append(byPath[path], span)
	}

	inline := make([]Finding, 0, len(findings))
	summary := make([]SummaryFinding, 0)
	for _, finding := range findings {
		path := filepath.ToSlash(filepath.Clean(finding.Path))
		ranges := byPath[path]
		anchored := false
		for _, span := range ranges {
			if finding.StartLine >= span.StartLine && finding.EndLine <= span.EndLine {
				anchored = true
				break
			}
		}
		if anchored {
			inline = append(inline, finding)
			continue
		}
		reason := "line range is not part of the current diff"
		if len(ranges) == 0 {
			reason = "file has no commentable lines in the current diff"
		}
		summary = append(summary, SummaryFinding{Finding: finding, Reason: reason})
	}
	return inline, summary, nil
}
