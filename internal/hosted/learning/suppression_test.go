package learning

import (
	"testing"
	"time"
)

func TestDecideSuppressionRequiresThreeRecentSignals(t *testing.T) {
	now := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	events := []NoiseEvent{{ID: 1, Category: "style", Path: "src/a.go", Kind: "ignored", OccurredAt: now.Add(-time.Hour)}, {ID: 2, Category: "style", Path: "src/b.go", Kind: "thumbs_down", OccurredAt: now.Add(-2 * time.Hour)}, {ID: 3, Category: "style", Path: "src/c.go", Kind: "ignored", OccurredAt: now.Add(-3 * time.Hour)}}
	decision, err := DecideSuppression(SuppressionFinding{Severity: "P2", Category: "style", Path: "src/new.go"}, events, SuppressionConfig{PathGlob: "src/*"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Suppress || len(decision.EvidenceIDs) != 3 {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestDecideSuppressionNeverHidesProtectedFinding(t *testing.T) {
	now := time.Now()
	events := []NoiseEvent{{ID: 1, Category: "security", Path: "a.go", Kind: "thumbs_down", OccurredAt: now}, {ID: 2, Category: "security", Path: "a.go", Kind: "thumbs_down", OccurredAt: now}, {ID: 3, Category: "security", Path: "a.go", Kind: "thumbs_down", OccurredAt: now}}
	decision, err := DecideSuppression(SuppressionFinding{Severity: "P2", Category: "security", Path: "a.go"}, events, SuppressionConfig{}, now)
	if err != nil || decision.Suppress {
		t.Fatalf("protected decision = %+v, %v", decision, err)
	}
}
