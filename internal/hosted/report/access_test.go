package report

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestOAuthStateIsSignedScopedAndExpires(t *testing.T) {
	now := time.Unix(1000, 0)
	flow := OAuthFlow{ClientID: "client", CallbackURL: "https://app.px1.dev/oauth/callback", StateKey: []byte(strings.Repeat("k", 32)), Now: func() time.Time { return now }}
	binding := "browser-session-binding"
	location, err := flow.Begin("/r/acme/widget/"+strings.Repeat("a", 40), binding)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(location)
	state := parsed.Query().Get("state")
	redirect, err := flow.ValidateState(state, binding)
	if err != nil || !strings.HasPrefix(redirect, "/r/acme/widget/") {
		t.Fatalf("redirect = %q, err = %v", redirect, err)
	}
	if _, err := flow.ValidateState(state+"x", binding); err == nil {
		t.Fatal("accepted tampered state")
	}
	if _, err := flow.ValidateState(state, "different-browser-binding"); err == nil {
		t.Fatal("accepted state from another browser session")
	}
	flow.Now = func() time.Time { return now.Add(11 * time.Minute) }
	if _, err := flow.ValidateState(state, binding); err == nil {
		t.Fatal("accepted expired state")
	}
}

func TestAccessAuthorizerChecksGitHubForEveryRepository(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("missing bearer token")
		}
		if strings.HasSuffix(r.URL.Path, "/allowed") {
			w.WriteHeader(200)
		} else {
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	authorizer := AccessAuthorizer{HTTP: server.Client(), BaseURL: server.URL}
	allowed, err := authorizer.CanRead(context.Background(), "secret", "acme", "allowed")
	if err != nil || !allowed {
		t.Fatalf("allowed = %v, err = %v", allowed, err)
	}
	allowed, err = authorizer.CanRead(context.Background(), "secret", "acme", "private")
	if err != nil || allowed {
		t.Fatalf("allowed = %v, err = %v", allowed, err)
	}
}
