// Package githubapp provides short-lived installation authentication and a
// rate-limit-aware GitHub API client for the hosted reviewer.
package githubapp

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Token struct {
	Value     string
	ExpiresAt time.Time
}

type Minter interface {
	Mint(context.Context, int64) (Token, error)
}

type mintCall struct {
	done  chan struct{}
	token Token
	err   error
}

// CachedTokenSource keeps installation tokens in memory only and coalesces
// concurrent refreshes for the same installation.
type CachedTokenSource struct {
	Minter Minter
	Skew   time.Duration
	Now    func() time.Time

	mu       sync.Mutex
	tokens   map[int64]Token
	inflight map[int64]*mintCall
}

func (s *CachedTokenSource) Token(ctx context.Context, installationID int64) (Token, error) {
	if s.Minter == nil {
		return Token{}, errors.New("github app token minter is required")
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	skew := s.Skew
	if skew <= 0 {
		skew = 2 * time.Minute
	}

	s.mu.Lock()
	if token, ok := s.tokens[installationID]; ok && token.Value != "" && now().Add(skew).Before(token.ExpiresAt) {
		s.mu.Unlock()
		return token, nil
	}
	if call := s.inflight[installationID]; call != nil {
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return Token{}, ctx.Err()
		case <-call.done:
			return call.token, call.err
		}
	}
	if s.inflight == nil {
		s.inflight = map[int64]*mintCall{}
	}
	call := &mintCall{done: make(chan struct{})}
	s.inflight[installationID] = call
	s.mu.Unlock()

	token, err := s.Minter.Mint(ctx, installationID)
	if err == nil && (token.Value == "" || !token.ExpiresAt.After(now().Add(skew))) {
		err = errors.New("github returned an empty or already-expiring installation token")
	}

	s.mu.Lock()
	if err == nil {
		if s.tokens == nil {
			s.tokens = map[int64]Token{}
		}
		s.tokens[installationID] = token
	}
	call.token, call.err = token, err
	delete(s.inflight, installationID)
	close(call.done)
	s.mu.Unlock()
	return token, err
}

type TokenSource interface {
	Token(context.Context, int64) (Token, error)
}

type Client struct {
	HTTP       *http.Client
	Tokens     TokenSource
	MaxRetries int
	Now        func() time.Time
	Sleep      func(context.Context, time.Duration) error
}

func (c Client) Do(ctx context.Context, installationID int64, request *http.Request) (*http.Response, error) {
	if request == nil {
		return nil, errors.New("github request is required")
	}
	if c.Tokens == nil {
		return nil, errors.New("github installation token source is required")
	}
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	maxRetries := c.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	var body []byte
	var err error
	if request.Body != nil {
		body, err = io.ReadAll(request.Body)
		if err != nil {
			return nil, fmt.Errorf("read GitHub request body: %w", err)
		}
	}
	retryable := request.Method == http.MethodGet || request.Method == http.MethodHead || request.Method == http.MethodOptions || request.Header.Get("X-GitHub-Idempotency-Key") != ""

	for attempt := 0; ; attempt++ {
		token, err := c.Tokens.Token(ctx, installationID)
		if err != nil {
			return nil, err
		}
		clone := request.Clone(ctx)
		if body != nil {
			clone.Body = io.NopCloser(bytes.NewReader(body))
			clone.ContentLength = int64(len(body))
		}
		clone.Header = request.Header.Clone()
		clone.Header.Set("Authorization", "Bearer "+token.Value)
		clone.Header.Set("Accept", "application/vnd.github+json")
		clone.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		response, err := httpClient.Do(clone)
		if err != nil {
			if !retryable || attempt >= maxRetries {
				return nil, err
			}
			if err := c.sleep(ctx, backoff(attempt)); err != nil {
				return nil, err
			}
			continue
		}
		wait, limited := c.retryDelay(response, attempt)
		if !limited || !retryable || attempt >= maxRetries {
			return response, nil
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if err := c.sleep(ctx, wait); err != nil {
			return nil, err
		}
	}
}

func (c Client) retryDelay(response *http.Response, attempt int) (time.Duration, bool) {
	if response.StatusCode != http.StatusTooManyRequests && response.StatusCode != http.StatusForbidden && response.StatusCode < 500 {
		return 0, false
	}
	if value := response.Header.Get("Retry-After"); value != "" {
		if seconds, err := strconv.Atoi(value); err == nil {
			return maxDuration(time.Duration(seconds)*time.Second, 0), true
		}
		if when, err := http.ParseTime(value); err == nil {
			return maxDuration(when.Sub(c.now()), 0), true
		}
	}
	if response.Header.Get("X-RateLimit-Remaining") == "0" {
		if epoch, err := strconv.ParseInt(response.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			return maxDuration(time.Unix(epoch, 0).Sub(c.now()), 0), true
		}
	}
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return backoff(attempt), true
	}
	return 0, false
}

func (c Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c Client) sleep(ctx context.Context, duration time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, duration)
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

func backoff(attempt int) time.Duration {
	if attempt > 5 {
		attempt = 5
	}
	return time.Duration(1<<attempt) * time.Second
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

// AppMinter exchanges a short-lived RS256 GitHub App JWT for an installation
// token. PrivateKey is supplied by the deployment secrets manager.
type AppMinter struct {
	AppID      string
	PrivateKey *rsa.PrivateKey
	BaseURL    string
	HTTP       *http.Client
	Now        func() time.Time
}

func (m AppMinter) Mint(ctx context.Context, installationID int64) (Token, error) {
	if m.AppID == "" || m.PrivateKey == nil || installationID <= 0 {
		return Token{}, errors.New("GitHub App ID, private key, and installation ID are required")
	}
	now := time.Now
	if m.Now != nil {
		now = m.Now
	}
	jwt, err := signAppJWT(m.AppID, m.PrivateKey, now())
	if err != nil {
		return Token{}, err
	}
	baseURL := strings.TrimRight(m.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/app/installations/%d/access_tokens", baseURL, installationID), nil)
	if err != nil {
		return Token{}, err
	}
	request.Header.Set("Authorization", "Bearer "+jwt)
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	httpClient := m.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return Token{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, response.Body)
		return Token{}, fmt.Errorf("mint GitHub installation token: status %d", response.StatusCode)
	}
	var payload struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return Token{}, fmt.Errorf("decode GitHub installation token: %w", err)
	}
	return Token{Value: payload.Token, ExpiresAt: payload.ExpiresAt}, nil
}

func signAppJWT(appID string, key *rsa.PrivateKey, now time.Time) (string, error) {
	encode := base64.RawURLEncoding.EncodeToString
	header := encode([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payload, err := json.Marshal(map[string]any{
		"iat": now.Add(-30 * time.Second).Unix(),
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": appID,
	})
	if err != nil {
		return "", err
	}
	unsigned := header + "." + encode(payload)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign GitHub App JWT: %w", err)
	}
	return unsigned + "." + encode(signature), nil
}
