package agent

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

type Tier string

const (
	Base Tier = "base"
	Plus Tier = "plus"
	Apex Tier = "apex"
)

// Budget is a hard per-review ceiling. Model calls reserve their worst case
// (prompt tokens plus the maximum output they may produce) before they are
// sent, so a review can never spend more than its budget, only less.
type Budget struct {
	Credits              int64
	MaxInputTokens       int64
	MaxOutputTokens      int64
	MaxCostMicrousd      int64
	MaxSubAgents         int
	MaxToolCallsPerAgent int
	MaxModelCalls        int
	VerifierPasses       int
	WallTime             time.Duration
}

// TierBudgets covers only the agentic tiers. Base remains the fixed v2
// pipeline and has no agent budget. Cost ceilings keep each tier at or above
// the 60% margin guardrail at the Pro list price; a test enforces that.
var TierBudgets = map[Tier]Budget{
	Plus: {
		Credits: 3, MaxInputTokens: 400_000, MaxOutputTokens: 40_000, MaxCostMicrousd: 960_000,
		MaxSubAgents: 6, MaxToolCallsPerAgent: 12, MaxModelCalls: 48, VerifierPasses: 2, WallTime: 4 * time.Minute,
	},
	Apex: {
		Credits: 10, MaxInputTokens: 1_500_000, MaxOutputTokens: 120_000, MaxCostMicrousd: 3_200_000,
		MaxSubAgents: 10, MaxToolCallsPerAgent: 30, MaxModelCalls: 160, VerifierPasses: 2, WallTime: 10 * time.Minute,
	},
}

func BudgetFor(tier Tier) (Budget, error) {
	budget, ok := TierBudgets[tier]
	if !ok {
		return Budget{}, fmt.Errorf("tier %q has no agentic budget", tier)
	}
	return budget, nil
}

var ErrBudgetExhausted = errors.New("review budget exhausted")

type Meter struct {
	budget   Budget
	deadline time.Time
	now      func() time.Time

	mu        sync.Mutex
	input     int64
	output    int64
	cost      int64
	calls     int
	toolCalls map[string]int
	exhausted string
}

type Reservation struct {
	meter               *Meter
	input, output, cost int64
	settled             bool
}

type Spend struct {
	InputTokens, OutputTokens, CostMicrousd int64
	ModelCalls                              int
	ToolCalls                               int
	Exhausted                               string
}

func NewMeter(budget Budget, start time.Time, now func() time.Time) *Meter {
	if now == nil {
		now = time.Now
	}
	return &Meter{budget: budget, deadline: start.Add(budget.WallTime), now: now, toolCalls: map[string]int{}}
}

func (m *Meter) Deadline() time.Time { return m.deadline }

// Reserve holds the worst-case cost of one model call. It fails without
// side effects when the call could cross any ceiling.
func (m *Meter) Reserve(inputTokens, maxOutputTokens, maxCostMicrousd int64) (*Reservation, error) {
	if inputTokens < 0 || maxOutputTokens <= 0 || maxCostMicrousd < 0 {
		return nil, errors.New("reservations need non-negative input, positive output, and non-negative cost")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	switch {
	case !m.now().Before(m.deadline):
		return nil, m.exhaust("wall time")
	case m.calls >= m.budget.MaxModelCalls:
		return nil, m.exhaust("model calls")
	case m.input+inputTokens > m.budget.MaxInputTokens:
		return nil, m.exhaust("input tokens")
	case m.output+maxOutputTokens > m.budget.MaxOutputTokens:
		return nil, m.exhaust("output tokens")
	case m.cost+maxCostMicrousd > m.budget.MaxCostMicrousd:
		return nil, m.exhaust("cost")
	}
	m.calls++
	m.input += inputTokens
	m.output += maxOutputTokens
	m.cost += maxCostMicrousd
	return &Reservation{meter: m, input: inputTokens, output: maxOutputTokens, cost: maxCostMicrousd}, nil
}

// Settle replaces the reservation with actual usage. Usage above the
// reservation is recorded in full and reported so the provider adapter can be
// fixed; the ceiling still holds because later reservations see it.
func (r *Reservation) Settle(inputTokens, outputTokens, costMicrousd int64) error {
	m := r.meter
	m.mu.Lock()
	defer m.mu.Unlock()
	if r.settled {
		return errors.New("reservation already settled")
	}
	r.settled = true
	m.input += inputTokens - r.input
	m.output += outputTokens - r.output
	m.cost += costMicrousd - r.cost
	if inputTokens > r.input || outputTokens > r.output || costMicrousd > r.cost {
		return fmt.Errorf("model usage exceeded its reservation (input %d/%d, output %d/%d, cost %d/%d)", inputTokens, r.input, outputTokens, r.output, costMicrousd, r.cost)
	}
	return nil
}

// UseTool counts one tool call for a sub-agent against its per-agent limit.
func (m *Meter) UseTool(agentID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.now().Before(m.deadline) {
		return m.exhaust("wall time")
	}
	if m.toolCalls[agentID] >= m.budget.MaxToolCallsPerAgent {
		return fmt.Errorf("%w: sub-agent %s used %d tool calls", ErrBudgetExhausted, agentID, m.budget.MaxToolCallsPerAgent)
	}
	m.toolCalls[agentID]++
	return nil
}

func (m *Meter) Spend() Spend {
	m.mu.Lock()
	defer m.mu.Unlock()
	tools := 0
	for _, count := range m.toolCalls {
		tools += count
	}
	return Spend{InputTokens: m.input, OutputTokens: m.output, CostMicrousd: m.cost, ModelCalls: m.calls, ToolCalls: tools, Exhausted: m.exhausted}
}

func (m *Meter) exhaust(limit string) error {
	if m.exhausted == "" {
		m.exhausted = limit
	}
	return fmt.Errorf("%w: %s", ErrBudgetExhausted, limit)
}
