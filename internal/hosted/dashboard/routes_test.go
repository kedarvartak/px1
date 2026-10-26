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
	for _, required := range []string{"overview", "repositories", "pull requests", "rules", "memories", "analytics", "billing", "members", "settings"} {
		if !seenNames[required] {
			t.Fatalf("missing route %q", required)
		}
	}
}
