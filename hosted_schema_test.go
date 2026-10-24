package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestHostedSchemaDefinesCoreTenantScopedTables(t *testing.T) {
	body, err := os.ReadFile("migrations/0001_hosted_core.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(body)
	for _, table := range []string{
		"installations", "repositories", "files", "chunks", "pull_requests",
		"reviews", "findings", "comments", "feedback_events", "jobs",
	} {
		if !strings.Contains(sql, "CREATE TABLE "+table+" (") {
			t.Errorf("missing %s table", table)
		}
	}
	for _, required := range []string{
		"CREATE EXTENSION IF NOT EXISTS vector",
		"embedding vector(1536) NOT NULL",
		"USING hnsw (embedding vector_cosine_ops)",
		"ENABLE ROW LEVEL SECURITY",
		"FORCE ROW LEVEL SECURITY",
		"current_setting('px1.installation_id', true)",
		"FOREIGN KEY (installation_id, repository_id) REFERENCES repositories(installation_id, id)",
		"FOREIGN KEY (installation_id, repository_id, pull_request_id) REFERENCES pull_requests(installation_id, repository_id, id)",
		"FOREIGN KEY (installation_id, repository_id, review_id) REFERENCES reviews(installation_id, repository_id, id)",
		"FOREIGN KEY (installation_id, repository_id, finding_id) REFERENCES findings(installation_id, repository_id, id)",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("schema missing %q", required)
		}
	}
}

func TestEveryHostedChildTableCarriesInstallationID(t *testing.T) {
	body, err := os.ReadFile("migrations/0001_hosted_core.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(body)
	for _, table := range []string{
		"repositories", "files", "chunks", "pull_requests", "reviews",
		"findings", "comments", "feedback_events", "jobs",
	} {
		pattern := regexp.MustCompile(`(?s)CREATE TABLE ` + regexp.QuoteMeta(table) + ` \((.*?)\);`)
		match := pattern.FindStringSubmatch(sql)
		if len(match) != 2 || !strings.Contains(match[1], "installation_id bigint") {
			t.Errorf("%s must carry installation_id", table)
		}
	}
}
