package graph

import (
	"errors"
	"path"
	"sort"
	"strings"
)

type Symbol struct {
	ID        int64
	Path      string
	Name      string
	Kind      string
	Signature string
	StartLine int
	EndLine   int
}

type Reference struct {
	SourceSymbolID int64
	SourcePath     string
	SourceLine     int
	Kind           string
	Target         string
	TargetPath     string
}

type ResolvedEdge struct {
	SourceSymbolID int64
	TargetSymbolID int64
	Unresolved     string
	Kind           string
	Confidence     float64
	SourcePath     string
	SourceLine     int
}

// Resolve links local and imported references deterministically. Ambiguous or
// absent targets remain visible as low-confidence unresolved edges.
func Resolve(symbols []Symbol, references []Reference) ([]ResolvedEdge, error) {
	byID := map[int64]Symbol{}
	byPathName := map[string][]Symbol{}
	byName := map[string][]Symbol{}
	fileSymbols := map[string][]Symbol{}
	for _, symbol := range symbols {
		if symbol.ID <= 0 || symbol.Path == "" || symbol.Name == "" {
			return nil, errors.New("symbols require ID, path, and name")
		}
		if _, exists := byID[symbol.ID]; exists {
			return nil, errors.New("symbol IDs must be unique")
		}
		byID[symbol.ID] = symbol
		cleanPath := path.Clean(symbol.Path)
		pathAliases := []string{cleanPath, strings.TrimSuffix(cleanPath, path.Ext(cleanPath))}
		for _, alias := range pathAliases {
			byPathName[alias+"\x00"+symbol.Name] = append(byPathName[alias+"\x00"+symbol.Name], symbol)
			fileSymbols[alias] = append(fileSymbols[alias], symbol)
		}
		byName[symbol.Name] = append(byName[symbol.Name], symbol)
	}
	edges := make([]ResolvedEdge, 0, len(references))
	for _, reference := range references {
		if _, ok := byID[reference.SourceSymbolID]; !ok || reference.SourcePath == "" || reference.SourceLine <= 0 || reference.Target == "" {
			return nil, errors.New("references require a known source, path, line, and target")
		}
		if reference.Kind != "calls" && reference.Kind != "imports" {
			return nil, errors.New("resolver supports calls and imports")
		}
		edge := ResolvedEdge{SourceSymbolID: reference.SourceSymbolID, Kind: reference.Kind, SourcePath: reference.SourcePath, SourceLine: reference.SourceLine}
		candidates := candidatesFor(reference, byPathName, byName, fileSymbols)
		if len(candidates) == 1 {
			edge.TargetSymbolID = candidates[0].ID
			edge.Confidence = confidenceFor(reference)
		} else {
			edge.Unresolved = reference.Target
			if reference.TargetPath != "" {
				edge.Unresolved = reference.TargetPath + ":" + reference.Target
			}
			edge.Confidence = .2
		}
		edges = append(edges, edge)
	}
	return edges, nil
}

func candidatesFor(reference Reference, byPathName, byName map[string][]Symbol, fileSymbols map[string][]Symbol) []Symbol {
	if reference.Kind == "imports" {
		targetPath := resolvePath(reference.SourcePath, reference.TargetPath)
		candidates := fileSymbols[targetPath]
		if len(candidates) == 0 {
			candidates = fileSymbols[targetPath+"/index"]
		}
		return uniqueSymbols(candidates)
	}
	if reference.TargetPath != "" {
		targetPath := resolvePath(reference.SourcePath, reference.TargetPath)
		return uniqueSymbols(byPathName[targetPath+"\x00"+reference.Target])
	}
	local := byPathName[path.Clean(reference.SourcePath)+"\x00"+reference.Target]
	if len(local) == 1 {
		return local
	}
	return uniqueSymbols(byName[reference.Target])
}

func resolvePath(sourcePath, targetPath string) string {
	clean := strings.TrimPrefix(path.Clean(targetPath), "./")
	if strings.HasPrefix(targetPath, ".") {
		clean = path.Clean(path.Join(path.Dir(sourcePath), targetPath))
	}
	return strings.TrimSuffix(clean, path.Ext(clean))
}

func confidenceFor(reference Reference) float64 {
	if reference.TargetPath != "" {
		return .9
	}
	return .75
}

func uniqueSymbols(symbols []Symbol) []Symbol {
	seen := map[int64]bool{}
	result := make([]Symbol, 0, len(symbols))
	for _, symbol := range symbols {
		if !seen[symbol.ID] {
			seen[symbol.ID] = true
			result = append(result, symbol)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}
