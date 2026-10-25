package graph

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

type OverlaySymbol struct {
	ID        int64
	Path      string
	Kind      string
	Name      string
	Signature string
	StartLine int
	EndLine   int
}

type OverlayEdge struct {
	SourceID int64
	TargetID int64
	Target   string
	Kind     string
}

type Snapshot struct {
	Symbols []OverlaySymbol
	Edges   []OverlayEdge
}

type ChangedFile struct {
	Path    string
	BlobSHA string
	Content []byte
	Deleted bool
}

type HeadParser interface {
	ParseGraph(context.Context, ChangedFile) ([]OverlaySymbol, []OverlayEdge, error)
}

type SymbolDelta struct {
	Added    []OverlaySymbol
	Removed  []OverlaySymbol
	Modified []OverlaySymbol
}

type Overlay struct {
	Graph Snapshot
	Delta SymbolDelta
}

// BuildOverlay replaces changed default-branch files with parsed PR-head data
// in memory. It never persists or executes pull-request content.
func BuildOverlay(ctx context.Context, base Snapshot, changes []ChangedFile, parser HeadParser) (Overlay, error) {
	if parser == nil {
		return Overlay{}, errors.New("head graph parser is required")
	}
	changed := map[string]bool{}
	for _, change := range changes {
		if change.Path == "" || (!change.Deleted && change.BlobSHA == "") {
			return Overlay{}, errors.New("changed files require path and blob SHA")
		}
		if changed[change.Path] {
			return Overlay{}, fmt.Errorf("duplicate changed path %q", change.Path)
		}
		changed[change.Path] = true
	}
	removedIDs := map[int64]bool{}
	result := Snapshot{}
	baseByKey := map[string]OverlaySymbol{}
	for _, symbol := range base.Symbols {
		if changed[symbol.Path] {
			removedIDs[symbol.ID] = true
			baseByKey[symbolKey(symbol)] = symbol
			continue
		}
		result.Symbols = append(result.Symbols, symbol)
	}
	overlay := Overlay{Graph: result}
	nextID := int64(-1)
	seenHead := map[string]bool{}
	replacementIDs := map[int64]int64{}
	for _, change := range changes {
		if change.Deleted {
			continue
		}
		symbols, edges, err := parser.ParseGraph(ctx, change)
		if err != nil {
			return Overlay{}, fmt.Errorf("parse graph overlay for %s: %w", change.Path, err)
		}
		idMap := map[int64]int64{}
		for _, symbol := range symbols {
			if symbol.ID <= 0 || symbol.Path != change.Path || symbol.Kind == "" || symbol.Name == "" || idMap[symbol.ID] != 0 {
				return Overlay{}, errors.New("overlay parser returned an invalid or duplicate symbol")
			}
			originalID := symbol.ID
			symbol.ID = nextID
			nextID--
			idMap[originalID] = symbol.ID
			key := symbolKey(symbol)
			seenHead[key] = true
			if old, ok := baseByKey[key]; !ok {
				overlay.Delta.Added = append(overlay.Delta.Added, symbol)
			} else {
				replacementIDs[old.ID] = symbol.ID
				if old.Signature != symbol.Signature || old.StartLine != symbol.StartLine || old.EndLine != symbol.EndLine {
					overlay.Delta.Modified = append(overlay.Delta.Modified, symbol)
				}
			}
			overlay.Graph.Symbols = append(overlay.Graph.Symbols, symbol)
		}
		for _, edge := range edges {
			edge.SourceID = idMap[edge.SourceID]
			if edge.SourceID == 0 {
				return Overlay{}, errors.New("overlay edge has an unknown source symbol")
			}
			if mapped := idMap[edge.TargetID]; mapped != 0 {
				edge.TargetID = mapped
			}
			if edge.TargetID == 0 && edge.Target == "" {
				return Overlay{}, errors.New("overlay edge requires a resolved or unresolved target")
			}
			overlay.Graph.Edges = append(overlay.Graph.Edges, edge)
		}
	}
	for _, edge := range base.Edges {
		if removedIDs[edge.SourceID] {
			edge.SourceID = replacementIDs[edge.SourceID]
		}
		if removedIDs[edge.TargetID] {
			edge.TargetID = replacementIDs[edge.TargetID]
		}
		if edge.SourceID != 0 && (edge.TargetID != 0 || edge.Target != "") {
			overlay.Graph.Edges = append(overlay.Graph.Edges, edge)
		}
	}
	for key, symbol := range baseByKey {
		if !seenHead[key] {
			overlay.Delta.Removed = append(overlay.Delta.Removed, symbol)
		}
	}
	sortOverlaySymbols(overlay.Delta.Added)
	sortOverlaySymbols(overlay.Delta.Removed)
	sortOverlaySymbols(overlay.Delta.Modified)
	return overlay, nil
}

func symbolKey(symbol OverlaySymbol) string {
	return symbol.Path + "\x00" + symbol.Kind + "\x00" + symbol.Name
}

func sortOverlaySymbols(symbols []OverlaySymbol) {
	sort.Slice(symbols, func(i, j int) bool { return symbolKey(symbols[i]) < symbolKey(symbols[j]) })
}
