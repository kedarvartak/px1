package agent

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"px1/internal/hosted/billing"
	"px1/internal/hosted/publicsite"
)

func TestTierBudgetsMatchPricingAndMarginGuardrail(t *testing.T) {
	var pro publicsite.Plan
	for _, plan := range publicsite.Plans {
		if plan.Slug == "pro" {
			pro = plan
		}
	}
	if _, err := BudgetFor(Base); err == nil {
		t.Fatal("base tier must stay on the fixed pipeline")
	}
	for tier, credits := range map[Tier]int64{Plus: 3, Apex: 10} {
		budget, err := BudgetFor(tier)
		if err != nil {
			t.Fatal(err)
		}
		ceiling := int64(float64(credits*pro.MicrousdPerCredit) * (1 - billing.DefaultMinimumMargin))
		if budget.Credits != credits || budget.MaxCostMicrousd > ceiling || budget.WallTime <= 0 || budget.VerifierPasses != 2 {
			t.Fatalf("%s budget %#v breaks the %d microUSD margin ceiling", tier, budget, ceiling)
		}
	}
	if TierBudgets[Apex].MaxToolCallsPerAgent != 30 {
		t.Fatal("apex allows up to 30 tool calls per sub-agent")
	}
}

func testBudget() Budget {
	return Budget{MaxInputTokens: 100, MaxOutputTokens: 50, MaxCostMicrousd: 1_000, MaxModelCalls: 3, MaxToolCallsPerAgent: 2, WallTime: time.Minute}
}

func TestReservationsNeverCrossTheCeiling(t *testing.T) {
	start := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	meter := NewMeter(testBudget(), start, func() time.Time { return start })
	first, err := meter.Reserve(60, 30, 400)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := meter.Reserve(60, 10, 100); !errors.Is(err, ErrBudgetContended) {
		t.Fatalf("input overrun while held reserved: %v", err)
	}
	if err := first.Settle(40, 5, 100); err != nil {
		t.Fatal(err)
	}
	if spend := meter.Spend(); spend.InputTokens != 40 || spend.OutputTokens != 5 || spend.CostMicrousd != 100 || spend.Exhausted != "" {
		t.Fatalf("settled spend: %#v", spend)
	}
	if err := first.Settle(40, 5, 100); err == nil {
		t.Fatal("double settlement accepted")
	}
	second, err := meter.Reserve(60, 45, 900)
	if err != nil {
		t.Fatalf("released headroom not reusable: %v", err)
	}
	if err := second.Settle(70, 45, 900); err == nil || !strings.Contains(err.Error(), "exceeded its reservation") {
		t.Fatalf("overrun not reported: %v", err)
	}
	if _, err := meter.Reserve(0, 1, 1); !errors.Is(err, ErrBudgetExhausted) {
		t.Fatal("reservation allowed after a ceiling was reached")
	}
}

func TestModelCallsToolCallsAndWallTimeAreCapped(t *testing.T) {
	start := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	now := start
	meter := NewMeter(testBudget(), start, func() time.Time { return now })
	for i := 0; i < 3; i++ {
		if _, err := meter.Reserve(1, 1, 1); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := meter.Reserve(1, 1, 1); !errors.Is(err, ErrBudgetExhausted) {
		t.Fatal("fourth model call allowed")
	}
	if meter.UseTool("a") != nil || meter.UseTool("a") != nil || meter.UseTool("b") != nil {
		t.Fatal("tool calls within the per-agent limit were refused")
	}
	if err := meter.UseTool("a"); !errors.Is(err, ErrToolCallLimit) || errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("third tool call for one agent: %v", err)
	}
	if meter.Spend().Exhausted != "model calls" {
		t.Fatalf("a per-agent tool limit must not be reported as review exhaustion: %q", meter.Spend().Exhausted)
	}
	now = start.Add(time.Minute)
	if err := meter.UseTool("c"); !errors.Is(err, ErrBudgetExhausted) {
		t.Fatal("tool call allowed after the deadline")
	}
}

func TestConcurrentReservationsRespectTheCeiling(t *testing.T) {
	budget := testBudget()
	budget.MaxModelCalls = 1_000
	meter := NewMeter(budget, time.Now(), nil)
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = meter.Reserve(1, 1, 10)
		}()
	}
	wg.Wait()
	if spend := meter.Spend(); spend.InputTokens > budget.MaxInputTokens || spend.OutputTokens > budget.MaxOutputTokens || spend.CostMicrousd > budget.MaxCostMicrousd {
		t.Fatalf("concurrent reservations crossed the ceiling: %#v", spend)
	}
}

func TestInFlightHoldsContendWithoutExhaustingTheReview(t *testing.T) {
	meter := NewMeter(testBudget(), time.Now(), nil)
	big, err := meter.Reserve(10, 40, 900)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := meter.Reserve(10, 20, 100); !errors.Is(err, ErrBudgetContended) || errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("contended reservation: %v", err)
	}
	if spend := meter.Spend(); spend.Exhausted != "" || spend.OutputTokens != 40 {
		t.Fatalf("contention recorded as exhaustion: %#v", spend)
	}
	if err := big.Settle(10, 5, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := meter.Reserve(10, 20, 100); err != nil {
		t.Fatalf("headroom released by settlement not reusable: %v", err)
	}
	if _, err := meter.Reserve(0, 30, 1); !errors.Is(err, ErrBudgetContended) {
		t.Fatalf("output 5 settled + 20 held + 30 > 50 must contend, got %v", err)
	}
	if _, err := meter.Reserve(0, 46, 1); !errors.Is(err, ErrBudgetExhausted) || meter.Spend().Exhausted != "output tokens" {
		t.Fatalf("a request that cannot fit even after holds settle must exhaust: %v", err)
	}
}
