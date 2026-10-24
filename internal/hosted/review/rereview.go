package review

import (
	"errors"
	"path/filepath"
	"sort"
	"strings"
)

type PriorFinding struct {
	Fingerprint string
	Path        string
	StartLine   int
	EndLine     int
	Status      string
	ThreadID    string
}

type FingerprintedFinding struct {
	Fingerprint string
	Finding     Finding
}

type ReReviewPlan struct {
	BaseSHA               string
	HeadSHA               string
	Findings              []FingerprintedFinding
	ResolveThreadIDs      []string
	DismissedFingerprints []string
}

// PlanReReview uses the last reviewed SHA as the next base, suppresses
// previously dismissed fingerprints, and resolves an old thread only when its
// range changed and the finding did not recur.
func PlanReReview(lastReviewedSHA, headSHA string, changed []DiffRange, prior []PriorFinding, proposed []FingerprintedFinding) (ReReviewPlan, error) {
	if !fullSHA(lastReviewedSHA) || !fullSHA(headSHA) || lastReviewedSHA == headSHA {
		return ReReviewPlan{}, errors.New("distinct full review SHAs are required")
	}
	dismissed := map[string]bool{}
	for _, old := range prior {
		if old.Status == "dismissed" && old.Fingerprint != "" {
			dismissed[old.Fingerprint] = true
		}
	}
	current := map[string]bool{}
	kept := make([]FingerprintedFinding, 0, len(proposed))
	for _, candidate := range proposed {
		if candidate.Fingerprint == "" {
			return ReReviewPlan{}, errors.New("proposed finding fingerprint is required")
		}
		current[candidate.Fingerprint] = true
		if !dismissed[candidate.Fingerprint] {
			kept = append(kept, candidate)
		}
	}
	resolve := map[string]bool{}
	for _, old := range prior {
		if old.Status != "open" || old.ThreadID == "" || current[old.Fingerprint] {
			continue
		}
		if rangeChanged(old, changed) {
			resolve[old.ThreadID] = true
		}
	}
	plan := ReReviewPlan{BaseSHA: lastReviewedSHA, HeadSHA: headSHA, Findings: kept}
	for id := range resolve {
		plan.ResolveThreadIDs = append(plan.ResolveThreadIDs, id)
	}
	for fingerprint := range dismissed {
		plan.DismissedFingerprints = append(plan.DismissedFingerprints, fingerprint)
	}
	sort.Strings(plan.ResolveThreadIDs)
	sort.Strings(plan.DismissedFingerprints)
	return plan, nil
}

func rangeChanged(old PriorFinding, changed []DiffRange) bool {
	path := filepath.ToSlash(filepath.Clean(old.Path))
	for _, span := range changed {
		if filepath.ToSlash(filepath.Clean(span.Path)) == path && old.StartLine <= span.EndLine && span.StartLine <= old.EndLine {
			return true
		}
	}
	return false
}

func fullSHA(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, r := range strings.ToLower(value) {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}
