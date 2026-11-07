package main

import (
	"os"
	"strings"
	"testing"
)

func TestAPIKeysSchemaStoresOnlyDigestsBehindTheTenantBoundary(t *testing.T) {
	body, err := os.ReadFile("migrations/0018_api_keys.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	schema := string(body)
	for _, want := range []string{"PRIMARY KEY (installation_id, id)", "secret_sha256 bytea NOT NULL CHECK (length(secret_sha256) = 32)", "FORCE ROW LEVEL SECURITY", "current_setting('px1.installation_id', true)", "REFERENCES api_keys(installation_id, id)"} {
		if !strings.Contains(schema, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
	if strings.Contains(schema, "secret text") || strings.Contains(schema, "token text") {
		t.Fatal("API key schema must not store plaintext secrets")
	}
}
