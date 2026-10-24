package main

import (
	"os"
	"strings"
	"testing"
)

func TestEmbeddingMigrationPersistsModelVersion(t *testing.T) {
	body, err := os.ReadFile("migrations/0002_embedding_model_version.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "embedding_model_version text NOT NULL") {
		t.Fatal("embedding migration must persist a non-null model version")
	}
}
