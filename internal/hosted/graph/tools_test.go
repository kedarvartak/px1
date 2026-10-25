package graph

import (
	"context"
	"testing"
)

type memoryGraph []ToolEdge

func (g memoryGraph) Neighbors(_ context.Context, installationID, repositoryID int64, ids []int64, direction Direction, kinds []string) ([]ToolEdge, error) {
	if installationID != 7 || repositoryID != 9 {
		panic("query escaped tenant scope")
	}
	idSet, kindSet := map[int64]bool{}, map[string]bool{}
	for _, id := range ids {
		idSet[id] = true
	}
	for _, kind := range kinds {
		kindSet[kind] = true
	}
	var result []ToolEdge
	for _, edge := range g {
		if !kindSet[edge.Kind] {
			continue
		}
		if (direction == Forward && idSet[edge.Source.ID]) || (direction == Reverse && idSet[edge.Target.ID]) {
			result = append(result, edge)
		}
	}
	return result, nil
}

func TestGraphToolsTraverseAtMostTwoHops(t *testing.T) {
	a, b, c, d := ToolSymbol{ID: 1, Name: "a"}, ToolSymbol{ID: 2, Name: "b"}, ToolSymbol{ID: 3, Name: "c"}, ToolSymbol{ID: 4, Name: "d"}
	tools := Tools{Reader: memoryGraph{{Source: a, Target: b, Kind: "calls"}, {Source: b, Target: c, Kind: "calls"}, {Source: c, Target: d, Kind: "calls"}}}
	result, err := tools.Callees(context.Background(), ToolQuery{InstallationID: 7, RepositoryID: 9, SymbolID: 1, Depth: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 || result[0].ID != 2 || result[1].ID != 3 {
		t.Fatalf("callees = %+v", result)
	}
	if _, err := tools.Callees(context.Background(), ToolQuery{InstallationID: 7, RepositoryID: 9, SymbolID: 1, Depth: 3}); err == nil {
		t.Fatal("accepted traversal beyond two hops")
	}
}

func TestGraphToolsReturnTestsImplementersAndSiblings(t *testing.T) {
	parent, subject := ToolSymbol{ID: 1}, ToolSymbol{ID: 2}
	sibling, test, implementation := ToolSymbol{ID: 3}, ToolSymbol{ID: 4}, ToolSymbol{ID: 5}
	tools := Tools{Reader: memoryGraph{
		{Source: parent, Target: subject, Kind: "contains"}, {Source: parent, Target: sibling, Kind: "contains"},
		{Source: test, Target: subject, Kind: "tests"}, {Source: implementation, Target: subject, Kind: "implements"},
	}}
	query := ToolQuery{InstallationID: 7, RepositoryID: 9, SymbolID: 2}
	siblings, _ := tools.Siblings(context.Background(), query)
	tests, _ := tools.TestsFor(context.Background(), query)
	implementers, _ := tools.Implementers(context.Background(), query)
	if len(siblings) != 1 || siblings[0].ID != 3 || len(tests) != 1 || tests[0].ID != 4 || len(implementers) != 1 || implementers[0].ID != 5 {
		t.Fatalf("unexpected graph tools: siblings=%+v tests=%+v implementers=%+v", siblings, tests, implementers)
	}
}
