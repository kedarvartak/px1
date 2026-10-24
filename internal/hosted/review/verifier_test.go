package review

import (
	"context"
	"px1/internal/hosted/llm"
	"testing"
)

type verifierStub struct{ response string }

func (s verifierStub) Complete(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{Text: s.response}, nil
}
func TestVerifierDropsUnsupportedLowConfidenceAndMissingEvidence(t *testing.T) {
	v := FindingVerifier{Client: verifierStub{`[{"id":"a","supported":true,"confidence":0.8,"reason":"evidence proves it"},{"id":"b","supported":true,"confidence":0.4,"reason":"ambiguous"}]`}, Provider: "openai", Model: "verifier", PromptVersion: "v1"}
	result, err := v.Verify(context.Background(), []VerificationCandidate{{ID: "a", Path: "a.go", StartLine: 1, EndLine: 1, Evidence: "return nil", Context: "line 1"}, {ID: "b", Path: "b.go", StartLine: 2, EndLine: 2, Evidence: "value", Context: "line 2"}, {ID: "c", Path: "c.go"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Kept) != 1 || result.Kept[0].ID != "a" || len(result.Dropped) != 2 {
		t.Fatalf("result=%+v", result)
	}
}
func TestVerifierRejectsOmittedDecision(t *testing.T) {
	v := FindingVerifier{Client: verifierStub{`[]`}, Provider: "openai", Model: "v", PromptVersion: "v1"}
	if _, err := v.Verify(context.Background(), []VerificationCandidate{{ID: "a", StartLine: 1, EndLine: 1, Evidence: "x"}}); err == nil {
		t.Fatal("expected omitted decision error")
	}
}
