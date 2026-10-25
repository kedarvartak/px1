package learning

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"
)

const MaxPromptMemories = 20

type Memory struct {
	ID              string
	Scope           string
	Kind            string
	Text            string
	PathGlob        string
	Language        string
	Weight          float64
	EvidenceLinks   []string
	LastConfirmedAt time.Time
}

type MemoryQuery struct {
	InstallationID int64
	RepositoryID   int64
	Path           string
	Language       string
	Embedding      []float32
	Limit          int
}

type MemoryStore interface {
	SearchMemories(context.Context, MemoryQuery) ([]Memory, error)
}

// RetrieveMemories always applies the product prompt cap, even if a caller
// requests more. The store performs tenant, scope, and similarity filtering.
func RetrieveMemories(ctx context.Context, store MemoryStore, query MemoryQuery) ([]Memory, error) {
	cleanPath := filepath.ToSlash(filepath.Clean(query.Path))
	if store == nil || query.InstallationID <= 0 || query.RepositoryID <= 0 || query.Path == "" || filepath.IsAbs(query.Path) || cleanPath == ".." || strings.HasPrefix(cleanPath, "../") || query.Language == "" || len(query.Embedding) == 0 {
		return nil, errors.New("memory retrieval requires store, tenant, path, language, and embedding")
	}
	query.Path = cleanPath
	if query.Limit <= 0 || query.Limit > MaxPromptMemories {
		query.Limit = MaxPromptMemories
	}
	memories, err := store.SearchMemories(ctx, query)
	if err != nil {
		return nil, err
	}
	if len(memories) > query.Limit {
		memories = memories[:query.Limit]
	}
	for _, memory := range memories {
		if memory.ID == "" || strings.TrimSpace(memory.Text) == "" || memory.Weight < 0 || memory.Weight > 1 {
			return nil, errors.New("memory store returned invalid memory")
		}
	}
	return memories, nil
}
