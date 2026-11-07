package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"px1/internal/hosted/llm"
)

// Completer is the model gateway as the agent sees it. llm.Gateway satisfies it.
type Completer interface {
	Complete(context.Context, llm.Request) (llm.Response, error)
}

// Model makes metered, traced model calls for one review.
type Model struct {
	Client          Completer
	Provider, Name  string
	Price           llm.Price
	MaxOutputTokens int
	Meter           *Meter
	Trace           *Trace
	// Record, when set, sees every call's prompt and reply (or error). It is
	// for local debugging by the operator; nothing it receives is written to
	// reports or traces.
	Record func(agent, role string, prompt Prompt, reply string, err error)
	// ContentionWait is how long to wait before retrying a reservation that
	// other in-flight calls are holding. Defaults to 50ms.
	ContentionWait time.Duration
}

// EstimateTokens is a deliberately high estimate (three bytes per token) so a
// reservation is more likely to cover real usage than to fall short of it.
func EstimateTokens(text string) int64 { return int64(len(text)+2) / 3 }

// ActualCost prices real usage in micro-USD, rounding up. Cached input uses
// the cache price only when one is configured.
func ActualCost(price llm.Price, usage llm.Usage) int64 {
	cached := usage.CacheReadTokens
	if price.CacheReadMicrousdPerMillion == 0 || cached > usage.InputTokens {
		cached = 0
	}
	total := (usage.InputTokens-cached)*price.InputMicrousdPerMillion + cached*price.CacheReadMicrousdPerMillion + usage.OutputTokens*price.OutputMicrousdPerMillion
	return (total + 999_999) / 1_000_000
}

// Call reserves the worst case for the request, sends it, and settles real
// usage. A failed call is settled at the input estimate with no output, because
// a provider may bill a request it did not complete; this can undercount a
// call that timed out after generating, and the real bill is authoritative.
func (m Model) Call(ctx context.Context, agent, role string, prompt Prompt) (string, error) {
	if m.Client == nil || m.Meter == nil || m.MaxOutputTokens <= 0 || m.Name == "" {
		return "", errors.New("model client, meter, model name, and output limit are required")
	}
	input := EstimateTokens(prompt.System) + EstimateTokens(prompt.User)
	reservation, err := m.reserve(ctx, input, WorstCaseCost(m.Price, input, int64(m.MaxOutputTokens)))
	if err != nil {
		return "", err
	}
	started := time.Now()
	response, callErr := m.Client.Complete(ctx, prompt.Request(m.Provider, m.Name, m.MaxOutputTokens))
	elapsed := time.Since(started)
	usage := response.Usage
	if callErr != nil {
		usage = llm.Usage{InputTokens: input}
	}
	_ = reservation.Settle(usage.InputTokens, usage.OutputTokens, ActualCost(m.Price, usage))
	if m.Record != nil {
		m.Record(agent, role, prompt, response.Text, callErr)
	}
	if m.Trace != nil {
		m.Trace.Model(agent, role, usage.InputTokens, usage.OutputTokens, callErr != nil, elapsed)
	}
	if callErr != nil {
		return "", fmt.Errorf("%s model call: %w", role, callErr)
	}
	return response.Text, nil
}

func (m Model) reserve(ctx context.Context, input, cost int64) (*Reservation, error) {
	wait := m.ContentionWait
	if wait <= 0 {
		wait = 50 * time.Millisecond
	}
	for {
		reservation, err := m.Meter.Reserve(input, int64(m.MaxOutputTokens), cost)
		if !errors.Is(err, ErrBudgetContended) {
			return reservation, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
}
