package graph

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

type ToolSymbol struct {
	ID        int64
	Path      string
	Kind      string
	Name      string
	Signature string
}

type ToolEdge struct {
	Source ToolSymbol
	Target ToolSymbol
	Kind   string
}

type Direction string

const (
	Forward         Direction = "forward"
	Reverse         Direction = "reverse"
	MaxGraphDepth             = 2
	MaxGraphResults           = 250
)

type GraphReader interface {
	Neighbors(context.Context, int64, int64, []int64, Direction, []string) ([]ToolEdge, error)
}

type ToolQuery struct {
	InstallationID int64
	RepositoryID   int64
	SymbolID       int64
	Depth          int
	Limit          int
}

type Tools struct{ Reader GraphReader }

func (t Tools) Callers(ctx context.Context, query ToolQuery) ([]ToolSymbol, error) {
	return t.traverse(ctx, query, Reverse, []string{"calls"})
}

func (t Tools) Callees(ctx context.Context, query ToolQuery) ([]ToolSymbol, error) {
	return t.traverse(ctx, query, Forward, []string{"calls"})
}

func (t Tools) TestsFor(ctx context.Context, query ToolQuery) ([]ToolSymbol, error) {
	query.Depth = 1
	return t.traverse(ctx, query, Reverse, []string{"tests"})
}

func (t Tools) Implementers(ctx context.Context, query ToolQuery) ([]ToolSymbol, error) {
	query.Depth = 1
	return t.traverse(ctx, query, Reverse, []string{"implements"})
}

func (t Tools) Siblings(ctx context.Context, query ToolQuery) ([]ToolSymbol, error) {
	if err := validateToolQuery(t.Reader, &query); err != nil {
		return nil, err
	}
	parents, err := t.Reader.Neighbors(ctx, query.InstallationID, query.RepositoryID, []int64{query.SymbolID}, Reverse, []string{"contains"})
	if err != nil {
		return nil, err
	}
	parentIDs := make([]int64, 0, len(parents))
	for _, edge := range parents {
		parentIDs = append(parentIDs, edge.Source.ID)
	}
	if len(parentIDs) == 0 {
		return nil, nil
	}
	children, err := t.Reader.Neighbors(ctx, query.InstallationID, query.RepositoryID, parentIDs, Forward, []string{"contains"})
	if err != nil {
		return nil, err
	}
	result := map[int64]ToolSymbol{}
	for _, edge := range children {
		if edge.Target.ID != query.SymbolID && len(result) < query.Limit {
			result[edge.Target.ID] = edge.Target
		}
	}
	return sortedToolSymbols(result), nil
}

func (t Tools) traverse(ctx context.Context, query ToolQuery, direction Direction, kinds []string) ([]ToolSymbol, error) {
	if err := validateToolQuery(t.Reader, &query); err != nil {
		return nil, err
	}
	frontier := []int64{query.SymbolID}
	seen := map[int64]bool{query.SymbolID: true}
	result := map[int64]ToolSymbol{}
	for depth := 0; depth < query.Depth && len(frontier) > 0 && len(result) < query.Limit; depth++ {
		edges, err := t.Reader.Neighbors(ctx, query.InstallationID, query.RepositoryID, frontier, direction, kinds)
		if err != nil {
			return nil, fmt.Errorf("query graph neighbors: %w", err)
		}
		next := []int64{}
		for _, edge := range edges {
			symbol := edge.Target
			if direction == Reverse {
				symbol = edge.Source
			}
			if symbol.ID <= 0 || seen[symbol.ID] {
				continue
			}
			seen[symbol.ID] = true
			result[symbol.ID] = symbol
			next = append(next, symbol.ID)
			if len(result) >= query.Limit {
				break
			}
		}
		frontier = next
	}
	return sortedToolSymbols(result), nil
}

func validateToolQuery(reader GraphReader, query *ToolQuery) error {
	if reader == nil || query.InstallationID <= 0 || query.RepositoryID <= 0 || query.SymbolID <= 0 {
		return errors.New("graph reader and tenant-scoped symbol are required")
	}
	if query.Depth == 0 {
		query.Depth = 1
	}
	if query.Depth < 1 || query.Depth > MaxGraphDepth {
		return fmt.Errorf("graph depth must be between 1 and %d", MaxGraphDepth)
	}
	if query.Limit == 0 {
		query.Limit = 100
	}
	if query.Limit < 1 || query.Limit > MaxGraphResults {
		return fmt.Errorf("graph result limit must be between 1 and %d", MaxGraphResults)
	}
	return nil
}

func sortedToolSymbols(symbols map[int64]ToolSymbol) []ToolSymbol {
	result := make([]ToolSymbol, 0, len(symbols))
	for _, symbol := range symbols {
		result = append(result, symbol)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}
