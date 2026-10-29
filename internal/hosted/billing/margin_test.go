package billing

import (
	"context"
	"testing"
	"time"
)

func TestEvaluateMarginAlertsBelowSixtyPercent(t *testing.T) {
	week := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	result, err := EvaluateMargin(WeeklyEconomics{InstallationID: 1, WeekStart: week, RevenueMicros: 100, LLMCostMicros: 41}, .60)
	if err != nil || !result.Available || !result.Alert || result.Margin != .59 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	missing, err := EvaluateMargin(WeeklyEconomics{InstallationID: 1, WeekStart: week}, .60)
	if err != nil || missing.Available || missing.Alert {
		t.Fatalf("missing=%#v err=%v", missing, err)
	}
}

type marginStore struct {
	rows    []WeeklyEconomics
	created bool
}

func (s *marginStore) WeeklyEconomics(context.Context, time.Time) ([]WeeklyEconomics, error) {
	return s.rows, nil
}
func (s *marginStore) RecordMargin(context.Context, WeeklyEconomics, MarginResult) (bool, error) {
	return s.created, nil
}

type marginAlerts struct{ count int }

func (a *marginAlerts) AlertLowMargin(context.Context, WeeklyEconomics, MarginResult) error {
	a.count++
	return nil
}

func TestMarginJobDeduplicatesAlertsThroughStore(t *testing.T) {
	week := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	alerts := &marginAlerts{}
	job := MarginJob{Store: &marginStore{rows: []WeeklyEconomics{{InstallationID: 1, WeekStart: week, RevenueMicros: 100, LLMCostMicros: 50}}, created: false}, Alerter: alerts}
	count, err := job.Run(context.Background(), week)
	if err != nil || count != 0 || alerts.count != 0 {
		t.Fatalf("count=%d alerts=%d err=%v", count, alerts.count, err)
	}
}
