package learning

import (
	"context"
	"errors"
	"net/url"
	"strings"
)

const MaxVisibleMemories = 500

type MemoryCatalogStore interface {
	ListMemories(context.Context, int64, int64, int) ([]Memory, error)
}

// VisibleMemories returns the complete bounded repository memory catalogue for
// the report. Evidence is retained so every learned preference is auditable.
func VisibleMemories(ctx context.Context, store MemoryCatalogStore, installationID, repositoryID int64) ([]Memory, error) {
	if store == nil || installationID <= 0 || repositoryID <= 0 {
		return nil, errors.New("memory catalogue requires store and tenant")
	}
	memories, err := store.ListMemories(ctx, installationID, repositoryID, MaxVisibleMemories+1)
	if err != nil {
		return nil, err
	}
	if len(memories) > MaxVisibleMemories {
		return nil, errors.New("memory catalogue exceeds report limit")
	}
	for _, memory := range memories {
		if memory.ID == "" || strings.TrimSpace(memory.Text) == "" || len(memory.EvidenceLinks) == 0 {
			return nil, errors.New("visible memory requires id, text, and evidence")
		}
		for _, raw := range memory.EvidenceLinks {
			evidence, err := url.Parse(raw)
			if err != nil || evidence.Scheme != "https" || !strings.EqualFold(evidence.Host, "github.com") {
				return nil, errors.New("visible memory evidence must be an HTTPS GitHub URL")
			}
		}
	}
	return memories, nil
}
