package learning

import (
	"context"
	"testing"
)

type catalogStoreFunc func(context.Context, int64, int64, int) ([]Memory, error)

func (f catalogStoreFunc) ListMemories(ctx context.Context, installationID, repositoryID int64, limit int) ([]Memory, error) {
	return f(ctx, installationID, repositoryID, limit)
}

func TestVisibleMemoriesRequiresEvidenceAndAppliesLimit(t *testing.T) {
	store := catalogStoreFunc(func(_ context.Context, installationID, repositoryID int64, limit int) ([]Memory, error) {
		if installationID != 1 || repositoryID != 2 || limit != MaxVisibleMemories+1 {
			t.Fatal("catalogue query lost its bounds")
		}
		return []Memory{{ID: "m1", Text: "Prefer wrapped errors", EvidenceLinks: []string{"https://github.com/acme/repo/pull/1#discussion_r1"}}}, nil
	})
	memories, err := VisibleMemories(context.Background(), store, 1, 2)
	if err != nil || len(memories) != 1 {
		t.Fatalf("memories = %#v, %v", memories, err)
	}
}
