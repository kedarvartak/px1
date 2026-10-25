package main

import (
	"os"
	"strings"
	"testing"
)

func TestMemorySchemaIsEvidenceBackedAndTenantScoped(t *testing.T) {
	body, err := os.ReadFile("migrations/0007_memories.up.sql")
	if err != nil { t.Fatal(err) }
	sql := string(body)
	for _, want := range []string{"CREATE TABLE memories", "installation_id bigint NOT NULL", "repository_id bigint", "evidence jsonb NOT NULL", "embedding vector(1536)", "weight real", "last_confirmed_at", "FORCE ROW LEVEL SECURITY", "current_setting('px1.installation_id', true)"} {
		if !strings.Contains(sql, want) { t.Errorf("memory migration missing %q", want) }
	}
}
