package learning

import (
	"context"
	"testing"
)

type catalogStoreFunc func(context.Context, int64, int64, string, int) ([]Memory, error)

func (f catalogStoreFunc) ListMemories(ctx context.Context, installationID, repositoryID int64, afterID string, limit int) ([]Memory, error) {
	return f(ctx, installationID, repositoryID, afterID, limit)
}

func TestVisibleMemoryPageRequiresEvidenceAndAppliesCursorLimit(t *testing.T) {
	store := catalogStoreFunc(func(_ context.Context, installationID, repositoryID int64, afterID string, limit int) ([]Memory, error) {
		if installationID != 1 || repositoryID != 2 || afterID != "previous" || limit != MemoryPageSize+1 {
			t.Fatal("catalogue query lost its bounds")
		}
		memories := make([]Memory, MemoryPageSize+1)
		for i := range memories {
			memories[i] = Memory{ID: string(rune('a' + i)), Text: "Prefer wrapped errors", EvidenceLinks: []string{"https://github.com/acme/repo/pull/1#discussion_r1"}}
		}
		return memories, nil
	})
	page, err := VisibleMemoryPage(context.Background(), store, 1, 2, "previous")
	if err != nil || len(page.Items) != MemoryPageSize || page.NextAfterID == "" {
		t.Fatalf("page = %#v, %v", page, err)
	}
}
