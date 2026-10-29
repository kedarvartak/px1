package billing

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fixedBalance struct {
	credits int64
	err     error
}

type skipPublisher struct {
	calls    int
	decision ReviewDecision
}

func (p *skipPublisher) PublishSkippedReview(_ context.Context, _ int64, _ string, decision ReviewDecision) error {
	p.calls++
	p.decision = decision
	return nil
}

func (b fixedBalance) Balance(context.Context, int64) (int64, error) { return b.credits, b.err }

func TestCreditGateAllowsOnlyAffordableReviews(t *testing.T) {
	allowed, err := (CreditGate{Balances: fixedBalance{credits: 9}}).Authorize(context.Background(), 1, 5)
	if err != nil || !allowed.Allowed {
		t.Fatalf("allowed=%#v err=%v", allowed, err)
	}
	skipped, err := (CreditGate{Balances: fixedBalance{credits: 2}}).Authorize(context.Background(), 1, 5)
	if err != nil || skipped.Allowed || skipped.SkipCode != "credits_exhausted" || !strings.Contains(skipped.Summary, "requires 5 credits") {
		t.Fatalf("skipped=%#v err=%v", skipped, err)
	}
}

func TestCreditGateFailsClosedWithVisibleSummary(t *testing.T) {
	decision, err := (CreditGate{Balances: fixedBalance{err: errors.New("database unavailable")}}).Authorize(context.Background(), 1, 5)
	if err == nil || decision.Allowed || decision.SkipCode != "billing_unavailable" || !strings.Contains(decision.Summary, "No AI review ran") {
		t.Fatalf("decision=%#v err=%v", decision, err)
	}
}

func TestCreditEnforcerPublishesExhaustion(t *testing.T) {
	publisher := &skipPublisher{}
	decision, err := (CreditEnforcer{Gate: CreditGate{Balances: fixedBalance{credits: 0}}, Publisher: publisher}).Enforce(context.Background(), 1, "review-9", 5)
	if err != nil || decision.Allowed || publisher.calls != 1 || publisher.decision.SkipCode != "credits_exhausted" {
		t.Fatalf("decision=%#v calls=%d err=%v", decision, publisher.calls, err)
	}
}
