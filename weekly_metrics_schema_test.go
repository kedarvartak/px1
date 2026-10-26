package main

import (
	"os"
	"strings"
	"testing"
)

func TestWeeklyMetricsSchemaPreservesTenantBoundary(t *testing.T) {
	body, err := os.ReadFile("migrations/0008_weekly_repository_metrics.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"PRIMARY KEY (installation_id, repository_id, week_start)", "REFERENCES repositories(installation_id, id)", "FORCE ROW LEVEL SECURITY", "current_setting('px1.installation_id', true)"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}
