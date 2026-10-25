package graph

import (
	"context"
	"errors"
	"sort"

	"px1/internal/hosted/indexer"
)

type FileGraph struct {
	Path    string
	BlobSHA string
	Symbols []SymbolRecord
	Edges   []EdgeRecord
}

type SymbolRecord struct {
	LocalID   string
	Kind      string
	Name      string
	Signature string
	StartLine int
	EndLine   int
}

type EdgeRecord struct {
	SourceLocalID string
	Target        string
	Kind          string
	SourceLine    int
}

type IncrementalMutation struct {
	InstallationID int64
	RepositoryID   int64
	HeadSHA        string
	DeletePaths    []string
	UpsertFiles    []FileGraph
	RelinkPaths    []string
}

type IncrementalStore interface {
	ApplyGraphMutation(context.Context, IncrementalMutation) error
}

// PlanIncremental builds one atomic graph mutation. Changed paths are deleted
// before replacement, and both changed targets and their known importers are
// re-linked by the store in the same transaction.
func PlanIncremental(previous map[string]string, current []indexer.BlobFile, parsed map[string]FileGraph, importers map[string][]string) (IncrementalMutation, error) {
	plan, err := indexer.PlanReindex(previous, current)
	if err != nil {
		return IncrementalMutation{}, err
	}
	mutation := IncrementalMutation{}
	changed := map[string]bool{}
	replaced := map[string]bool{}
	for _, file := range plan.Upsert {
		graph, ok := parsed[file.Path]
		if !ok || graph.Path != file.Path || graph.BlobSHA != file.SHA {
			return IncrementalMutation{}, errors.New("every changed blob requires a matching parsed graph")
		}
		mutation.UpsertFiles = append(mutation.UpsertFiles, graph)
		mutation.DeletePaths = append(mutation.DeletePaths, file.Path)
		changed[file.Path] = true
		replaced[file.Path] = true
	}
	for _, deleted := range plan.Delete {
		mutation.DeletePaths = append(mutation.DeletePaths, deleted)
		changed[deleted] = true
	}
	relink := map[string]bool{}
	for changedPath := range changed {
		if replaced[changedPath] {
			relink[changedPath] = true
		}
		for _, importer := range importers[changedPath] {
			if importer != "" {
				relink[importer] = true
			}
		}
	}
	for path := range relink {
		mutation.RelinkPaths = append(mutation.RelinkPaths, path)
	}
	sort.Strings(mutation.DeletePaths)
	sort.Slice(mutation.UpsertFiles, func(i, j int) bool { return mutation.UpsertFiles[i].Path < mutation.UpsertFiles[j].Path })
	sort.Strings(mutation.RelinkPaths)
	return mutation, nil
}

func ApplyIncremental(ctx context.Context, store IncrementalStore, installationID, repositoryID int64, headSHA string, mutation IncrementalMutation) error {
	if store == nil || installationID <= 0 || repositoryID <= 0 || headSHA == "" {
		return errors.New("store, tenant, repository, and head SHA are required")
	}
	mutation.InstallationID = installationID
	mutation.RepositoryID = repositoryID
	mutation.HeadSHA = headSHA
	return store.ApplyGraphMutation(ctx, mutation)
}
