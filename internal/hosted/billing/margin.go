// Package billing contains hosted billing guardrails.
package billing

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const DefaultMinimumMargin = 0.60

type WeeklyEconomics struct {
	InstallationID int64
	WeekStart      time.Time
	RevenueMicros  int64
	LLMCostMicros  int64
}

type MarginResult struct {
	Available bool
	Margin    float64
	Alert     bool
	Reason    string
}

func EvaluateMargin(e WeeklyEconomics, minimum float64) (MarginResult, error) {
	if e.InstallationID <= 0 || e.WeekStart.IsZero() || e.RevenueMicros < 0 || e.LLMCostMicros < 0 {
		return MarginResult{}, errors.New("valid tenant, week, revenue, and cost are required")
	}
	if e.RevenueMicros == 0 {
		return MarginResult{Reason: "margin unavailable because the organization has no recognized credit revenue"}, nil
	}
	if minimum <= 0 || minimum >= 1 {
		minimum = DefaultMinimumMargin
	}
	margin := float64(e.RevenueMicros-e.LLMCostMicros) / float64(e.RevenueMicros)
	return MarginResult{Available: true, Margin: margin, Alert: margin < minimum, Reason: fmt.Sprintf("weekly gross margin %.1f%%; threshold %.1f%%", margin*100, minimum*100)}, nil
}

type MarginStore interface {
	WeeklyEconomics(context.Context, time.Time) ([]WeeklyEconomics, error)
	RecordMargin(context.Context, WeeklyEconomics, MarginResult) (bool, error)
}
type MarginAlerter interface {
	AlertLowMargin(context.Context, WeeklyEconomics, MarginResult) error
}
type MarginJob struct {
	Store   MarginStore
	Alerter MarginAlerter
	Minimum float64
}

func (j MarginJob) Run(ctx context.Context, week time.Time) (int, error) {
	if j.Store == nil || j.Alerter == nil {
		return 0, errors.New("margin store and alerter are required")
	}
	week = utcWeek(week)
	rows, err := j.Store.WeeklyEconomics(ctx, week)
	if err != nil {
		return 0, err
	}
	alerts := 0
	for _, row := range rows {
		if !row.WeekStart.Equal(week) {
			return alerts, errors.New("margin store returned a row outside the requested week")
		}
		result, err := EvaluateMargin(row, j.Minimum)
		if err != nil {
			return alerts, err
		}
		created, err := j.Store.RecordMargin(ctx, row, result)
		if err != nil {
			return alerts, err
		}
		if created && result.Alert {
			if err := j.Alerter.AlertLowMargin(ctx, row, result); err != nil {
				return alerts, err
			}
			alerts++
		}
	}
	return alerts, nil
}

func utcWeek(value time.Time) time.Time {
	day := time.Date(value.UTC().Year(), value.UTC().Month(), value.UTC().Day(), 0, 0, 0, 0, time.UTC)
	return day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
}
