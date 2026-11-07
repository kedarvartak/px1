package review

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DroppedFinding explains why one model-proposed finding was discarded.
type DroppedFinding struct {
	Index  int
	Title  string
	Reason string
}

// DecodeFindingsLenient keeps every finding that passes Validate and reports
// the rest, instead of rejecting a whole array because one entry is malformed.
// Model output is the one place that is expected to be imperfect, so unknown
// fields are ignored and casing of severity and type is normalized; everything
// that reaches a report still passes the strict Validate. Findings past max
// are dropped, not truncated silently.
func DecodeFindingsLenient(raw json.RawMessage, max int) ([]Finding, []DroppedFinding, error) {
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, nil, fmt.Errorf("findings must be a JSON array: %w", err)
	}
	if max <= 0 {
		max = 100
	}
	kept := []Finding{}
	dropped := []DroppedFinding{}
	for i, entry := range entries {
		var finding Finding
		if err := json.Unmarshal(entry, &finding); err != nil {
			dropped = append(dropped, DroppedFinding{Index: i, Reason: "not a finding object"})
			continue
		}
		finding.Severity = strings.ToUpper(strings.TrimSpace(finding.Severity))
		finding.Type = strings.ToLower(strings.TrimSpace(finding.Type))
		finding.Path = strings.TrimPrefix(strings.TrimSpace(finding.Path), "./")
		if err := finding.Validate(); err != nil {
			dropped = append(dropped, DroppedFinding{Index: i, Title: clipTitle(finding.Title), Reason: err.Error()})
			continue
		}
		if len(kept) >= max {
			dropped = append(dropped, DroppedFinding{Index: i, Title: clipTitle(finding.Title), Reason: fmt.Sprintf("more than %d findings from one source", max)})
			continue
		}
		kept = append(kept, finding)
	}
	return kept, dropped, nil
}

func clipTitle(title string) string {
	if len(title) > 80 {
		return strings.ToValidUTF8(title[:80], "") + "…"
	}
	return title
}
