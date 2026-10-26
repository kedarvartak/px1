package learning

import (
	"testing"
	"time"
)

func TestAggregateWeeklyMetricsUsesUTCWeekAndReviewDenominators(t *testing.T) {
	week := time.Date(2026, 10, 21, 12, 0, 0, 0, time.FixedZone("local", 5*60*60+30*60))
	metrics, err := AggregateWeeklyMetrics(3, 9, week, []ReviewOutcome{
		{PullRequestID: "1", OccurredAt: time.Date(2026, 10, 19, 1, 0, 0, 0, time.UTC), Comments: 4, Addressed: 3, Downvoted: 1, Suppressed: 2},
		{PullRequestID: "2", OccurredAt: time.Date(2026, 10, 25, 23, 0, 0, 0, time.UTC), Comments: 2, Addressed: 1, Suppressed: 1},
		{PullRequestID: "old", OccurredAt: time.Date(2026, 10, 18, 23, 59, 0, 0, time.UTC), Comments: 20},
	})
	if err != nil {
		t.Fatal(err)
	}
	if metrics.WeekStart.Weekday() != time.Monday || metrics.PullRequests != 2 || metrics.Comments != 6 || metrics.CommentsPerPR != 3 || metrics.AddressedRate != 4.0/6.0 || metrics.DownvoteRate != 1.0/6.0 || metrics.Suppressed != 3 {
		t.Fatalf("metrics = %#v", metrics)
	}
}

func TestAggregateWeeklyMetricsRejectsDuplicateOrImpossibleOutcomes(t *testing.T) {
	now := time.Now()
	for _, outcomes := range [][]ReviewOutcome{
		{{PullRequestID: "1", OccurredAt: now}, {PullRequestID: "1", OccurredAt: now}},
		{{PullRequestID: "1", OccurredAt: now, Comments: 1, Addressed: 2}},
	} {
		if _, err := AggregateWeeklyMetrics(1, 2, now, outcomes); err == nil {
			t.Fatal("accepted invalid outcomes")
		}
	}
}
