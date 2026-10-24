package review

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type ContextItem struct {
	Kind, Path, Symbol, Content string
	Score                       float64
}

type ContextRequest struct {
	RepositoryID  int64
	DiffHunks     []ContextItem
	Functions     []ContextItem
	ChangedPaths  []string
	RulesMarkdown string
	MatchedRules  []string
}

type ContextSource interface {
	Similar(context.Context, int64, []ContextItem, int) ([]ContextItem, error)
	Relationships(context.Context, int64, []string) ([]ContextItem, error)
	Tests(context.Context, int64, []string) ([]ContextItem, error)
	Summaries(context.Context, int64, []string) ([]ContextItem, error)
}

type ContextBundle struct {
	Items      []ContextItem
	TokenCount int
	Truncated  bool
	Limits     []string
}

type ContextAssembler struct {
	Source    ContextSource
	Counter   interface{ Count(string) int }
	MaxTokens int
	TopK      int
}

func (a ContextAssembler) Assemble(ctx context.Context, request ContextRequest) (ContextBundle, error) {
	if a.Source == nil || a.Counter == nil || request.RepositoryID <= 0 {
		return ContextBundle{}, errors.New("context source, token counter, and repository are required")
	}
	topK := a.TopK
	if topK <= 0 {
		topK = 8
	}
	budget := a.MaxTokens
	if budget <= 0 {
		budget = 60_000
	}
	similar, err := a.Source.Similar(ctx, request.RepositoryID, request.DiffHunks, topK)
	if err != nil {
		return ContextBundle{}, fmt.Errorf("retrieve similar chunks: %w", err)
	}
	if len(similar) > topK {
		similar = similar[:topK]
	}
	related, err := a.Source.Relationships(ctx, request.RepositoryID, request.ChangedPaths)
	if err != nil {
		return ContextBundle{}, err
	}
	tests, err := a.Source.Tests(ctx, request.RepositoryID, request.ChangedPaths)
	if err != nil {
		return ContextBundle{}, err
	}
	summaries, err := a.Source.Summaries(ctx, request.RepositoryID, request.ChangedPaths)
	if err != nil {
		return ContextBundle{}, err
	}
	items := append([]ContextItem{}, request.DiffHunks...)
	items = append(items, request.Functions...)
	items = append(items, similar...)
	items = append(items, related...)
	items = append(items, tests...)
	items = append(items, summaries...)
	if request.RulesMarkdown != "" {
		items = append(items, ContextItem{Kind: "rules.md", Content: request.RulesMarkdown})
	}
	if len(request.MatchedRules) > 0 {
		items = append(items, ContextItem{Kind: "matched_rules", Content: strings.Join(request.MatchedRules, "\n")})
	}
	result := ContextBundle{Limits: []string{fmt.Sprintf("top %d similar chunks", topK), fmt.Sprintf("%d token context budget", budget)}}
	for _, item := range items {
		tokens := a.Counter.Count(item.Content)
		if tokens < 0 {
			return ContextBundle{}, errors.New("token counter returned a negative count")
		}
		if result.TokenCount+tokens > budget {
			result.Truncated = true
			break
		}
		result.Items = append(result.Items, item)
		result.TokenCount += tokens
	}
	return result, nil
}
