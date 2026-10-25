package graph

import (
	"context"
	"reflect"
	"testing"

	"px1/internal/hosted/indexer"
)

func TestPlanIncrementalReplacesChangedGraphsAndRelinksImporters(t *testing.T) {
	previous := map[string]string{"a.go": "old", "b.go": "same", "deleted.go": "gone"}
	current := []indexer.BlobFile{{Path: "a.go", SHA: "new"}, {Path: "b.go", SHA: "same"}}
	parsed := map[string]FileGraph{"a.go": {Path: "a.go", BlobSHA: "new"}}
	mutation, err := PlanIncremental(previous, current, parsed, map[string][]string{"a.go": {"caller.go"}, "deleted.go": {"other.go"}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(mutation.DeletePaths, []string{"a.go", "deleted.go"}) {
		t.Fatalf("delete paths = %#v", mutation.DeletePaths)
	}
	if !reflect.DeepEqual(mutation.RelinkPaths, []string{"a.go", "caller.go", "other.go"}) {
		t.Fatalf("relink paths = %#v", mutation.RelinkPaths)
	}
	if len(mutation.UpsertFiles) != 1 || mutation.UpsertFiles[0].BlobSHA != "new" {
		t.Fatalf("upserts = %#v", mutation.UpsertFiles)
	}
}

type graphStoreFunc func(context.Context, IncrementalMutation) error

func (f graphStoreFunc) ApplyGraphMutation(ctx context.Context, mutation IncrementalMutation) error {
	return f(ctx, mutation)
}

func TestApplyIncrementalAddsTenantBoundary(t *testing.T) {
	err := ApplyIncremental(context.Background(), graphStoreFunc(func(_ context.Context, mutation IncrementalMutation) error {
		if mutation.InstallationID != 7 || mutation.RepositoryID != 8 || mutation.HeadSHA != "head" {
			t.Fatalf("mutation lost scope: %+v", mutation)
		}
		return nil
	}), 7, 8, "head", IncrementalMutation{})
	if err != nil {
		t.Fatal(err)
	}
}
