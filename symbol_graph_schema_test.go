package main

import (
	"os"
	"strings"
	"testing"
)

func TestSymbolGraphSchemaIsTenantScopedAndTraversable(t *testing.T) {
	body, err := os.ReadFile("migrations/0005_symbol_graph.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(body)
	for _, want := range []string{
		"CREATE TABLE symbols (",
		"CREATE TABLE symbol_edges (",
		"installation_id bigint NOT NULL",
		"FOREIGN KEY (installation_id, repository_id, source_symbol_id)",
		"FOREIGN KEY (installation_id, repository_id, target_symbol_id)",
		"CHECK (target_symbol_id IS NOT NULL OR unresolved_target IS NOT NULL)",
		"symbol_edges_forward",
		"symbol_edges_reverse",
		"ALTER TABLE symbols FORCE ROW LEVEL SECURITY",
		"ALTER TABLE symbol_edges FORCE ROW LEVEL SECURITY",
		"current_setting('px1.installation_id', true)",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("symbol graph migration missing %q", want)
		}
	}
}

func TestSymbolGraphRollbackDropsEdgesBeforeSymbols(t *testing.T) {
	body, err := os.ReadFile("migrations/0005_symbol_graph.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(body)
	if strings.Index(sql, "DROP TABLE IF EXISTS symbol_edges") > strings.Index(sql, "DROP TABLE IF EXISTS symbols") {
		t.Fatal("rollback must drop dependent edges before symbols")
	}
}
