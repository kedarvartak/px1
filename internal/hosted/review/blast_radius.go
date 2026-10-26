package review

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strings"

	"px1/internal/hosted/graph"
)

const MaxBlastRadiusItems = 20

type BlastRadiusItem struct {
	Kind string
	Path string
	Name string
}

// BuildBlastRadius lists unchanged callers and tests connected to changed
// symbols. It reports relationships only; it does not claim those files break.
func BuildBlastRadius(ctx context.Context, tools graph.Tools, installationID, repositoryID int64, symbolIDs []int64, changedPaths []string) ([]BlastRadiusItem, error) {
	if installationID <= 0 || repositoryID <= 0 || len(symbolIDs) == 0 || len(symbolIDs) > 100 {
		return nil, errors.New("blast radius requires a bounded tenant-scoped symbol set")
	}
	changed := map[string]bool{}
	for _, path := range changedPaths {
		changed[filepath.ToSlash(filepath.Clean(path))] = true
	}
	items := map[string]BlastRadiusItem{}
	add := func(kind string, symbols []graph.ToolSymbol) {
		for _, symbol := range symbols {
			path := filepath.ToSlash(filepath.Clean(symbol.Path))
			if path == "." || strings.HasPrefix(path, "../") || changed[path] || len(items) >= MaxBlastRadiusItems {
				continue
			}
			key := kind + "\x00" + path + "\x00" + symbol.Name
			items[key] = BlastRadiusItem{Kind: kind, Path: path, Name: symbol.Name}
		}
	}
	for _, symbolID := range symbolIDs {
		query := graph.ToolQuery{InstallationID: installationID, RepositoryID: repositoryID, SymbolID: symbolID, Depth: 2, Limit: MaxBlastRadiusItems}
		callers, err := tools.Callers(ctx, query)
		if err != nil {
			return nil, err
		}
		tests, err := tools.TestsFor(ctx, query)
		if err != nil {
			return nil, err
		}
		add("caller", callers)
		add("test", tests)
	}
	result := make([]BlastRadiusItem, 0, len(items))
	for _, item := range items {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Kind != result[j].Kind {
			return result[i].Kind < result[j].Kind
		}
		if result[i].Path != result[j].Path {
			return result[i].Path < result[j].Path
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}
