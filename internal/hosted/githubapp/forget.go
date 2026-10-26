package githubapp

import (
	"context"
	"errors"
)

type ForgetMemoryStore interface {
	DeleteExplicitMemory(context.Context, int64, int64, string, int64) (bool, error)
}

type ForgetHandler struct{ Store ForgetMemoryStore }

// Handle deletes one tenant-scoped memory after CommandAuthorizer has
// established that the actor has write access to the repository.
func (h ForgetHandler) Handle(ctx context.Context, command Command, installationID, repositoryID, actorID int64) (bool, error) {
	if h.Store == nil || command.Name != "forget" || !validMemoryID(command.MemoryID) || installationID <= 0 || repositoryID <= 0 || actorID <= 0 {
		return false, errors.New("authorized forget command and tenant-scoped memory are required")
	}
	return h.Store.DeleteExplicitMemory(ctx, installationID, repositoryID, command.MemoryID, actorID)
}
