package learning

import (
	"context"
	"errors"
	"net/url"
	"strings"
)

const MemoryPageSize = 100

type MemoryCatalogStore interface {
	ListMemories(context.Context, int64, int64, string, int) ([]Memory, error)
}

type MemoryPage struct {
	Items       []Memory
	NextAfterID string
}

// VisibleMemoryPage returns a bounded page for the report. The cursor allows
// every memory to remain reachable without an unbounded report payload.
func VisibleMemoryPage(ctx context.Context, store MemoryCatalogStore, installationID, repositoryID int64, afterID string) (MemoryPage, error) {
	if store == nil || installationID <= 0 || repositoryID <= 0 {
		return MemoryPage{}, errors.New("memory catalogue requires store and tenant")
	}
	memories, err := store.ListMemories(ctx, installationID, repositoryID, afterID, MemoryPageSize+1)
	if err != nil {
		return MemoryPage{}, err
	}
	page := MemoryPage{Items: memories}
	if len(page.Items) > MemoryPageSize {
		page.Items = page.Items[:MemoryPageSize]
		page.NextAfterID = page.Items[len(page.Items)-1].ID
	}
	for _, memory := range page.Items {
		if memory.ID == "" || strings.TrimSpace(memory.Text) == "" || len(memory.EvidenceLinks) == 0 {
			return MemoryPage{}, errors.New("visible memory requires id, text, and evidence")
		}
		for _, raw := range memory.EvidenceLinks {
			evidence, err := url.Parse(raw)
			if err != nil || evidence.Scheme != "https" || !strings.EqualFold(evidence.Host, "github.com") {
				return MemoryPage{}, errors.New("visible memory evidence must be an HTTPS GitHub URL")
			}
		}
	}
	return page, nil
}
