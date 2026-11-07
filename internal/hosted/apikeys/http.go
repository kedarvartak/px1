package apikeys

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RateLimiter is a per-key token bucket. It is process-local: with N API
// processes a key can make up to N × the configured rate.
type RateLimiter struct {
	PerMinute int
	Burst     int
	Now       func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

const maxTrackedKeys = 100_000

// Allow spends one token for key and, when refused, says how long to wait.
func (l *RateLimiter) Allow(key string) (bool, time.Duration) {
	now := time.Now()
	if l.Now != nil {
		now = l.Now()
	}
	perMinute, burst := max(l.PerMinute, 1), max(l.Burst, 1)
	rate := float64(perMinute) / 60
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.buckets == nil {
		l.buckets = map[string]*bucket{}
	}
	b := l.buckets[key]
	if b == nil {
		if len(l.buckets) >= maxTrackedKeys {
			l.evictFull(now, rate, float64(burst))
		}
		b = &bucket{tokens: float64(burst), last: now}
		l.buckets[key] = b
	}
	b.tokens = math.Min(float64(burst), b.tokens+now.Sub(b.last).Seconds()*rate)
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration(math.Ceil((1-b.tokens)/rate*1000)) * time.Millisecond
}

// evictFull drops buckets that have refilled completely; forgetting them
// changes nothing because a new bucket also starts full.
func (l *RateLimiter) evictFull(now time.Time, rate, burst float64) {
	for key, b := range l.buckets {
		if b.tokens+now.Sub(b.last).Seconds()*rate >= burst {
			delete(l.buckets, key)
		}
	}
}

type principalKey struct{}

func PrincipalFrom(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalKey{}).(Principal)
	return principal, ok
}

// Middleware authenticates "Authorization: Bearer px1k_..." with the required
// scope and rate-limits by key. Responses never say whether a key exists.
func Middleware(manager Manager, limiter *RateLimiter, required Scope, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="px1"`)
			http.Error(w, "API key required", http.StatusUnauthorized)
			return
		}
		principal, err := manager.Authenticate(r.Context(), strings.TrimSpace(token), required)
		switch {
		case errors.Is(err, ErrUnauthorized):
			w.Header().Set("WWW-Authenticate", `Bearer realm="px1", error="invalid_token"`)
			http.Error(w, "invalid API key", http.StatusUnauthorized)
			return
		case errors.Is(err, ErrForbidden):
			http.Error(w, "API key lacks the "+string(required)+" scope", http.StatusForbidden)
			return
		case err != nil:
			http.Error(w, "API key verification unavailable", http.StatusServiceUnavailable)
			return
		}
		if limiter != nil {
			if allowed, wait := limiter.Allow(principal.KeyID); !allowed {
				w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, principal)))
	})
}
