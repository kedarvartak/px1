package onboarding

import (
	"context"
	"strings"
	"testing"
	"time"
)

type pullReader struct {
	pull  MergedPull
	found bool
}

func (r pullReader) LatestMergedPull(context.Context, Repository) (MergedPull, bool, error) {
	return r.pull, r.found, nil
}

type demoQueue struct {
	jobs []DemoReview
	keys map[string]bool
}

func (q *demoQueue) EnqueueDemoReview(_ context.Context, job DemoReview) (bool, error) {
	if q.keys[job.IdempotencyKey] {
		return false, nil
	}
	q.keys[job.IdempotencyKey] = true
	q.jobs = append(q.jobs, job)
	return true, nil
}

func TestDemoSchedulerUsesLatestMergedHeadWithProvenance(t *testing.T) {
	pull := MergedPull{Number: 9, HeadSHA: strings.Repeat("A", 40), URL: "https://github.com/acme/api/pull/9", MergedAt: time.Now()}
	queue := &demoQueue{keys: map[string]bool{}}
	scheduler := DemoScheduler{Pulls: pullReader{pull: pull, found: true}, Queue: queue}
	repositories := []Repository{{InstallationID: 7, RepositoryID: 8, Owner: "acme", Name: "api"}}
	count, err := scheduler.Schedule(context.Background(), 7, repositories)
	if err != nil || count != 1 || len(queue.jobs) != 1 {
		t.Fatalf("count=%d jobs=%#v err=%v", count, queue.jobs, err)
	}
	if !strings.Contains(queue.jobs[0].Provenance, "latest merged") || queue.jobs[0].HeadSHA != strings.Repeat("a", 40) {
		t.Fatalf("job=%#v", queue.jobs[0])
	}
	count, err = scheduler.Schedule(context.Background(), 7, repositories)
	if err != nil || count != 0 {
		t.Fatalf("duplicate count=%d err=%v", count, err)
	}
}

func TestDemoSchedulerRejectsCrossTenantRepository(t *testing.T) {
	scheduler := DemoScheduler{Pulls: pullReader{}, Queue: &demoQueue{keys: map[string]bool{}}}
	if _, err := scheduler.Schedule(context.Background(), 7, []Repository{{InstallationID: 9, RepositoryID: 8, Owner: "acme", Name: "api"}}); err == nil {
		t.Fatal("accepted cross-tenant repository")
	}
}
