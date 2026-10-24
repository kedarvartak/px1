package runtime

import (
	"fmt"
	"os"
	"strings"
)

var roles = map[string]bool{
	"web": true, "webhook": true, "indexer": true, "reviewer": true, "learner": true,
}

type Config struct {
	Environment    string
	Role           string
	Address        string
	Release        string
	DatabaseURL    string
	ObjectStoreURL string
	OTLPEndpoint   string
	SentryDSN      string
}

func LoadConfig(roleOverride string) (Config, error) {
	role := first(roleOverride, os.Getenv("PX1_ROLE"), "web")
	cfg := Config{
		Environment:    first(os.Getenv("PX1_ENVIRONMENT"), "local"),
		Role:           role,
		Address:        first(os.Getenv("PX1_ADDRESS"), ":8080"),
		Release:        os.Getenv("PX1_RELEASE"),
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		ObjectStoreURL: os.Getenv("PX1_OBJECT_STORE_URL"),
		OTLPEndpoint:   os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		SentryDSN:      os.Getenv("SENTRY_DSN"),
	}
	if !roles[cfg.Role] {
		return Config{}, fmt.Errorf("unsupported PX1 role %q", cfg.Role)
	}
	if cfg.Environment != "local" && cfg.Environment != "staging" && cfg.Environment != "production" {
		return Config{}, fmt.Errorf("unsupported PX1 environment %q", cfg.Environment)
	}
	if cfg.Environment != "local" {
		var missing []string
		if cfg.DatabaseURL == "" {
			missing = append(missing, "DATABASE_URL")
		}
		if cfg.ObjectStoreURL == "" {
			missing = append(missing, "PX1_OBJECT_STORE_URL")
		}
		if cfg.OTLPEndpoint == "" {
			missing = append(missing, "OTEL_EXPORTER_OTLP_ENDPOINT")
		}
		if cfg.SentryDSN == "" {
			missing = append(missing, "SENTRY_DSN")
		}
		if len(missing) > 0 {
			return Config{}, fmt.Errorf("hosted environment is missing %s", strings.Join(missing, ", "))
		}
	}
	return cfg, nil
}

func (c Config) PublicAttributes() map[string]string {
	return map[string]string{"environment": c.Environment, "role": c.Role, "release": c.Release}
}

func first(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
