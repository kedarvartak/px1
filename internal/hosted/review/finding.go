package review

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

type Finding struct {
	Path       string  `json:"path"`
	StartLine  int     `json:"start_line"`
	EndLine    int     `json:"end_line"`
	Severity   string  `json:"severity"`
	Type       string  `json:"type"`
	Title      string  `json:"title"`
	Body       string  `json:"body"`
	Evidence   string  `json:"evidence"`
	Suggestion string  `json:"suggestion,omitempty"`
	Confidence float64 `json:"confidence"`
	RuleID     string  `json:"rule_id,omitempty"`
}

func DecodeFindings(data []byte, maxFindings int) ([]Finding, error) {
	if maxFindings <= 0 {
		maxFindings = 100
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var findings []Finding
	if err := decoder.Decode(&findings); err != nil {
		return nil, fmt.Errorf("decode findings: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return nil, err
	}
	if len(findings) > maxFindings {
		return nil, fmt.Errorf("finding count %d exceeds limit %d", len(findings), maxFindings)
	}
	for i, finding := range findings {
		if err := finding.Validate(); err != nil {
			return nil, fmt.Errorf("finding %d: %w", i, err)
		}
	}
	return findings, nil
}

func (f Finding) Validate() error {
	clean := filepath.ToSlash(filepath.Clean(f.Path))
	if f.Path == "" || filepath.IsAbs(f.Path) || clean == ".." || strings.HasPrefix(clean, "../") {
		return errors.New("path must be repository-relative")
	}
	if f.StartLine <= 0 || f.EndLine < f.StartLine {
		return errors.New("line range is invalid")
	}
	if f.Severity != "P0" && f.Severity != "P1" && f.Severity != "P2" {
		return errors.New("severity must be P0, P1, or P2")
	}
	if !map[string]bool{"logic": true, "syntax": true, "style": true, "security": true}[f.Type] {
		return errors.New("unsupported finding type")
	}
	if strings.TrimSpace(f.Title) == "" || strings.TrimSpace(f.Body) == "" || strings.TrimSpace(f.Evidence) == "" {
		return errors.New("title, body, and evidence are required")
	}
	if len(f.Title) > 200 || len(f.Body) > 16_000 || len(f.Evidence) > 8_000 || len(f.Suggestion) > 16_000 {
		return errors.New("finding text exceeds its limit")
	}
	if f.Confidence < 0 || f.Confidence > 1 {
		return errors.New("confidence must be between 0 and 1")
	}
	return nil
}
func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("findings JSON contains trailing data")
	}
	return nil
}
