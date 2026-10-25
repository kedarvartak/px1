package graph

import "testing"

func TestResolveLinksLocalCallAndImportedFile(t *testing.T) {
	symbols := []Symbol{
		{ID: 1, Path: "app/main.go", Name: "main", Kind: "function"},
		{ID: 2, Path: "app/main.go", Name: "helper", Kind: "function"},
		{ID: 3, Path: "lib/client.go", Name: "Client", Kind: "type"},
	}
	edges, err := Resolve(symbols, []Reference{
		{SourceSymbolID: 1, SourcePath: "app/main.go", SourceLine: 4, Kind: "calls", Target: "helper"},
		{SourceSymbolID: 1, SourcePath: "app/main.go", SourceLine: 2, Kind: "imports", Target: "client", TargetPath: "../lib/client.go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if edges[0].TargetSymbolID != 2 || edges[0].Confidence != .75 {
		t.Fatalf("local call = %+v", edges[0])
	}
	if edges[1].TargetSymbolID != 3 || edges[1].Confidence != .9 {
		t.Fatalf("import = %+v", edges[1])
	}
}

func TestResolveKeepsAmbiguousCallExplicit(t *testing.T) {
	symbols := []Symbol{{ID: 1, Path: "main.go", Name: "main"}, {ID: 2, Path: "a.go", Name: "run"}, {ID: 3, Path: "b.go", Name: "run"}}
	edges, err := Resolve(symbols, []Reference{{SourceSymbolID: 1, SourcePath: "main.go", SourceLine: 4, Kind: "calls", Target: "run"}})
	if err != nil {
		t.Fatal(err)
	}
	if edges[0].TargetSymbolID != 0 || edges[0].Unresolved != "run" || edges[0].Confidence != .2 {
		t.Fatalf("ambiguous edge = %+v", edges[0])
	}
}
