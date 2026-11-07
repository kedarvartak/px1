package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"px1/internal/hosted/review"
)

func runBudget(subAgents int, wall time.Duration) Budget {
	return Budget{MaxSubAgents: subAgents, MaxInputTokens: 1_000, MaxOutputTokens: 1_000, MaxCostMicrousd: 1_000, MaxModelCalls: 100, MaxToolCallsPerAgent: 5, WallTime: wall}
}

func tasksFor(ids ...string) []PlanTask {
	tasks := make([]PlanTask, len(ids))
	for i, id := range ids {
		tasks[i] = PlanTask{BlockID: id, Focus: "check " + id}
	}
	return tasks
}

func TestRunnerCapsConcurrencyPerOrganization(t *testing.T) {
	limiter, _ := NewOrgLimiter(2)
	var active, peak atomic.Int32
	agent := func(ctx context.Context, task PlanTask, _ *Meter) ([]review.Finding, error) {
		now := active.Add(1)
		for {
			old := peak.Load()
			if now <= old || peak.CompareAndSwap(old, now) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		active.Add(-1)
		return []review.Finding{finding(task.BlockID+".go", 1, 1, "P2")}, nil
	}
	runner := Runner{Limiter: limiter, SubAgent: agent}
	var wg sync.WaitGroup
	for run := 0; run < 2; run++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			meter := NewMeter(runBudget(6, time.Minute), time.Now(), nil)
			result, err := runner.Run(context.Background(), 7, tasksFor("a", "b", "c", "d", "e", "f"), meter)
			if err != nil || !result.Complete || len(result.Candidates) != 6 {
				t.Errorf("result=%#v err=%v", result, err)
			}
		}()
	}
	wg.Wait()
	if peak.Load() > 2 {
		t.Fatalf("installation ran %d sub-agents at once across reviews; cap is 2", peak.Load())
	}
	if len(limiter.slots) != 0 {
		t.Fatalf("idle installations retained: %d", len(limiter.slots))
	}
}

func TestOrganizationsDoNotShareSlots(t *testing.T) {
	limiter, _ := NewOrgLimiter(1)
	holdA, err := limiter.Acquire(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	defer holdA()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	holdB, err := limiter.Acquire(ctx, 2)
	if err != nil {
		t.Fatal("a busy installation blocked another installation")
	}
	holdB()
	if _, err := limiter.Acquire(ctx, 1); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second slot for a capped installation: %v", err)
	}
}

func TestPartialsStreamSeriallyAndAreProvisional(t *testing.T) {
	limiter, _ := NewOrgLimiter(3)
	var calls atomic.Int32
	var seen []Partial
	runner := Runner{
		Limiter: limiter,
		SubAgent: func(_ context.Context, task PlanTask, _ *Meter) ([]review.Finding, error) {
			return []review.Finding{finding(task.BlockID+".go", 1, 1, "P1")}, nil
		},
		OnPartial: func(p Partial) {
			if calls.Add(1) != 1 {
				t.Error("OnPartial called concurrently")
			}
			seen = append(seen, p)
			calls.Add(-1)
		},
	}
	result, err := runner.Run(context.Background(), 7, tasksFor("a", "b", "c"), NewMeter(runBudget(3, time.Minute), time.Now(), nil))
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range seen {
		if p.Done != i+1 || p.Total != 3 || !p.Provisional || p.Outcome != TaskCompleted {
			t.Fatalf("partial %d: %#v", i, p)
		}
	}
	if result.Candidates[0].Path != "a.go" || result.Candidates[2].Path != "c.go" {
		t.Fatalf("candidates must follow plan order, got %v", result.Candidates)
	}
}

func TestFailuresAndPanicsStayInsideTheirTask(t *testing.T) {
	limiter, _ := NewOrgLimiter(3)
	runner := Runner{Limiter: limiter, SubAgent: func(_ context.Context, task PlanTask, _ *Meter) ([]review.Finding, error) {
		switch task.BlockID {
		case "boom":
			panic("nil map")
		case "bad":
			return nil, fmt.Errorf("provider said: sk-live-secret rejected")
		}
		return []review.Finding{finding("ok.go", 1, 1, "P2")}, nil
	}}
	result, err := runner.Run(context.Background(), 7, tasksFor("ok", "bad", "boom"), NewMeter(runBudget(3, time.Minute), time.Now(), nil))
	if err != nil || result.Complete || len(result.Candidates) != 1 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if result.Outcomes["boom"].Outcome != TaskFailed || result.Outcomes["bad"].Outcome != TaskFailed {
		t.Fatalf("outcomes: %#v", result.Outcomes)
	}
	if strings.Contains(result.Outcomes["bad"].Reason, "secret") {
		t.Fatal("provider error text leaked into the task reason")
	}
}

func TestExhaustedBudgetSkipsUnstartedTasks(t *testing.T) {
	limiter, _ := NewOrgLimiter(1)
	runner := Runner{Limiter: limiter, SubAgent: func(_ context.Context, _ PlanTask, meter *Meter) ([]review.Finding, error) {
		_, err := meter.Reserve(2_000, 1, 1)
		return nil, err
	}}
	result, err := runner.Run(context.Background(), 7, tasksFor("a", "b", "c"), NewMeter(runBudget(3, time.Minute), time.Now(), nil))
	if err != nil {
		t.Fatal(err)
	}
	failed, skipped := 0, 0
	for _, outcome := range result.Outcomes {
		switch outcome.Outcome {
		case TaskFailed:
			failed++
			if outcome.Reason != "review budget exhausted during this task" {
				t.Fatalf("reason %q", outcome.Reason)
			}
		case TaskSkipped:
			skipped++
			if !strings.Contains(outcome.Reason, "input tokens") {
				t.Fatalf("skip reason %q does not name the exhausted limit", outcome.Reason)
			}
		}
	}
	if failed != 1 || skipped != 2 || result.Complete {
		t.Fatalf("failed=%d skipped=%d complete=%v", failed, skipped, result.Complete)
	}
}

func TestWallTimeBoundsTheWholeRun(t *testing.T) {
	limiter, _ := NewOrgLimiter(1)
	runner := Runner{Limiter: limiter, SubAgent: func(ctx context.Context, _ PlanTask, _ *Meter) ([]review.Finding, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	started := time.Now()
	result, err := runner.Run(context.Background(), 7, tasksFor("a", "b"), NewMeter(runBudget(2, 30*time.Millisecond), started, nil))
	if err != nil || time.Since(started) > time.Second {
		t.Fatalf("run ignored the deadline: %v", err)
	}
	if result.Outcomes["a"].Outcome == TaskCompleted || result.Outcomes["b"].Outcome == TaskCompleted {
		t.Fatalf("tasks completed past the deadline: %#v", result.Outcomes)
	}
}

func TestRunRejectsInvalidPlans(t *testing.T) {
	limiter, _ := NewOrgLimiter(1)
	runner := Runner{Limiter: limiter, SubAgent: func(context.Context, PlanTask, *Meter) ([]review.Finding, error) { return nil, nil }}
	meter := NewMeter(runBudget(2, time.Minute), time.Now(), nil)
	for name, tasks := range map[string][]PlanTask{"empty": nil, "too many": tasksFor("a", "b", "c"), "duplicate": tasksFor("a", "a")} {
		if _, err := runner.Run(context.Background(), 7, tasks, meter); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	if _, err := NewOrgLimiter(0); err == nil {
		t.Fatal("zero limit accepted")
	}
}
