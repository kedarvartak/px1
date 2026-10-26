// Package billing contains hosted credit enforcement contracts.
package billing

import (
	"context"
	"errors"
	"fmt"
)

type BalanceReader interface {
	Balance(context.Context, int64) (int64, error)
}

type ReviewDecision struct {
	Allowed          bool
	AvailableCredits int64
	RequiredCredits  int64
	SkipCode         string
	Summary          string
}

type CreditGate struct{ Balances BalanceReader }

type SkipPublisher interface {
	PublishSkippedReview(context.Context, int64, string, ReviewDecision) error
}

type CreditEnforcer struct {
	Gate      CreditGate
	Publisher SkipPublisher
}

// Enforce always attempts to publish a durable explanation when a review is
// skipped, including fail-closed billing outages, so exhaustion is not silent.
func (e CreditEnforcer) Enforce(ctx context.Context, installationID int64, reviewID string, required int64) (ReviewDecision, error) {
	if e.Publisher == nil || reviewID == "" {
		return ReviewDecision{}, errors.New("skip publisher and review ID are required")
	}
	decision, gateErr := e.Gate.Authorize(ctx, installationID, required)
	if decision.Allowed {
		return decision, gateErr
	}
	publishErr := e.Publisher.PublishSkippedReview(ctx, installationID, reviewID, decision)
	if gateErr != nil && publishErr != nil {
		return decision, errors.Join(gateErr, publishErr)
	}
	if gateErr != nil {
		return decision, gateErr
	}
	return decision, publishErr
}

func (g CreditGate) Authorize(ctx context.Context, installationID, required int64) (ReviewDecision, error) {
	if g.Balances == nil || installationID <= 0 || required <= 0 {
		return ReviewDecision{}, errors.New("balance reader, tenant, and positive required credits are required")
	}
	available, err := g.Balances.Balance(ctx, installationID)
	if err != nil {
		return ReviewDecision{Allowed: false, RequiredCredits: required, SkipCode: "billing_unavailable", Summary: "px1 skipped this review because credit availability could not be verified. No AI review ran; retry after billing recovers."}, fmt.Errorf("read credit balance: %w", err)
	}
	decision := ReviewDecision{AvailableCredits: available, RequiredCredits: required}
	if available < required {
		decision.SkipCode = "credits_exhausted"
		decision.Summary = fmt.Sprintf("px1 skipped this review because it requires %d credits and this organization has %d available. No AI review ran; add credits or reduce the review scope, then retry.", required, available)
		return decision, nil
	}
	decision.Allowed = true
	decision.Summary = fmt.Sprintf("px1 reserved up to %d credits for this review; %d were available before reservation.", required, available)
	return decision, nil
}
