package graph

import (
	"context"
	"strings"
	"testing"
	"time"

	"px1/internal/hosted/indexer"
)

type scipRunnerFunc func(context.Context, SCIPRequest) error

func (f scipRunnerFunc) Run(ctx context.Context, request SCIPRequest) error { return f(ctx, request) }

func TestSCIPIndexerRunsDefaultBranchInLockedSandbox(t *testing.T) {
	called := false
	service := SCIPIndexer{
		Images: map[indexer.Language]SCIPImage{indexer.LanguageGo: {Image: "registry.example/scip-go@sha256:" + strings.Repeat("a", 64), Command: []string{"scip-go", "index"}}},
		Runner: scipRunnerFunc(func(ctx context.Context, request SCIPRequest) error {
			called = true
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > SCIPCPULimit {
				t.Fatal("SCIP run is not time bounded")
			}
			if request.NetworkEnabled || !request.ReadOnlyRoot || request.CPULimit != SCIPCPULimit {
				t.Fatalf("unsafe sandbox request: %+v", request)
			}
			return nil
		}),
	}
	if err := service.Index(context.Background(), indexer.LanguageGo, "/repo", "main", "main", "/out/index.scip"); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("sandbox runner was not called")
	}
}

func TestSCIPIndexerRejectsPullRequestHead(t *testing.T) {
	service := SCIPIndexer{Runner: scipRunnerFunc(func(context.Context, SCIPRequest) error { t.Fatal("must not run"); return nil })}
	if err := service.Index(context.Background(), indexer.LanguageGo, "/repo", "refs/pull/1/head", "main", "/out/index.scip"); err == nil {
		t.Fatal("accepted PR head for executable indexing")
	}
}
