package learning

import (
	"errors"
	"path"
	"sort"
	"time"
)

const (
	DefaultSuppressionThreshold = 3
	DefaultSuppressionWindow    = 30 * 24 * time.Hour
)

type SuppressionFinding struct {
	Severity string
	Category string
	Path     string
}

type NoiseEvent struct {
	ID         int64
	Category   string
	Path       string
	Kind       string
	OccurredAt time.Time
}

type SuppressionConfig struct {
	Threshold int
	Window    time.Duration
	PathGlob  string
}

type SuppressionDecision struct {
	Suppress    bool
	Reason      string
	EvidenceIDs []int64
}

// DecideSuppression applies only to lower-risk categories. P0, security, and
// logic findings always remain visible regardless of feedback history.
func DecideSuppression(finding SuppressionFinding, events []NoiseEvent, config SuppressionConfig, now time.Time) (SuppressionDecision, error) {
	if finding.Path == "" || !map[string]bool{"P0": true, "P1": true, "P2": true}[finding.Severity] || finding.Category == "" || now.IsZero() {
		return SuppressionDecision{}, errors.New("finding path, severity, category, and current time are required")
	}
	if finding.Severity == "P0" || finding.Category == "security" || finding.Category == "logic" {
		return SuppressionDecision{Reason: "protected finding categories are never auto-suppressed"}, nil
	}
	if config.Threshold == 0 {
		config.Threshold = DefaultSuppressionThreshold
	}
	if config.Threshold < DefaultSuppressionThreshold {
		return SuppressionDecision{}, errors.New("suppression threshold cannot be lower than three")
	}
	if config.Window == 0 {
		config.Window = DefaultSuppressionWindow
	}
	if config.Window <= 0 || config.Window > 365*24*time.Hour {
		return SuppressionDecision{}, errors.New("suppression window is invalid")
	}
	if config.PathGlob == "" {
		config.PathGlob = "**"
	}
	matched, err := path.Match(config.PathGlob, finding.Path)
	if err != nil {
		return SuppressionDecision{}, errors.New("suppression path glob is invalid")
	}
	if !matched && config.PathGlob != "**" {
		return SuppressionDecision{Reason: "finding is outside the configured path scope"}, nil
	}
	cutoff := now.Add(-config.Window)
	seen := map[int64]bool{}
	decision := SuppressionDecision{}
	for _, event := range events {
		if event.ID <= 0 || seen[event.ID] || event.Category != finding.Category || event.OccurredAt.Before(cutoff) || event.OccurredAt.After(now) || (event.Kind != "ignored" && event.Kind != "thumbs_down") {
			continue
		}
		if config.PathGlob != "**" {
			inScope, _ := path.Match(config.PathGlob, event.Path)
			if !inScope {
				continue
			}
		}
		seen[event.ID] = true
		decision.EvidenceIDs = append(decision.EvidenceIDs, event.ID)
	}
	sort.Slice(decision.EvidenceIDs, func(i, j int) bool { return decision.EvidenceIDs[i] < decision.EvidenceIDs[j] })
	if len(decision.EvidenceIDs) >= config.Threshold {
		decision.Suppress = true
		decision.Reason = "eligible category repeatedly ignored or downvoted in scope"
	} else {
		decision.Reason = "feedback threshold not reached"
	}
	return decision, nil
}
