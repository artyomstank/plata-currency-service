package config

import (
	"os"
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

func TestLoadServiceConfigHTTPSettings(t *testing.T) {
	t.Setenv("DATABASE_DSN", "postgres://localhost:54322/postgres")
	t.Setenv("HTTP_ADDR", ":8081")
	t.Setenv("HTTP_REQUEST_TIMEOUT", "2s")
	t.Setenv("HTTP_READ_TIMEOUT", "4s")
	t.Setenv("HTTP_WRITE_TIMEOUT", "6s")
	t.Setenv("HTTP_IDLE_TIMEOUT", "20s")
	cfg, err := LoadServiceConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":8081" || cfg.RequestTimeout.String() != "2s" || cfg.ReadTimeout.String() != "4s" || cfg.WriteTimeout.String() != "6s" || cfg.IdleTimeout.String() != "20s" {
		t.Fatalf("unexpected HTTP settings: addr=%s request=%s read=%s write=%s idle=%s", cfg.HTTPAddr, cfg.RequestTimeout, cfg.ReadTimeout, cfg.WriteTimeout, cfg.IdleTimeout)
	}
}

func TestLoadServiceConfigRejectsInvalidHTTPSettings(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"HTTP_ADDR", " "},
		{"HTTP_REQUEST_TIMEOUT", "soon"},
		{"HTTP_READ_TIMEOUT", "0s"},
		{"HTTP_WRITE_TIMEOUT", "-1s"},
		{"HTTP_IDLE_TIMEOUT", "soon"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			t.Setenv("DATABASE_DSN", "postgres://localhost:54322/postgres")
			t.Setenv(tc.key, tc.value)
			_, err := LoadServiceConfig()
			if err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("error = %v, want %s error", err, tc.key)
			}
		})
	}
}

func TestLoadServiceConfigAllowedCurrencies(t *testing.T) {
	t.Setenv("DATABASE_DSN", "postgres://localhost:54322/postgres")
	t.Setenv("ALLOWED_CURRENCIES", " chf, jpy, CHF ")
	cfg, err := LoadServiceConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(cfg.AllowedCurrencies, ","); got != "CHF,JPY" {
		t.Fatalf("allowed currencies = %q, want CHF,JPY", got)
	}
}

func TestLoadServiceConfigRejectsInvalidCurrencies(t *testing.T) {
	for _, raw := range []string{"", "USD", "USD,usd", "USD,", "USD,US1", "USD,USDT"} {
		t.Run(raw, func(t *testing.T) {
			t.Setenv("DATABASE_DSN", "postgres://localhost:54322/postgres")
			t.Setenv("ALLOWED_CURRENCIES", raw)
			_, err := LoadServiceConfig()
			if err == nil || !strings.Contains(err.Error(), "ALLOWED_CURRENCIES") {
				t.Fatalf("currency configuration %q error = %v", raw, err)
			}
		})
	}
}

func TestLoadServiceConfigDefaultCurrencies(t *testing.T) {
	t.Setenv("DATABASE_DSN", "postgres://localhost:54322/postgres")
	t.Setenv("ALLOWED_CURRENCIES", "")
	if err := os.Unsetenv("ALLOWED_CURRENCIES"); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadServiceConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(cfg.AllowedCurrencies, ","); got != "EUR,MXN,USD" {
		t.Fatalf("default allowed currencies = %q", got)
	}
}
