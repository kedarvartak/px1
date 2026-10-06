package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const testReviewSHA = "0123456789abcdef0123456789abcdef01234567"

func TestParseGitHubReviewTarget(t *testing.T) {
	target, err := parseGitHubReviewTarget("/github/Acme/widgets.go/pull/42", url.Values{"sha": {strings.ToUpper(testReviewSHA)}})
	if err != nil {
		t.Fatal(err)
	}
	want := githubReviewTarget{Provider: "github", Owner: "Acme", Repository: "widgets.go", PullRequest: 42, Head: testReviewSHA}
	if *target != want {
		t.Fatalf("target = %#v, want %#v", *target, want)
	}
}

func TestParseGitHubReviewTargetRejectsAmbiguousLinks(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		query url.Values
	}{
		{"missing owner", "/github//repo/pull/1", url.Values{"sha": {testReviewSHA}}},
		{"invalid owner", "/github/-acme/repo/pull/1", url.Values{"sha": {testReviewSHA}}},
		{"invalid owner hyphens", "/github/acme--team/repo/pull/1", url.Values{"sha": {testReviewSHA}}},
		{"invalid repository", "/github/acme/re%20po/pull/1", url.Values{"sha": {testReviewSHA}}},
		{"missing pull number", "/github/acme/repo/pull", url.Values{"sha": {testReviewSHA}}},
		{"zero pull number", "/github/acme/repo/pull/0", url.Values{"sha": {testReviewSHA}}},
		{"trailing slash", "/github/acme/repo/pull/1/", url.Values{"sha": {testReviewSHA}}},
		{"missing sha", "/github/acme/repo/pull/1", url.Values{}},
		{"short sha", "/github/acme/repo/pull/1", url.Values{"sha": {"01234567"}}},
		{"duplicate sha", "/github/acme/repo/pull/1", url.Values{"sha": {testReviewSHA, testReviewSHA}}},
		{"unknown query", "/github/acme/repo/pull/1", url.Values{"sha": {testReviewSHA}, "next": {"1"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseGitHubReviewTarget(tt.path, tt.query); err == nil {
				t.Fatal("expected link to be rejected")
			}
		})
	}
}

func TestGitHubReviewRouteEmbedsImmutableTarget(t *testing.T) {
	s, _ := newTestServer(t)
	r := httptest.NewRequest(http.MethodGet, "/github/acme/widgets/pull/42?sha="+testReviewSHA, nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	prefix := `<script type="application/json" id="` + reviewTargetElementID + `">`
	start := strings.Index(body, prefix)
	if start < 0 {
		t.Fatalf("review target metadata missing from response: %s", body)
	}
	start += len(prefix)
	end := strings.Index(body[start:], "</script>")
	if end < 0 {
		t.Fatal("review target metadata is not terminated")
	}
	var target githubReviewTarget
	if err := json.Unmarshal([]byte(body[start:start+end]), &target); err != nil {
		t.Fatal(err)
	}
	if target.Owner != "acme" || target.Repository != "widgets" || target.PullRequest != 42 || target.Head != testReviewSHA {
		t.Fatalf("unexpected embedded target: %#v", target)
	}
}

func TestGitHubReviewRouteRequiresValidSignatureWhenConfigured(t *testing.T) {
	t.Setenv(reviewLinkSecretEnv, "review-link-secret-for-tests")
	s, _ := newTestServer(t)
	unsigned := "/github/acme/widgets/pull/42?sha=" + testReviewSHA
	for _, tt := range []struct {
		name string
		url  string
		code int
	}{
		{"missing signature", unsigned, http.StatusUnauthorized},
		{"wrong signature", unsigned + "&sig=" + strings.Repeat("0", 64), http.StatusForbidden},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, tt.url, nil)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != tt.code {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, tt.code, w.Body.String())
			}
		})
	}
	target, err := parseGitHubReviewTarget("/github/acme/widgets/pull/42", url.Values{"sha": {testReviewSHA}})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, reviewLinkPath(*target), nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), reviewTargetElementID) {
		t.Fatalf("signed route status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestGitHubReviewRouteRejectsInvalidRequests(t *testing.T) {
	s, _ := newTestServer(t)
	for _, tt := range []struct {
		method string
		url    string
		code   int
	}{
		{http.MethodGet, "/github/acme/widgets/pull/42", http.StatusBadRequest},
		{http.MethodGet, "/github/acme/widgets/pull/42?sha=abc", http.StatusBadRequest},
		{http.MethodPost, "/github/acme/widgets/pull/42?sha=" + testReviewSHA, http.StatusMethodNotAllowed},
	} {
		r := httptest.NewRequest(tt.method, tt.url, nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != tt.code {
			t.Errorf("%s %s: status = %d, want %d", tt.method, tt.url, w.Code, tt.code)
		}
	}
}

func TestRootDoesNotEmbedReviewTarget(t *testing.T) {
	s, _ := newTestServer(t)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if strings.Contains(w.Body.String(), reviewTargetElementID) {
		t.Fatal("ordinary workspace route unexpectedly contains a review target")
	}
}
