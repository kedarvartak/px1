package indexer

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

type BlobFile struct {
	Path string
	SHA  string
	Size int64
}

type ReindexPlan struct {
	Upsert    []BlobFile
	Delete    []string
	Unchanged int
}

func PlanReindex(previous map[string]string, current []BlobFile) (ReindexPlan, error) {
	for path, sha := range previous {
		if path == "" || sha == "" {
			return ReindexPlan{}, errors.New("previous blob inventory requires paths and SHAs")
		}
	}
	seen := make(map[string]bool, len(current))
	plan := ReindexPlan{}
	for _, file := range current {
		if file.Path == "" || file.SHA == "" || file.Size < 0 {
			return ReindexPlan{}, errors.New("indexed blobs require path, SHA, and non-negative size")
		}
		if seen[file.Path] {
			return ReindexPlan{}, fmt.Errorf("duplicate blob path %q", file.Path)
		}
		seen[file.Path] = true
		if previous[file.Path] == file.SHA {
			plan.Unchanged++
			continue
		}
		plan.Upsert = append(plan.Upsert, file)
	}
	for path := range previous {
		if !seen[path] {
			plan.Delete = append(plan.Delete, path)
		}
	}
	sort.Slice(plan.Upsert, func(i, j int) bool { return plan.Upsert[i].Path < plan.Upsert[j].Path })
	sort.Strings(plan.Delete)
	return plan, nil
}

type ReindexMutation struct {
	InstallationID int64
	RepositoryID   int64
	HeadSHA        string
	Upsert         []BlobFile
	Delete         []string
}

type ReindexStore interface {
	ApplyReindex(context.Context, ReindexMutation) error
}

func ApplyReindex(ctx context.Context, store ReindexStore, installationID, repositoryID int64, headSHA string, plan ReindexPlan) error {
	if store == nil || installationID <= 0 || repositoryID <= 0 || headSHA == "" {
		return errors.New("store, tenant, repository, and head SHA are required")
	}
	return store.ApplyReindex(ctx, ReindexMutation{
		InstallationID: installationID, RepositoryID: repositoryID, HeadSHA: headSHA,
		Upsert: append([]BlobFile(nil), plan.Upsert...), Delete: append([]string(nil), plan.Delete...),
	})
}
