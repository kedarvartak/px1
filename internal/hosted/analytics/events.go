// Package analytics defines auditable event and rollup contracts for hosted metrics.
package analytics

import (
	"context"
	"errors"
	"sort"
	"time"
)

type EventKind string

const (
	ReviewEvent   EventKind = "review"
	FindingEvent  EventKind = "finding"
	FeedbackEvent EventKind = "feedback"
)

type Event struct {
	ID             string
	InstallationID int64
	RepositoryID   int64
	Team           string
	Author         string
	Kind           EventKind
	Severity       string
	Addressed      bool
	Downvoted      bool
	ReviewLatency  time.Duration
	OccurredAt     time.Time
}

type DailyMetrics struct {
	InstallationID      int64
	RepositoryID        int64
	Day                 time.Time
	Team                string
	Author              string
	Reviews             int
	Findings            int
	AddressedFindings   int
	DownvotedFindings   int
	MedianReviewLatency time.Duration
}

type NightlyRollupStore interface {
	EventsForDay(context.Context, int64, int64, time.Time, time.Time) ([]Event, error)
	ReplaceDailyMetrics(context.Context, int64, int64, time.Time, []DailyMetrics) error
}

type NightlyRollupJob struct{ Store NightlyRollupStore }

func (j NightlyRollupJob) Run(ctx context.Context, installationID, repositoryID int64, day time.Time) (int, error) {
	if j.Store == nil {
		return 0, errors.New("nightly rollup store is required")
	}
	start := utcDay(day)
	events, err := j.Store.EventsForDay(ctx, installationID, repositoryID, start, start.Add(24*time.Hour))
	if err != nil {
		return 0, err
	}
	metrics, err := RollupDayByDimension(installationID, repositoryID, start, events)
	if err != nil {
		return 0, err
	}
	if err := j.Store.ReplaceDailyMetrics(ctx, installationID, repositoryID, start, metrics); err != nil {
		return 0, err
	}
	return len(metrics), nil
}

// RollupDay deduplicates immutable events and aggregates one UTC calendar day.
func RollupDay(installationID, repositoryID int64, day time.Time, events []Event) (DailyMetrics, error) {
	if installationID <= 0 || repositoryID <= 0 || day.IsZero() {
		return DailyMetrics{}, errors.New("tenant and day are required")
	}
	start := utcDay(day)
	end := start.Add(24 * time.Hour)
	result := DailyMetrics{InstallationID: installationID, RepositoryID: repositoryID, Day: start}
	seen := map[string]bool{}
	var latencies []time.Duration
	for _, event := range events {
		if event.ID == "" || seen[event.ID] || event.InstallationID != installationID || event.RepositoryID != repositoryID || !validEvent(event) {
			return DailyMetrics{}, errors.New("analytics event is invalid, duplicated, or outside the tenant")
		}
		seen[event.ID] = true
		if event.OccurredAt.Before(start) || !event.OccurredAt.Before(end) {
			continue
		}
		switch event.Kind {
		case ReviewEvent:
			result.Reviews++
			latencies = append(latencies, event.ReviewLatency)
		case FindingEvent:
			result.Findings++
			if event.Addressed {
				result.AddressedFindings++
			}
			if event.Downvoted {
				result.DownvotedFindings++
			}
		}
	}
	if len(latencies) > 0 {
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		middle := len(latencies) / 2
		result.MedianReviewLatency = latencies[middle]
		if len(latencies)%2 == 0 {
			result.MedianReviewLatency = (latencies[middle-1] + latencies[middle]) / 2
		}
	}
	return result, nil
}

// RollupDayByDimension returns an overall row plus team, author, and
// team-author rows so each dashboard filter has an explicit denominator.
func RollupDayByDimension(installationID, repositoryID int64, day time.Time, events []Event) ([]DailyMetrics, error) {
	base, err := RollupDay(installationID, repositoryID, day, events)
	if err != nil {
		return nil, err
	}
	rows := []DailyMetrics{base}
	type dimension struct{ team, author string }
	dimensions := map[dimension]bool{}
	start, end := utcDay(day), utcDay(day).Add(24*time.Hour)
	for _, event := range events {
		if event.OccurredAt.Before(start) || !event.OccurredAt.Before(end) {
			continue
		}
		if event.Team != "" {
			dimensions[dimension{team: event.Team}] = true
		}
		if event.Author != "" {
			dimensions[dimension{author: event.Author}] = true
		}
		if event.Team != "" && event.Author != "" {
			dimensions[dimension{team: event.Team, author: event.Author}] = true
		}
	}
	keys := make([]dimension, 0, len(dimensions))
	for key := range dimensions {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].team+"\x00"+keys[i].author < keys[j].team+"\x00"+keys[j].author })
	for _, key := range keys {
		var selected []Event
		for _, event := range events {
			if (key.team == "" || event.Team == key.team) && (key.author == "" || event.Author == key.author) {
				selected = append(selected, event)
			}
		}
		row, err := RollupDay(installationID, repositoryID, day, selected)
		if err != nil {
			return nil, err
		}
		row.Team, row.Author = key.team, key.author
		rows = append(rows, row)
	}
	return rows, nil
}

func utcDay(day time.Time) time.Time {
	return time.Date(day.UTC().Year(), day.UTC().Month(), day.UTC().Day(), 0, 0, 0, 0, time.UTC)
}

func validEvent(event Event) bool {
	if event.OccurredAt.IsZero() || (event.Kind != ReviewEvent && event.Kind != FindingEvent && event.Kind != FeedbackEvent) {
		return false
	}
	if event.Kind == ReviewEvent {
		return event.ReviewLatency >= 0 && event.Severity == "" && !event.Addressed && !event.Downvoted
	}
	if event.Kind == FindingEvent {
		return event.ReviewLatency == 0 && (event.Severity == "P0" || event.Severity == "P1" || event.Severity == "P2")
	}
	return event.ReviewLatency == 0 && event.Severity == "" && !event.Addressed
}
