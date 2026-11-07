package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"px1/internal/hosted/review"
)

// OrgLimiter caps concurrently running sub-agents per installation across all
// reviews in this process, so one large organization cannot starve others.
// It is process-local: N workers allow up to N × limit per installation.
type OrgLimiter struct {
	limit int
	mu    sync.Mutex
	slots map[int64]*orgSlot
}

type orgSlot struct {
	sem   chan struct{}
	users int
}

func NewOrgLimiter(limit int) (*OrgLimiter, error) {
	if limit <= 0 {
		return nil, errors.New("per-organization concurrency limit must be positive")
	}
	return &OrgLimiter{limit: limit, slots: map[int64]*orgSlot{}}, nil
}

// Acquire blocks until the installation has a free slot or ctx ends. Idle
// installations are forgotten so the map does not grow with tenant count.
func (l *OrgLimiter) Acquire(ctx context.Context, installationID int64) (func(), error) {
	l.mu.Lock()
	slot := l.slots[installationID]
	if slot == nil {
		slot = &orgSlot{sem: make(chan struct{}, l.limit)}
		l.slots[installationID] = slot
	}
	slot.users++
	l.mu.Unlock()
	leave := func() {
		l.mu.Lock()
		if slot.users--; slot.users == 0 {
			delete(l.slots, installationID)
		}
		l.mu.Unlock()
	}
	select {
	case slot.sem <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-slot.sem; leave() }) }, nil
	case <-ctx.Done():
		leave()
		return nil, ctx.Err()
	}
}

// SubAgent runs one planned task to completion: the model and tool loop that
// spends from meter. It returns candidate findings for the judge.
type SubAgent func(ctx context.Context, task PlanTask, meter *Meter) ([]review.Finding, error)

type TaskOutcome string

const (
	TaskCompleted TaskOutcome = "completed"
	TaskFailed    TaskOutcome = "failed"
	TaskSkipped   TaskOutcome = "skipped"
)

// Partial is streamed as each task finishes so the summary can post early.
// Its findings are provisional: they have not been judged or verified and
// must be labelled that way wherever they are shown.
type Partial struct {
	BlockID     string
	Outcome     TaskOutcome
	Reason      string
	Findings    []review.Finding
	Provisional bool
	Done, Total int
}

type RunResult struct {
	Candidates []review.Finding
	Outcomes   map[string]Partial
	Complete   bool
}

type Runner struct {
	Limiter   *OrgLimiter
	SubAgent  SubAgent
	OnPartial func(Partial)
	// Trace, when set, records each task outcome alongside the model and
	// tool calls the sub-agents recorded through it.
	Trace *Trace
}

// Run starts every task in parallel, subject to the organization limit and
// the review's wall-time deadline. A failed or panicking sub-agent becomes a
// failed task, not a failed review. Once any budget is exhausted, tasks that
// have not started yet are skipped with that reason. OnPartial is called
// from this goroutine only, one task at a time, in completion order.
func (r Runner) Run(ctx context.Context, installationID int64, tasks []PlanTask, meter *Meter) (RunResult, error) {
	if r.Limiter == nil || r.SubAgent == nil || meter == nil || installationID <= 0 {
		return RunResult{}, errors.New("runner needs a limiter, sub-agent, meter, and tenant")
	}
	if len(tasks) == 0 || len(tasks) > meter.budget.MaxSubAgents {
		return RunResult{}, fmt.Errorf("run needs 1 to %d tasks", meter.budget.MaxSubAgents)
	}
	seen := map[string]bool{}
	for _, task := range tasks {
		if task.BlockID == "" || seen[task.BlockID] {
			return RunResult{}, errors.New("tasks need unique change-group IDs")
		}
		seen[task.BlockID] = true
	}
	ctx, cancel := context.WithDeadline(ctx, meter.Deadline())
	defer cancel()

	results := make(chan Partial, len(tasks))
	for _, task := range tasks {
		go func() { results <- r.runOne(ctx, installationID, task, meter) }()
	}

	byBlock := make(map[string]Partial, len(tasks))
	for done := 1; done <= len(tasks); done++ {
		partial := <-results
		partial.Done, partial.Total = done, len(tasks)
		byBlock[partial.BlockID] = partial
		if r.Trace != nil {
			r.Trace.task(partial)
		}
		if r.OnPartial != nil {
			r.OnPartial(partial)
		}
	}

	result := RunResult{Outcomes: byBlock, Complete: true}
	for _, task := range tasks {
		partial := byBlock[task.BlockID]
		result.Candidates = append(result.Candidates, partial.Findings...)
		result.Complete = result.Complete && partial.Outcome == TaskCompleted
	}
	return result, nil
}

func (r Runner) runOne(ctx context.Context, installationID int64, task PlanTask, meter *Meter) (partial Partial) {
	partial = Partial{BlockID: task.BlockID, Provisional: true}
	release, err := r.Limiter.Acquire(ctx, installationID)
	if err != nil {
		partial.Outcome, partial.Reason = TaskSkipped, "review deadline reached before a sub-agent slot was free"
		return partial
	}
	defer release()
	if limit := meter.Spend().Exhausted; limit != "" {
		partial.Outcome, partial.Reason = TaskSkipped, "review budget exhausted ("+limit+") before this task started"
		return partial
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			partial = Partial{BlockID: task.BlockID, Outcome: TaskFailed, Reason: "sub-agent panicked", Provisional: true}
		}
	}()
	findings, err := r.SubAgent(ctx, task, meter)
	if err != nil {
		partial.Outcome, partial.Reason = TaskFailed, failureReason(err)
		partial.Findings = findings
		return partial
	}
	partial.Outcome, partial.Findings = TaskCompleted, findings
	return partial
}

// failureReason keeps provider and tool error text out of PR summaries; only
// the class of failure is reported.
func failureReason(err error) string {
	switch {
	case errors.Is(err, ErrBudgetExhausted):
		return "review budget exhausted during this task"
	case errors.Is(err, ErrToolCallLimit):
		return "sub-agent reached its tool-call limit"
	case errors.Is(err, ErrBudgetContended):
		return "review budget was held by other in-flight sub-agents"
	case errors.Is(err, context.DeadlineExceeded):
		return "review wall time reached during this task"
	case errors.Is(err, context.Canceled):
		return "review cancelled"
	default:
		return "sub-agent failed"
	}
}
