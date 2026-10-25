package githubapp

import (
	"context"
	"testing"
	"time"
)

type explicitMemoryStoreFunc func(context.Context, ExplicitMemory) error

func (f explicitMemoryStoreFunc) WriteExplicitMemory(ctx context.Context, memory ExplicitMemory) error {
	return f(ctx, memory)
}

func TestRememberHandlerWritesAuthorizedThreadMemory(t *testing.T) {
	now := time.Date(2026, 10, 26, 1, 0, 0, 0, time.UTC)
	written := false
	handler := RememberHandler{Now: func() time.Time { return now }, Store: explicitMemoryStoreFunc(func(_ context.Context, memory ExplicitMemory) error {
		written = memory.InstallationID == 1 && memory.RepositoryID == 2 && memory.ThreadID == "thread" && memory.Text == "Prefer wrapped errors" && memory.CreatedAt == now
		return nil
	})}
	err := handler.Handle(context.Background(), Command{Name: "remember", Text: "Prefer wrapped errors"}, ExplicitMemory{InstallationID: 1, RepositoryID: 2, FindingID: "finding", ThreadID: "thread", ActorID: 3, EvidenceURL: "https://github.com/acme/repo/pull/1#discussion"})
	if err != nil {
		t.Fatal(err)
	}
	if !written {
		t.Fatal("memory was not written")
	}
}

func TestRememberCommandStillRequiresWriteAuthorization(t *testing.T) {
	api := &commandAPIFake{permission: "read"}
	if _, err := (CommandAuthorizer{Client: api, BaseURL: "https://github.test"}).Authorize(context.Background(), 1, "acme", "widget", "octocat", "@px1 remember a preference"); err == nil {
		t.Fatal("read-only user created memory")
	}
}
