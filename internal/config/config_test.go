package config

import (
	"strings"
	"testing"
)

func TestLoadServiceConfigRequiresDatabaseDSN(t *testing.T) {
	t.Setenv("DATABASE_DSN", " ")

	_, err := LoadServiceConfig()
	if err == nil || !strings.Contains(err.Error(), "DATABASE_DSN") {
		t.Fatalf("LoadServiceConfig() error = %v, want DATABASE_DSN error", err)
	}
}

func TestLoadServiceConfigRejectsInvalidDuration(t *testing.T) {
	t.Setenv("DATABASE_DSN", "postgres://localhost/test")
	t.Setenv("POLL_INTERVAL", "soon")

	_, err := LoadServiceConfig()
	if err == nil || !strings.Contains(err.Error(), "POLL_INTERVAL") {
		t.Fatalf("LoadServiceConfig() error = %v, want POLL_INTERVAL error", err)
	}
}

func TestLoadServiceConfigRejectsLeaseShorterThanProviderTimeout(t *testing.T) {
	t.Setenv("DATABASE_DSN", "postgres://localhost/test")
	t.Setenv("JOB_LEASE_DURATION", "2s")
	t.Setenv("PROVIDER_TIMEOUT", "3s")

	_, err := LoadServiceConfig()
	if err == nil || !strings.Contains(err.Error(), "JOB_LEASE_DURATION") {
		t.Fatalf("LoadServiceConfig() error = %v, want lease validation error", err)
	}
}
