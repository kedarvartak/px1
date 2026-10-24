package llm

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type temporaryError struct{}

func (temporaryError) Error() string   { return "try again" }
func (temporaryError) Temporary() bool { return true }

type fakeProvider struct {
	mu       sync.Mutex
	calls    int
	failures int
}

func (*fakeProvider) Name() string { return "fake" }

func (p *fakeProvider) Complete(_ context.Context, _ Request) (Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.calls <= p.failures {
		return Response{}, temporaryError{}
	}
	return Response{Text: "review result", Usage: Usage{InputTokens: 1000, OutputTokens: 200, CacheReadTokens: 500}}, nil
}

type recordingSink struct {
	records []CallRecord
}

type brokenCache struct {
	getErr error
	putErr error
}

func (c brokenCache) Get(context.Context, string) (Response, bool, error) {
	return Response{}, false, c.getErr
}

func (c brokenCache) Put(context.Context, string, Response, time.Duration) error {
	return c.putErr
}

func (r *recordingSink) Record(_ context.Context, record CallRecord) {
	r.records = append(r.records, record)
}

func TestGatewayRetriesRecordsCostAndCaches(t *testing.T) {
	provider := &fakeProvider{failures: 1}
	recorder := &recordingSink{}
	cache := &MemoryCache{}
	gateway := Gateway{
		Providers: map[string]Provider{"fake": provider}, Cache: cache, Recorder: recorder,
		Prices: map[string]Price{"fake/model": {InputMicrousdPerMillion: 1_000_000, OutputMicrousdPerMillion: 2_000_000, CacheReadMicrousdPerMillion: 500_000}},
		Sleep:  func(context.Context, time.Duration) error { return nil },
	}
	request := Request{Provider: "fake", Model: "model", Prompt: "review this diff", MaxOutputTokens: 500}
	first, err := gateway.Complete(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Cached || provider.calls != 2 {
		t.Fatalf("first response cached=%v calls=%d", first.Cached, provider.calls)
	}
	second, err := gateway.Complete(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Cached || provider.calls != 2 {
		t.Fatalf("second response cached=%v calls=%d", second.Cached, provider.calls)
	}
	if len(recorder.records) != 2 || recorder.records[0].Attempts != 2 || recorder.records[1].Attempts != 0 {
		t.Fatalf("records = %+v", recorder.records)
	}
	if recorder.records[0].CostMicrousd != 1650 {
		t.Fatalf("cost = %d microusd, want 1650", recorder.records[0].CostMicrousd)
	}
	if recorder.records[1].CostMicrousd != 0 {
		t.Fatalf("cached call cost = %d microusd, want 0", recorder.records[1].CostMicrousd)
	}
}

func TestGatewayDoesNotRetryPermanentErrors(t *testing.T) {
	provider := permanentProvider{}
	gateway := Gateway{Providers: map[string]Provider{"permanent": provider}, Sleep: func(context.Context, time.Duration) error { return errors.New("must not sleep") }}
	_, err := gateway.Complete(context.Background(), Request{Provider: "permanent", Model: "model", Prompt: "prompt", MaxOutputTokens: 1})
	if err == nil || err.Error() != "invalid request" {
		t.Fatalf("error = %v", err)
	}
}

func TestGatewayFailsOpenWhenOptionalCacheIsUnavailable(t *testing.T) {
	provider := &fakeProvider{}
	recorder := &recordingSink{}
	gateway := Gateway{
		Providers: map[string]Provider{"fake": provider},
		Cache:     brokenCache{getErr: errors.New("redis unavailable"), putErr: errors.New("redis unavailable")},
		Recorder:  recorder,
	}

	response, err := gateway.Complete(context.Background(), Request{Provider: "fake", Model: "model", Prompt: "prompt", MaxOutputTokens: 1})
	if err != nil {
		t.Fatal(err)
	}
	if response.Text != "review result" || provider.calls != 1 {
		t.Fatalf("response = %+v, provider calls = %d", response, provider.calls)
	}
	if len(recorder.records) != 1 || recorder.records[0].CacheErr == "" {
		t.Fatalf("records = %+v", recorder.records)
	}
}

type permanentProvider struct{}

func (permanentProvider) Name() string { return "permanent" }
func (permanentProvider) Complete(context.Context, Request) (Response, error) {
	return Response{}, errors.New("invalid request")
}
