package main

import (
	"os"
	"strings"
	"testing"
)

func TestDashboardSessionsStoreOnlyHashedCredentials(t *testing.T) {
	body, err := os.ReadFile("migrations/0009_dashboard_sessions.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"provider IN ('github', 'gitlab')", "token_hash bytea PRIMARY KEY", "csrf_hash bytea NOT NULL", "REFERENCES dashboard_memberships(user_id, installation_id)"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("migration missing %q", want)
		}
	}
	if strings.Contains(string(body), "access_token") || strings.Contains(string(body), "refresh_token") {
		t.Fatal("migration persists OAuth bearer tokens")
	}
}
