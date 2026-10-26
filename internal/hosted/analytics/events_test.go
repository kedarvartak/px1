package analytics

import (
	"testing"
	"time"
)

func TestRollupDayAggregatesTenantScopedEvents(t *testing.T) {
	day := time.Date(2026, 10, 27, 10, 0, 0, 0, time.FixedZone("IST", 19800))
	events := []Event{
		{ID: "r1", InstallationID: 1, RepositoryID: 2, Team: "platform", Author: "ada", Kind: ReviewEvent, ReviewLatency: 2 * time.Minute, OccurredAt: time.Date(2026, 10, 27, 1, 0, 0, 0, time.UTC)},
		{ID: "r2", InstallationID: 1, RepositoryID: 2, Kind: ReviewEvent, ReviewLatency: 4 * time.Minute, OccurredAt: time.Date(2026, 10, 27, 2, 0, 0, 0, time.UTC)},
		{ID: "f1", InstallationID: 1, RepositoryID: 2, Kind: FindingEvent, Severity: "P1", Addressed: true, OccurredAt: time.Date(2026, 10, 27, 3, 0, 0, 0, time.UTC)},
	}
	got, err := RollupDay(1, 2, day, events)
	if err != nil {
		t.Fatal(err)
	}
	if got.Reviews != 2 || got.Findings != 1 || got.AddressedFindings != 1 || got.MedianReviewLatency != 3*time.Minute || got.Day.Location() != time.UTC {
		t.Fatalf("metrics = %#v", got)
	}
}

func TestRollupDayByDimensionCreatesFilterRows(t *testing.T) {
	now := time.Date(2026, 10, 27, 1, 0, 0, 0, time.UTC)
	rows, err := RollupDayByDimension(1, 2, now, []Event{{ID: "r1", InstallationID: 1, RepositoryID: 2, Team: "platform", Author: "ada", Kind: ReviewEvent, ReviewLatency: time.Minute, OccurredAt: now}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 || rows[0].Team != "" || rows[0].Author != "" {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestRollupDayRejectsCrossTenantAndDuplicateEvents(t *testing.T) {
	now := time.Now()
	for _, events := range [][]Event{
		{{ID: "x", InstallationID: 9, RepositoryID: 2, Kind: ReviewEvent, OccurredAt: now}},
		{{ID: "x", InstallationID: 1, RepositoryID: 2, Kind: ReviewEvent, OccurredAt: now}, {ID: "x", InstallationID: 1, RepositoryID: 2, Kind: ReviewEvent, OccurredAt: now}},
	} {
		if _, err := RollupDay(1, 2, now, events); err == nil {
			t.Fatal("accepted invalid analytics events")
		}
	}
}
