package review

import (
	"context"
	"strings"
	"testing"
)

type words struct{}

func (words) Count(s string) int { return len(strings.Fields(s)) }

type sourceStub struct{}

func (sourceStub) Similar(context.Context, int64, []ContextItem, int) ([]ContextItem, error) {
	return []ContextItem{{Kind: "similar", Content: "one two"}, {Kind: "similar", Content: "three four"}}, nil
}
func (sourceStub) Relationships(context.Context, int64, []string) ([]ContextItem, error) {
	return []ContextItem{{Kind: "importer", Content: "five"}}, nil
}
func (sourceStub) Tests(context.Context, int64, []string) ([]ContextItem, error) {
	return []ContextItem{{Kind: "test", Content: "six"}}, nil
}
func (sourceStub) Summaries(context.Context, int64, []string) ([]ContextItem, error) {
	return []ContextItem{{Kind: "summary", Content: "seven"}}, nil
}

func TestContextAssemblyPrioritizesDiffAndHonorsBudget(t *testing.T) {
	a := ContextAssembler{Source: sourceStub{}, Counter: words{}, MaxTokens: 5, TopK: 1}
	result, err := a.Assemble(context.Background(), ContextRequest{RepositoryID: 1, DiffHunks: []ContextItem{{Kind: "diff", Content: "diff first"}}, Functions: []ContextItem{{Kind: "function", Content: "function body"}}, ChangedPaths: []string{"a.go"}, RulesMarkdown: "late rules"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Truncated || result.TokenCount > 5 || len(result.Items) < 2 || result.Items[0].Kind != "diff" {
		t.Fatalf("result = %+v", result)
	}
}
