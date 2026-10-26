package main

import (
	"os"
	"strings"
	"testing"
)

func TestAnalyticsSchemaIsTenantScopedAndIdempotent(t *testing.T) {
	body, err := os.ReadFile("migrations/0010_analytics_events.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"UNIQUE (installation_id, source_id)", "PRIMARY KEY (installation_id, repository_id, metric_day, team, author_login)", "FORCE ROW LEVEL SECURITY", "current_setting('px1.installation_id', true)"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}
