package runtime

import "testing"

func TestHostedConfigRequiresOperationalEndpoints(t *testing.T) {
	t.Setenv("PX1_ENVIRONMENT", "staging")
	t.Setenv("DATABASE_URL", "")
	if _, err := LoadConfig("reviewer"); err == nil {
		t.Fatal("expected missing hosted configuration to fail")
	}
	for key, value := range map[string]string{
		"DATABASE_URL": "postgres://example", "PX1_OBJECT_STORE_URL": "s3://bucket",
		"OTEL_EXPORTER_OTLP_ENDPOINT": "https://otel.example", "SENTRY_DSN": "https://public@sentry.example/1",
	} {
		t.Setenv(key, value)
	}
	cfg, err := LoadConfig("reviewer")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PublicAttributes()["role"] != "reviewer" {
		t.Fatalf("public attributes = %#v", cfg.PublicAttributes())
	}
}

func TestConfigRejectsUnknownRole(t *testing.T) {
	if _, err := LoadConfig("executor"); err == nil {
		t.Fatal("expected unknown role to fail")
	}
}
