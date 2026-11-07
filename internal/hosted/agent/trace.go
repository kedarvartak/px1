package agent

import (
	"context"
	"sync"
	"time"
	"unicode/utf8"

	"px1/internal/hosted/review"
)

// Trace records every model call, tool call, and task outcome of one agentic
// review, in order, for the report's "How px1 reviewed this" section. It is
// safe for concurrent sub-agents and bounded: events past MaxTraceEvents are
// counted, not stored.
type Trace struct {
	revision string
	tier     Tier
	now      func() time.Time

	mu      sync.Mutex
	events  []review.TraceEvent
	dropped int
}

func NewTrace(revision string, tier Tier, now func() time.Time) *Trace {
	if now == nil {
		now = time.Now
	}
	return &Trace{revision: revision, tier: tier, now: now}
}

// Dispatch runs a tool call through box and records it. Linked-issue content
// is never copied into the trace: reports can be published more widely than
// the tracker it came from.
func (t *Trace) Dispatch(ctx context.Context, box Toolbox, scope Scope, agent string, call Call) (Result, error) {
	started := t.now()
	result, err := box.Dispatch(ctx, scope, call)
	event := review.TraceEvent{Agent: agent, Kind: "tool", Name: call.Name, Detail: clipTrace(string(call.Arguments), review.MaxTraceDetailBytes), ElapsedMS: t.now().Sub(started).Milliseconds()}
	if err != nil {
		event.Failed = true
	} else {
		event.ResultBytes, event.Truncated = len(result.Content), result.Truncated
		if call.Name != ReadIssue {
			event.Preview = clipTrace(result.Content, review.MaxTracePreview)
		}
	}
	t.add(event)
	return result, err
}

// Model records one model call for a role ("planner", "judge", or a
// sub-agent's change-group ID) with its settled token usage.
func (t *Trace) Model(agent, role string, inputTokens, outputTokens int64, failed bool, elapsed time.Duration) {
	t.add(review.TraceEvent{Agent: agent, Kind: "model", Name: role, InputTokens: inputTokens, OutputTokens: outputTokens, Failed: failed, ElapsedMS: elapsed.Milliseconds()})
}

// Note records something that happened outside a model or tool call, such as
// a finding the pipeline discarded and why.
func (t *Trace) Note(agent, name, detail string) {
	t.add(review.TraceEvent{Agent: agent, Kind: "note", Name: name, Detail: clipTrace(detail, review.MaxTraceDetailBytes)})
}

func (t *Trace) task(p Partial) {
	t.add(review.TraceEvent{Agent: p.BlockID, Kind: "task", Name: string(p.Outcome), Detail: p.Reason, Failed: p.Outcome != TaskCompleted})
}

func (t *Trace) add(event review.TraceEvent) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.events) >= review.MaxTraceEvents {
		t.dropped++
		return
	}
	event.Seq = len(t.events) + 1
	t.events = append(t.events, event)
}

// File snapshots the trace with the meter's final spend.
func (t *Trace) File(spend Spend) review.TraceFile {
	t.mu.Lock()
	defer t.mu.Unlock()
	return review.TraceFile{
		Revision: t.revision,
		Tier:     string(t.tier),
		Spend: review.TraceSpend{
			InputTokens: spend.InputTokens, OutputTokens: spend.OutputTokens, CostMicrousd: spend.CostMicrousd,
			ModelCalls: spend.ModelCalls, ToolCalls: spend.ToolCalls, Exhausted: spend.Exhausted,
		},
		Events:  append([]review.TraceEvent{}, t.events...),
		Dropped: t.dropped,
	}
}

func clipTrace(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	cut := limit - len("…")
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut] + "…"
}
