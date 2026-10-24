package githubapp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeMinter struct {
	mu    sync.Mutex
	calls int
	now   func() time.Time
}

func (m *fakeMinter) Mint(_ context.Context, installationID int64) (Token, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	return Token{Value: "token-for-installation", ExpiresAt: m.now().Add(time.Hour)}, nil
}

func TestCachedTokenSourceCachesAndRefreshes(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	minter := &fakeMinter{now: func() time.Time { return now }}
	source := &CachedTokenSource{Minter: minter, Now: func() time.Time { return now }, Skew: 2 * time.Minute}
	for i := 0; i < 2; i++ {
		if _, err := source.Token(context.Background(), 42); err != nil {
			t.Fatal(err)
		}
	}
	if minter.calls != 1 {
		t.Fatalf("mint calls = %d, want 1", minter.calls)
	}
	now = now.Add(59 * time.Minute)
	if _, err := source.Token(context.Background(), 42); err != nil {
		t.Fatal(err)
	}
	if minter.calls != 2 {
		t.Fatalf("mint calls after refresh = %d, want 2", minter.calls)
	}
}

type staticTokens struct{}

func (staticTokens) Token(context.Context, int64) (Token, error) {
	return Token{Value: "installation-token", ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func TestClientRetriesRateLimitsForSafeRequests(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if got := r.Header.Get("Authorization"); got != "Bearer installation-token" {
			t.Fatalf("authorization = %q", got)
		}
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer server.Close()

	request, err := http.NewRequest(http.MethodGet, server.URL+"/repos/o/r", nil)
	if err != nil {
		t.Fatal(err)
	}
	client := Client{Tokens: staticTokens{}, Sleep: func(context.Context, time.Duration) error { return nil }}
	response, err := client.Do(context.Background(), 42, request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if calls != 2 || response.StatusCode != http.StatusOK {
		t.Fatalf("calls=%d status=%d", calls, response.StatusCode)
	}
}

func TestClientDoesNotReplayUnsafeRequestWithoutIdempotencyKey(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	request, err := http.NewRequest(http.MethodPost, server.URL+"/comments", strings.NewReader(`{"body":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	client := Client{Tokens: staticTokens{}, Sleep: func(context.Context, time.Duration) error { return nil }}
	response, err := client.Do(context.Background(), 42, request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if calls != 1 {
		t.Fatalf("unsafe POST replayed %d times", calls)
	}
}
