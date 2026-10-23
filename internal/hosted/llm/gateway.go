// Package llm provides the bounded, observable model interface used by hosted
// review workers. Provider credentials stay inside provider adapters and are
// never included in requests, cache keys, or call records.
package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

type Request struct {
	Provider        string
	Model           string
	System          string
	Prompt          string
	MaxOutputTokens int
	Temperature     float64
}

type Usage struct {
	InputTokens     int64
	OutputTokens    int64
	CacheReadTokens int64
}

type Response struct {
	Text      string
	Usage     Usage
	RequestID string
	Cached    bool
}

type Provider interface {
	Name() string
	Complete(context.Context, Request) (Response, error)
}

type Cache interface {
	Get(context.Context, string) (Response, bool, error)
	Put(context.Context, string, Response, time.Duration) error
}

type Recorder interface {
	Record(context.Context, CallRecord)
}

type CallRecord struct {
	Provider     string
	Model        string
	Duration     time.Duration
	Attempts     int
	Usage        Usage
	CostMicrousd int64
	Cached       bool
	Err          string
}

type Price struct {
	InputMicrousdPerMillion     int64
	OutputMicrousdPerMillion    int64
	CacheReadMicrousdPerMillion int64
}

type Gateway struct {
	Providers  map[string]Provider
	Cache      Cache
	Recorder   Recorder
	Prices     map[string]Price
	Timeout    time.Duration
	CacheTTL   time.Duration
	MaxRetries int
	Sleep      func(context.Context, time.Duration) error
}

func (g Gateway) Complete(ctx context.Context, request Request) (response Response, err error) {
	provider := g.Providers[request.Provider]
	if provider == nil {
		return Response{}, fmt.Errorf("unknown LLM provider %q", request.Provider)
	}
	if request.Model == "" || request.Prompt == "" || request.MaxOutputTokens <= 0 {
		return Response{}, errors.New("LLM model, prompt, and positive output-token limit are required")
	}
	if request.Temperature < 0 || request.Temperature > 2 {
		return Response{}, errors.New("LLM temperature must be between 0 and 2")
	}

	started := time.Now()
	record := CallRecord{Provider: request.Provider, Model: request.Model}
	defer func() {
		record.Duration = time.Since(started)
		record.Usage = response.Usage
		record.Cached = response.Cached
		if !response.Cached {
			record.CostMicrousd = cost(g.Prices[request.Provider+"/"+request.Model], response.Usage)
		}
		if err != nil {
			record.Err = err.Error()
		}
		if g.Recorder != nil {
			g.Recorder.Record(ctx, record)
		}
	}()

	cacheKey := promptKey(request)
	if g.Cache != nil {
		cached, ok, cacheErr := g.Cache.Get(ctx, cacheKey)
		if cacheErr != nil {
			return Response{}, fmt.Errorf("read LLM prompt cache: %w", cacheErr)
		}
		if ok {
			cached.Cached = true
			return cached, nil
		}
	}

	timeout := g.Timeout
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	maxRetries := g.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}
	if maxRetries == 0 {
		maxRetries = 2
	}

	for attempt := 0; ; attempt++ {
		record.Attempts = attempt + 1
		callCtx, cancel := context.WithTimeout(ctx, timeout)
		response, err = provider.Complete(callCtx, request)
		cancel()
		if err == nil {
			break
		}
		var temporary interface{ Temporary() bool }
		if attempt >= maxRetries || !errors.As(err, &temporary) || !temporary.Temporary() {
			return Response{}, err
		}
		if sleepErr := g.sleep(ctx, time.Duration(1<<attempt)*time.Second); sleepErr != nil {
			return Response{}, sleepErr
		}
	}

	if response.Text == "" {
		return Response{}, errors.New("LLM provider returned an empty response")
	}
	if response.Usage.InputTokens < 0 || response.Usage.OutputTokens < 0 || response.Usage.CacheReadTokens < 0 {
		return Response{}, errors.New("LLM provider returned invalid token usage")
	}
	if g.Cache != nil {
		ttl := g.CacheTTL
		if ttl <= 0 {
			ttl = 15 * time.Minute
		}
		if cacheErr := g.Cache.Put(ctx, cacheKey, response, ttl); cacheErr != nil {
			return Response{}, fmt.Errorf("write LLM prompt cache: %w", cacheErr)
		}
	}
	return response, nil
}

func (g Gateway) sleep(ctx context.Context, duration time.Duration) error {
	if g.Sleep != nil {
		return g.Sleep(ctx, duration)
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func promptKey(request Request) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%d\x00%g", request.Provider, request.Model, request.System, request.Prompt, request.MaxOutputTokens, request.Temperature)))
	return hex.EncodeToString(digest[:])
}

func cost(price Price, usage Usage) int64 {
	return (usage.InputTokens*price.InputMicrousdPerMillion + usage.OutputTokens*price.OutputMicrousdPerMillion + usage.CacheReadTokens*price.CacheReadMicrousdPerMillion) / 1_000_000
}

type memoryEntry struct {
	response  Response
	expiresAt time.Time
}

type MemoryCache struct {
	Now func() time.Time
	mu  sync.Mutex
	set map[string]memoryEntry
}

func (c *MemoryCache) Get(_ context.Context, key string) (Response, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.set[key]
	if !ok || !entry.expiresAt.After(c.now()) {
		delete(c.set, key)
		return Response{}, false, nil
	}
	return entry.response, true, nil
}

func (c *MemoryCache) Put(_ context.Context, key string, response Response, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.set == nil {
		c.set = map[string]memoryEntry{}
	}
	response.Cached = false
	c.set[key] = memoryEntry{response: response, expiresAt: c.now().Add(ttl)}
	return nil
}

func (c *MemoryCache) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}
