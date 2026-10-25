package review

import (
	"errors"
	"fmt"
	"strings"
)

type ConfidenceInput struct {
	UnresolvedP0       int
	UnresolvedP1       int
	ChangedLines       int
	ComplexityIncrease int
	ChangedSymbols     int
	TestedSymbols      int
	HighRiskPaths      int
	PatternFit         float64
}

type ConfidenceReason struct {
	Input  string `json:"input"`
	Impact int    `json:"impact"`
	Detail string `json:"detail"`
}

type ConfidenceScore struct {
	Value   int                `json:"value"`
	Reasons []ConfidenceReason `json:"reasons"`
}

func (s ConfidenceScore) Validate() error {
	if s.Value < 0 || s.Value > 5 || len(s.Reasons) == 0 || len(s.Reasons) > 8 {
		return errors.New("confidence score or reason count is invalid")
	}
	for _, reason := range s.Reasons {
		if reason.Input == "" || len(reason.Input) > 80 || reason.Impact < -5 || reason.Impact > 0 || strings.TrimSpace(reason.Detail) == "" || len(reason.Detail) > 400 {
			return errors.New("confidence reason is invalid")
		}
	}
	return nil
}

// ScoreConfidence implements the published zero-to-five rubric. It describes
// how much supporting evidence the review has; it is never an approval or a
// probability that the change is correct.
func ScoreConfidence(input ConfidenceInput) (ConfidenceScore, error) {
	if input.UnresolvedP0 < 0 || input.UnresolvedP1 < 0 || input.ChangedLines < 0 || input.ComplexityIncrease < 0 || input.ChangedSymbols < 0 || input.TestedSymbols < 0 || input.TestedSymbols > input.ChangedSymbols || input.HighRiskPaths < 0 || input.PatternFit < 0 || input.PatternFit > 1 {
		return ConfidenceScore{}, errors.New("confidence inputs are outside their valid ranges")
	}
	score := 5
	reasons := []ConfidenceReason{}
	apply := func(name string, impact int, detail string) {
		if impact == 0 {
			return
		}
		score += impact
		reasons = append(reasons, ConfidenceReason{Input: name, Impact: impact, Detail: detail})
	}
	if input.UnresolvedP0 > 0 {
		apply("unresolved_p0", -min(4, input.UnresolvedP0*2), fmt.Sprintf("%d unresolved P0 findings", input.UnresolvedP0))
	}
	if input.UnresolvedP1 > 0 {
		apply("unresolved_p1", -min(3, input.UnresolvedP1), fmt.Sprintf("%d unresolved P1 findings", input.UnresolvedP1))
	}
	switch {
	case input.ChangedLines > 3000:
		apply("changed_lines", -2, "more than 3,000 changed lines")
	case input.ChangedLines > 1000:
		apply("changed_lines", -1, "more than 1,000 changed lines")
	}
	if input.ComplexityIncrease > 15 {
		apply("complexity", -1, "complexity increased by more than 15")
	}
	if input.ChangedSymbols > 0 {
		coverage := float64(input.TestedSymbols) / float64(input.ChangedSymbols)
		if coverage < .5 {
			apply("test_coverage", -1, fmt.Sprintf("%.0f%% of changed symbols have linked tests", coverage*100))
		}
	}
	if input.HighRiskPaths > 0 {
		apply("path_risk", -1, fmt.Sprintf("%d high-risk paths changed", input.HighRiskPaths))
	}
	if input.PatternFit < .5 {
		apply("pattern_fit", -1, fmt.Sprintf("repository pattern fit is %.0f%%", input.PatternFit*100))
	}
	if score < 0 {
		score = 0
	}
	if len(reasons) == 0 {
		reasons = append(reasons, ConfidenceReason{Input: "supporting_evidence", Impact: 0, Detail: "no rubric deductions applied"})
	}
	return ConfidenceScore{Value: score, Reasons: reasons}, nil
}
