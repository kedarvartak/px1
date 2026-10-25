package graph

import (
	"context"
	"testing"
)

type headParserFunc func(context.Context, ChangedFile) ([]OverlaySymbol, []OverlayEdge, error)

func (f headParserFunc) ParseGraph(ctx context.Context, file ChangedFile) ([]OverlaySymbol, []OverlayEdge, error) {
	return f(ctx, file)
}

func TestBuildOverlayDiffsChangedFileWithoutPersistingBaseIDs(t *testing.T) {
	base := Snapshot{
		Symbols: []OverlaySymbol{{ID: 1, Path: "a.go", Kind: "function", Name: "old", StartLine: 1, EndLine: 2}, {ID: 2, Path: "keep.go", Kind: "function", Name: "keep"}},
		Edges:   []OverlayEdge{{SourceID: 2, TargetID: 1, Kind: "calls"}},
	}
	overlay, err := BuildOverlay(context.Background(), base, []ChangedFile{{Path: "a.go", BlobSHA: "new", Content: []byte("package p")}}, headParserFunc(func(context.Context, ChangedFile) ([]OverlaySymbol, []OverlayEdge, error) {
		return []OverlaySymbol{{ID: 10, Path: "a.go", Kind: "function", Name: "new", StartLine: 1, EndLine: 3}}, nil, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(overlay.Delta.Added) != 1 || len(overlay.Delta.Removed) != 1 || overlay.Delta.Added[0].ID >= 0 {
		t.Fatalf("delta = %+v", overlay.Delta)
	}
	if len(overlay.Graph.Edges) != 0 {
		t.Fatal("edge to replaced base symbol survived")
	}
	if len(base.Symbols) != 2 || base.Symbols[0].ID != 1 {
		t.Fatal("base graph was mutated")
	}
}

func TestBuildOverlayHandlesDeletedFile(t *testing.T) {
	overlay, err := BuildOverlay(context.Background(), Snapshot{Symbols: []OverlaySymbol{{ID: 1, Path: "gone.go", Kind: "function", Name: "gone"}}}, []ChangedFile{{Path: "gone.go", Deleted: true}}, headParserFunc(func(context.Context, ChangedFile) ([]OverlaySymbol, []OverlayEdge, error) {
		t.Fatal("deleted file must not be parsed")
		return nil, nil, nil
	}))
	if err != nil || len(overlay.Delta.Removed) != 1 || len(overlay.Graph.Symbols) != 0 {
		t.Fatalf("deleted overlay = %+v, %v", overlay, err)
	}
}

func TestBuildOverlayRewiresIncomingEdgeToModifiedSymbol(t *testing.T) {
	base := Snapshot{Symbols: []OverlaySymbol{{ID: 1, Path: "a.go", Kind: "function", Name: "run", Signature: "old"}, {ID: 2, Path: "caller.go", Kind: "function", Name: "caller"}}, Edges: []OverlayEdge{{SourceID: 2, TargetID: 1, Kind: "calls"}}}
	overlay, err := BuildOverlay(context.Background(), base, []ChangedFile{{Path: "a.go", BlobSHA: "new"}}, headParserFunc(func(context.Context, ChangedFile) ([]OverlaySymbol, []OverlayEdge, error) {
		return []OverlaySymbol{{ID: 10, Path: "a.go", Kind: "function", Name: "run", Signature: "new"}}, nil, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(overlay.Graph.Edges) != 1 || overlay.Graph.Edges[0].TargetID >= 0 || len(overlay.Delta.Modified) != 1 {
		t.Fatalf("overlay did not rewire modified symbol: %+v", overlay)
	}
}
