package review

import (
	"strings"
	"testing"
)

func TestScoreConfidenceIsDeterministicAndExplained(t *testing.T) {
	score, err := ScoreConfidence(ConfidenceInput{UnresolvedP1: 1, ChangedLines: 1200, ChangedSymbols: 4, TestedSymbols: 1, HighRiskPaths: 1, PatternFit: .4})
	if err != nil || score.Value != 0 || len(score.Reasons) != 5 {
		t.Fatalf("score = %#v, %v", score, err)
	}
}

func TestScoreConfidenceIsNotReducedForWellSupportedReview(t *testing.T) {
	score, err := ScoreConfidence(ConfidenceInput{ChangedLines: 80, ChangedSymbols: 4, TestedSymbols: 4, PatternFit: .9})
	if err != nil || score.Value != 5 || len(score.Reasons) != 1 {
		t.Fatalf("score = %#v, %v", score, err)
	}
}

func TestRenderSummaryPublishesConfidenceReasons(t *testing.T) {
	score, _ := ScoreConfidence(ConfidenceInput{ChangedLines: 1200, PatternFit: 1})
	body, err := RenderSummary(SummaryInput{HeadSHA: strings.Repeat("a", 40), ChangedFiles: 1, ReportURL: "https://px1.example/r/a", Confidence: &score})
	if err != nil || !strings.Contains(body, "Review confidence: 4/5") || !strings.Contains(body, "not approval") {
		t.Fatalf("summary = %q, %v", body, err)
	}
}
