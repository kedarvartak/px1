package review

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"px1/internal/hosted/llm"
)

type VerificationCandidate struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Title     string `json:"title"`
	Evidence  string `json:"evidence"`
	Context   string `json:"context"`
}
type VerificationDecision struct {
	ID         string  `json:"id"`
	Supported  bool    `json:"supported"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}
type VerificationResult struct {
	Kept      []VerificationCandidate
	Dropped   map[string]string
	Decisions []VerificationDecision
}
type VerificationClient interface {
	Complete(context.Context, llm.Request) (llm.Response, error)
}
type FindingVerifier struct {
	Client                         VerificationClient
	Provider, Model, PromptVersion string
	MinConfidence                  float64
	MaxOutputTokens                int
}

func (v FindingVerifier) Verify(ctx context.Context, candidates []VerificationCandidate) (VerificationResult, error) {
	result := VerificationResult{Dropped: map[string]string{}}
	eligible := make([]VerificationCandidate, 0, len(candidates))
	seen := map[string]bool{}
	eligibleIDs := map[string]bool{}
	for _, candidate := range candidates {
		if candidate.ID == "" || seen[candidate.ID] {
			return result, errors.New("verification candidates require unique IDs")
		}
		seen[candidate.ID] = true
		if candidate.StartLine <= 0 || candidate.EndLine < candidate.StartLine || strings.TrimSpace(candidate.Evidence) == "" {
			result.Dropped[candidate.ID] = "missing line evidence"
			continue
		}
		eligible = append(eligible, candidate)
		eligibleIDs[candidate.ID] = true
	}
	if len(eligible) == 0 {
		return result, nil
	}
	if v.Client == nil || v.Provider == "" || v.Model == "" || v.PromptVersion == "" {
		return result, errors.New("verifier client, provider, model, and prompt version are required")
	}
	payload, _ := json.Marshal(eligible)
	maxOutput := v.MaxOutputTokens
	if maxOutput <= 0 {
		maxOutput = 2000
	}
	response, err := v.Client.Complete(ctx, llm.Request{Provider: v.Provider, Model: v.Model, System: "Verify each proposed code-review finding only against its supplied line evidence and context. Return strict JSON decisions and do not add findings.", Prompt: "Prompt version: " + v.PromptVersion + "\nCandidates:\n" + string(payload), MaxOutputTokens: maxOutput, Temperature: 0})
	if err != nil {
		return result, fmt.Errorf("verify findings: %w", err)
	}
	decisions, err := decodeDecisions([]byte(response.Text))
	if err != nil {
		return result, err
	}
	result.Decisions = decisions
	byID := map[string]VerificationDecision{}
	for _, decision := range decisions {
		if !eligibleIDs[decision.ID] || byID[decision.ID].ID != "" {
			return result, fmt.Errorf("unexpected or duplicate verification ID %q", decision.ID)
		}
		byID[decision.ID] = decision
	}
	threshold := v.MinConfidence
	if threshold <= 0 {
		threshold = .6
	}
	for _, candidate := range eligible {
		decision, ok := byID[candidate.ID]
		if !ok {
			return result, fmt.Errorf("verifier omitted candidate %q", candidate.ID)
		}
		if !decision.Supported || decision.Confidence < threshold {
			result.Dropped[candidate.ID] = decision.Reason
			continue
		}
		result.Kept = append(result.Kept, candidate)
	}
	return result, nil
}
func decodeDecisions(data []byte) ([]VerificationDecision, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var decisions []VerificationDecision
	if err := decoder.Decode(&decisions); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, errors.New("verifier response contains trailing data")
	}
	for _, d := range decisions {
		if d.ID == "" || d.Confidence < 0 || d.Confidence > 1 || strings.TrimSpace(d.Reason) == "" {
			return nil, errors.New("verifier decision is invalid")
		}
	}
	return decisions, nil
}
