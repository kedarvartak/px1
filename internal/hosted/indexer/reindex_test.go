package indexer

import (
	"context"
	"reflect"
	"testing"
)

func TestPlanReindexTouchesOnlyChangedBlobs(t *testing.T) {
	plan, err := PlanReindex(map[string]string{"a.go": "same", "b.go": "old", "gone.go": "gone"}, []BlobFile{
		{Path: "b.go", SHA: "new", Size: 20}, {Path: "a.go", SHA: "same", Size: 10}, {Path: "c.go", SHA: "added", Size: 30},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Unchanged != 1 || !reflect.DeepEqual(plan.Delete, []string{"gone.go"}) || len(plan.Upsert) != 2 || plan.Upsert[0].Path != "b.go" {
		t.Fatalf("plan = %+v", plan)
	}
}

type reindexStoreStub struct{ mutation ReindexMutation }

func (s *reindexStoreStub) ApplyReindex(_ context.Context, mutation ReindexMutation) error {
	s.mutation = mutation
	return nil
}

func TestApplyReindexCarriesTenantAndNewHeadAtomically(t *testing.T) {
	store := &reindexStoreStub{}
	plan := ReindexPlan{Upsert: []BlobFile{{Path: "a.go", SHA: "new"}}, Delete: []string{"old.go"}}
	if err := ApplyReindex(context.Background(), store, 7, 9, "head", plan); err != nil {
		t.Fatal(err)
	}
	if store.mutation.InstallationID != 7 || store.mutation.RepositoryID != 9 || store.mutation.HeadSHA != "head" {
		t.Fatalf("mutation = %+v", store.mutation)
	}
}
