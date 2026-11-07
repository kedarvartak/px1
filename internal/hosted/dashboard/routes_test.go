package dashboard

import (
	"strings"
	"testing"
)

func TestDashboardRoutesAreOrganizationScopedAndUnique(t *testing.T) {
	seenNames, seenPatterns := map[string]bool{}, map[string]bool{}
	for _, route := range Routes {
		if route.Name == "" || !strings.HasPrefix(route.Pattern, "/:organization") || seenNames[route.Name] || seenPatterns[route.Pattern] {
			t.Fatalf("invalid route: %#v", route)
		}
		seenNames[route.Name], seenPatterns[route.Pattern] = true, true
	}
	for _, required := range []string{"overview", "repositories", "repository settings", "pull requests", "rules", "memories", "analytics", "billing", "members", "settings"} {
		if !seenNames[required] {
			t.Fatalf("missing route %q", required)
		}
	}
}

func TestPublicRoutesCannotShadowAnOrganization(t *testing.T) {
	seen := map[string]bool{}
	for _, route := range PublicRoutes {
		rest, ok := strings.CutPrefix(route.Pattern, PublicPrefix)
		if !ok || rest == "" || strings.ContainsAny(rest, ":/") || route.Navigation || seen[route.Pattern] {
			t.Fatalf("invalid public route: %#v", route)
		}
		seen[route.Pattern] = true
	}
	for _, required := range []string{"/-/pricing", "/-/docs", "/-/changelog", "/-/status"} {
		if !seen[required] {
			t.Fatalf("missing public route %q", required)
		}
	}
}
