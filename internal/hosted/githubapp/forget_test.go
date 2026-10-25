package githubapp

import (
	"context"
	"testing"
)

type forgetStoreFunc func(context.Context, int64, int64, string, int64) (bool, error)

func (f forgetStoreFunc) DeleteExplicitMemory(ctx context.Context, installationID, repositoryID int64, memoryID string, actorID int64) (bool, error) {
	return f(ctx, installationID, repositoryID, memoryID, actorID)
}

func TestForgetHandlerKeepsTenantAndActorScope(t *testing.T) {
	called := false
	store := forgetStoreFunc(func(_ context.Context, installationID, repositoryID int64, memoryID string, actorID int64) (bool, error) {
		called = installationID == 1 && repositoryID == 2 && actorID == 3 && memoryID == "550e8400-e29b-41d4-a716-446655440000"
		return true, nil
	})
	deleted, err := (ForgetHandler{Store: store}).Handle(context.Background(), Command{Name: "forget", MemoryID: "550e8400-e29b-41d4-a716-446655440000"}, 1, 2, 3)
	if err != nil || !deleted || !called {
		t.Fatalf("forget = %v, %v; called=%v", deleted, err, called)
	}
}
