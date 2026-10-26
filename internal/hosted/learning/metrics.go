package learning

import (
	"errors"
	"time"
)

type ReviewOutcome struct {
	PullRequestID string
	OccurredAt    time.Time
	Comments      int
	Addressed     int
	Downvoted     int
	Suppressed    int
}

type WeeklyRepositoryMetrics struct {
	InstallationID int64
	RepositoryID   int64
	WeekStart      time.Time
	PullRequests   int
	Comments       int
	CommentsPerPR  float64
	AddressedRate  float64
	DownvoteRate   float64
	Suppressed     int
}

// AggregateWeeklyMetrics produces one UTC-week snapshot. Rates use delivered
// comments as their denominator and are zero when no comments were delivered.
func AggregateWeeklyMetrics(installationID, repositoryID int64, week time.Time, outcomes []ReviewOutcome) (WeeklyRepositoryMetrics, error) {
	if installationID <= 0 || repositoryID <= 0 || week.IsZero() {
		return WeeklyRepositoryMetrics{}, errors.New("tenant and week are required")
	}
	weekStart := startOfUTCWeek(week)
	weekEnd := weekStart.Add(7 * 24 * time.Hour)
	result := WeeklyRepositoryMetrics{InstallationID: installationID, RepositoryID: repositoryID, WeekStart: weekStart}
	seen := map[string]bool{}
	for _, outcome := range outcomes {
		if outcome.PullRequestID == "" || seen[outcome.PullRequestID] || outcome.Comments < 0 || outcome.Addressed < 0 || outcome.Downvoted < 0 || outcome.Suppressed < 0 || outcome.Addressed > outcome.Comments || outcome.Downvoted > outcome.Comments {
			return WeeklyRepositoryMetrics{}, errors.New("weekly review outcome is invalid")
		}
		if outcome.OccurredAt.Before(weekStart) || !outcome.OccurredAt.Before(weekEnd) {
			continue
		}
		seen[outcome.PullRequestID] = true
		result.PullRequests++
		result.Comments += outcome.Comments
		result.Suppressed += outcome.Suppressed
		result.AddressedRate += float64(outcome.Addressed)
		result.DownvoteRate += float64(outcome.Downvoted)
	}
	if result.PullRequests > 0 {
		result.CommentsPerPR = float64(result.Comments) / float64(result.PullRequests)
	}
	if result.Comments > 0 {
		result.AddressedRate /= float64(result.Comments)
		result.DownvoteRate /= float64(result.Comments)
	} else {
		result.AddressedRate = 0
		result.DownvoteRate = 0
	}
	return result, nil
}

func startOfUTCWeek(value time.Time) time.Time {
	value = value.UTC()
	daysSinceMonday := (int(value.Weekday()) + 6) % 7
	date := value.AddDate(0, 0, -daysSinceMonday)
	return time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
}
