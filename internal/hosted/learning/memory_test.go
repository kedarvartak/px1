package learning

import (
	"context"
	"fmt"
	"testing"
)

type memoryStoreFunc func(context.Context, MemoryQuery) ([]Memory, error)

func (f memoryStoreFunc) SearchMemories(ctx context.Context, query MemoryQuery) ([]Memory, error) {
	return f(ctx, query)
}

func TestRetrieveMemoriesEnforcesTenantScopeAndPromptCap(t *testing.T) {
	store := memoryStoreFunc(func(_ context.Context, query MemoryQuery) ([]Memory, error) {
		if query.InstallationID != 1 || query.RepositoryID != 2 || query.Path != "src/app.go" || query.Language != "go" || query.Limit != MaxPromptMemories {
			t.Fatalf("query = %+v", query)
		}
		result := make([]Memory, 25)
		for i := range result {
			result[i] = Memory{ID: fmt.Sprint(i + 1), Text: "Use wrapped errors", Weight: 1}
		}
		return result, nil
	})
	memories, err := RetrieveMemories(context.Background(), store, MemoryQuery{InstallationID: 1, RepositoryID: 2, Path: "src/app.go", Language: "go", Embedding: []float32{1}, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(memories) != MaxPromptMemories {
		t.Fatalf("returned %d memories", len(memories))
	}
}
