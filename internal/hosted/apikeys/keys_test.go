package apikeys_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"px1/internal/hosted/apikeys"
	"px1/internal/hosted/apikeys/apikeystest"
)

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func newManager() (apikeys.Manager, *apikeystest.Store, *clock) {
	store, c := &apikeystest.Store{}, &clock{now: time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)}
	return apikeys.Manager{Store: store, Now: c.Now}, store, c
}

func TestIssuedKeysStoreOnlyADigestAndAuthenticateWithScopes(t *testing.T) {
	manager, store, _ := newManager()
	token, key, err := manager.Issue(context.Background(), 42, "ci bot", []apikeys.Scope{apikeys.FindingsRead, apikeys.ReviewsRead, apikeys.FindingsRead}, 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, "px1k_42_"+key.ID+"_") || len(key.Scopes) != 2 {
		t.Fatalf("token=%q scopes=%v", token, key.Scopes)
	}
	secret := token[strings.LastIndex(token, "_")+1:]
	stored, _, _ := store.Get(context.Background(), 42, key.ID)
	if strings.Contains(stored.Name+string(stored.SecretHash[:]), secret) {
		t.Fatal("plaintext secret stored")
	}
	principal, err := manager.Authenticate(context.Background(), token, apikeys.FindingsRead)
	if err != nil || principal.InstallationID != 42 || principal.KeyID != key.ID {
		t.Fatalf("principal=%#v err=%v", principal, err)
	}
	if _, err := manager.Authenticate(context.Background(), token, apikeys.ReviewsTrigger); !errors.Is(err, apikeys.ErrForbidden) {
		t.Fatalf("missing scope: %v", err)
	}
}

func TestAuthenticationFailuresAreIndistinguishable(t *testing.T) {
	manager, _, c := newManager()
	token, key, _ := manager.Issue(context.Background(), 42, "k", []apikeys.Scope{apikeys.ReviewsRead}, time.Hour)
	wrongSecret := token[:len(token)-1] + map[bool]string{true: "A", false: "B"}[token[len(token)-1] != 'A']
	otherTenant := strings.Replace(token, "px1k_42_", "px1k_43_", 1)
	for name, presented := range map[string]string{
		"empty": "", "garbage": "Bearer nope", "wrong secret": wrongSecret, "other tenant": otherTenant,
		"unknown id": strings.Replace(token, key.ID, "0123456789abcdef", 1), "leading zero tenant": strings.Replace(token, "px1k_42", "px1k_042", 1),
	} {
		if _, err := manager.Authenticate(context.Background(), presented, ""); !errors.Is(err, apikeys.ErrUnauthorized) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	c.now = c.now.Add(time.Hour)
	if _, err := manager.Authenticate(context.Background(), token, ""); !errors.Is(err, apikeys.ErrUnauthorized) {
		t.Fatal("expired key accepted")
	}
}

func TestRotationOverlapsThenRetiresTheOldKey(t *testing.T) {
	manager, _, c := newManager()
	oldToken, old, _ := manager.Issue(context.Background(), 42, "deploy", []apikeys.Scope{apikeys.ReviewsRead}, 90*24*time.Hour)
	newToken, next, err := manager.Rotate(context.Background(), 42, old.ID, time.Hour)
	if err != nil || next.Name != "deploy" || next.Scopes[0] != apikeys.ReviewsRead || next.ID == old.ID {
		t.Fatalf("next=%#v err=%v", next, err)
	}
	for _, token := range []string{oldToken, newToken} {
		if _, err := manager.Authenticate(context.Background(), token, ""); err != nil {
			t.Fatalf("key unusable during overlap: %v", err)
		}
	}
	if _, _, err := manager.Rotate(context.Background(), 42, old.ID, time.Hour); err == nil {
		t.Fatal("a superseded key was rotated twice")
	}
	c.now = c.now.Add(time.Hour)
	if _, err := manager.Authenticate(context.Background(), oldToken, ""); !errors.Is(err, apikeys.ErrUnauthorized) {
		t.Fatal("old key outlived its overlap")
	}
	if _, err := manager.Authenticate(context.Background(), newToken, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.Rotate(context.Background(), 42, next.ID, 8*24*time.Hour); err == nil {
		t.Fatal("overlap above seven days accepted")
	}
	if err := manager.Revoke(context.Background(), 42, next.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Authenticate(context.Background(), newToken, ""); !errors.Is(err, apikeys.ErrUnauthorized) {
		t.Fatal("revoked key accepted")
	}
}

func TestIssueValidatesInput(t *testing.T) {
	manager, _, _ := newManager()
	for name, issue := range map[string]func() error{
		"no scopes": func() error { _, _, err := manager.Issue(context.Background(), 1, "k", nil, time.Hour); return err },
		"unknown scope": func() error {
			_, _, err := manager.Issue(context.Background(), 1, "k", []apikeys.Scope{"admin"}, time.Hour)
			return err
		},
		"forever": func() error {
			_, _, err := manager.Issue(context.Background(), 1, "k", []apikeys.Scope{apikeys.ReviewsRead}, 400*24*time.Hour)
			return err
		},
		"no tenant": func() error {
			_, _, err := manager.Issue(context.Background(), 0, "k", []apikeys.Scope{apikeys.ReviewsRead}, time.Hour)
			return err
		},
		"no name": func() error {
			_, _, err := manager.Issue(context.Background(), 1, " ", []apikeys.Scope{apikeys.ReviewsRead}, time.Hour)
			return err
		},
	} {
		if issue() == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestRateLimiterRefillsPerKey(t *testing.T) {
	now := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	limiter := &apikeys.RateLimiter{PerMinute: 60, Burst: 2, Now: func() time.Time { return now }}
	if ok, _ := limiter.Allow("a"); !ok {
		t.Fatal("first request refused")
	}
	limiter.Allow("a")
	ok, wait := limiter.Allow("a")
	if ok || wait != time.Second {
		t.Fatalf("burst exceeded: ok=%v wait=%s", ok, wait)
	}
	if ok, _ := limiter.Allow("b"); !ok {
		t.Fatal("keys share a bucket")
	}
	now = now.Add(time.Second)
	if ok, _ := limiter.Allow("a"); !ok {
		t.Fatal("bucket did not refill")
	}
}

func TestMiddlewareMapsFailuresToHTTPStatus(t *testing.T) {
	manager, _, _ := newManager()
	token, _, _ := manager.Issue(context.Background(), 42, "k", []apikeys.Scope{apikeys.FindingsRead}, time.Hour)
	limiter := &apikeys.RateLimiter{PerMinute: 1, Burst: 1}
	handler := apikeys.Middleware(manager, limiter, apikeys.FindingsRead, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, _ := apikeys.PrincipalFrom(r.Context())
		w.Write([]byte(principal.KeyID))
	}))
	forbidden := apikeys.Middleware(manager, nil, apikeys.ReviewsTrigger, handler)
	request := func(h http.Handler, auth string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if auth != "" {
			r.Header.Set("Authorization", auth)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := request(handler, ""); w.Code != http.StatusUnauthorized || w.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("missing key: %d", w.Code)
	}
	if w := request(handler, "Bearer px1k_1_0000000000000000_"+strings.Repeat("a", 43)); w.Code != http.StatusUnauthorized {
		t.Fatalf("unknown key: %d", w.Code)
	}
	if w := request(forbidden, "Bearer "+token); w.Code != http.StatusForbidden {
		t.Fatalf("missing scope: %d", w.Code)
	}
	if w := request(handler, "Bearer "+token); w.Code != http.StatusOK || w.Body.Len() != 16 {
		t.Fatalf("valid key: %d %q", w.Code, w.Body.String())
	}
	if w := request(handler, "Bearer "+token); w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("rate limit: %d", w.Code)
	}
}
