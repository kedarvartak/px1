package publicsite

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPlansAreValidAndMatchTheDashboardCopy(t *testing.T) {
	if err := ValidatePlans(Plans); err != nil {
		t.Fatal(err)
	}
	want, err := json.MarshalIndent(Plans, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../../../web/dashboard/lib/pricing.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(got), want) {
		t.Fatalf("web/dashboard/lib/pricing.json drifted from publicsite.Plans; regenerate it with:\n%s", want)
	}
}

func TestProStandardReviewCostCeilingHonoursTheMarginGuardrail(t *testing.T) {
	for _, plan := range Plans {
		if plan.Slug == "pro" && MaxStandardReviewCostMicrousd(plan) != 320_000 {
			t.Fatalf("Pro review cost ceiling = %d microUSD, want 320000", MaxStandardReviewCostMicrousd(plan))
		}
	}
}

func TestValidatePlansRejectsAmbiguousPricing(t *testing.T) {
	cases := map[string][]Plan{
		"empty":       nil,
		"duplicate":   {Plans[1], Plans[1]},
		"seats":       {{Slug: "pro", Name: "Pro", Audience: "Teams", MicrousdPerCredit: 1, SeatFees: true, Includes: []string{"x"}}},
		"sales price": {{Slug: "e", Name: "E", Audience: "A", MicrousdPerCredit: 1, ContactSales: true, Includes: []string{"x"}}},
		"no paid":     {Plans[0]},
	}
	for name, plans := range cases {
		if ValidatePlans(plans) == nil {
			t.Fatalf("%s: expected rejection", name)
		}
	}
}

func TestRepositoryChangelogParses(t *testing.T) {
	data, err := os.ReadFile("../../../CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := ParseChangelog(string(data))
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].PullNumber <= 0 || entries[0].Date.IsZero() {
		t.Fatalf("unexpected first entry: %#v", entries[0])
	}
}

func TestParseChangelogRejectsDrift(t *testing.T) {
	link := func(n string) string {
		return "- Thing ([#" + n + "](https://github.com/kedarvartak/px1/pull/" + n + "))"
	}
	cases := map[string]string{
		"oldest first":   "## 2026-01-01\n" + link("1") + "\n## 2026-02-01\n" + link("2"),
		"label mismatch": "## 2026-01-01\n- Thing ([#1](https://github.com/kedarvartak/px1/pull/2))",
		"foreign link":   "## 2026-01-01\n- Thing ([#1](https://example.com/pull/1))",
		"repeated":       "## 2026-02-01\n" + link("1") + "\n## 2026-01-01\n" + link("1"),
		"freeform":       "## 2026-01-01\nSomething happened",
		"empty":          "# Changelog\n",
	}
	for name, markdown := range cases {
		if _, err := ParseChangelog(markdown); err == nil {
			t.Fatalf("%s: expected rejection", name)
		}
	}
}

func TestParseStatusComponentsRequiresSafeURLs(t *testing.T) {
	components, err := ParseStatusComponents(" webhook=https://hook.px1.dev/readyz, local=http://127.0.0.1:8080/readyz ")
	if err != nil || len(components) != 2 {
		t.Fatalf("components=%v err=%v", components, err)
	}
	for _, value := range []string{"a=http://example.com/readyz", "a=https://user:pw@x.dev/readyz", "a=https://x.dev,a=https://y.dev", "=https://x.dev", "a"} {
		if _, err := ParseStatusComponents(value); err == nil {
			t.Fatalf("%q: expected rejection", value)
		}
	}
}

func TestStatusIsUnavailableWhenAnyComponentFailsAndLeaksNoDetail(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer ok.Close()
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "database password rejected for tenant 42", http.StatusServiceUnavailable)
	}))
	defer failing.Close()
	checker := StatusChecker{
		Components: []Component{{Name: "webhook", ReadyURL: failing.URL}, {Name: "api", ReadyURL: ok.URL}},
		Now:        func() time.Time { return time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC) },
	}
	recorder := httptest.NewRecorder()
	checker.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/status.json", nil))
	body := recorder.Body.String()
	if strings.Contains(body, "password") || strings.Contains(body, "127.0.0.1") || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status leaked probe detail or is cacheable: %s", body)
	}
	var status Status
	if err := json.Unmarshal([]byte(body), &status); err != nil {
		t.Fatal(err)
	}
	if status.Overall != Unavailable || status.Components[0].Name != "api" || status.Components[0].State != Operational || status.Components[1].State != Unavailable {
		t.Fatalf("unexpected status: %#v", status)
	}
}

func TestStatusWithoutComponentsNeverClaimsOperational(t *testing.T) {
	if status := (StatusChecker{}).Check(context.Background()); status.Overall != Unavailable {
		t.Fatalf("empty status = %s", status.Overall)
	}
}
