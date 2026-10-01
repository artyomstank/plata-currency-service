package config

import (
	"os"
	"strings"
	"testing"
	"time"
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
	if cfg.HTTPServer.Addr != ":8081" || cfg.HTTPTransport.RequestTimeout.String() != "2s" || cfg.HTTPServer.ReadTimeout.String() != "4s" || cfg.HTTPServer.WriteTimeout.String() != "6s" || cfg.HTTPServer.IdleTimeout.String() != "20s" {
		t.Fatalf("unexpected HTTP settings: addr=%s request=%s read=%s write=%s idle=%s", cfg.HTTPServer.Addr, cfg.HTTPTransport.RequestTimeout, cfg.HTTPServer.ReadTimeout, cfg.HTTPServer.WriteTimeout, cfg.HTTPServer.IdleTimeout)
	}
	if cfg.HTTPServer.ReadHeaderTimeout != cfg.HTTPServer.ReadTimeout {
		t.Errorf("read header timeout = %s, want %s", cfg.HTTPServer.ReadHeaderTimeout, cfg.HTTPServer.ReadTimeout)
	}
	if cfg.HTTPTransport.MaxBodyBytes != 1<<20 {
		t.Errorf("max body bytes = %d, want %d", cfg.HTTPTransport.MaxBodyBytes, 1<<20)
	}
	if cfg.HTTPServer.MaxHeaderBytes != 1<<20 {
		t.Errorf("max header bytes = %d, want %d", cfg.HTTPServer.MaxHeaderBytes, 1<<20)
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
	if got := strings.Join(cfg.Currencies.AllowedCurrencies, ","); got != "CHF,JPY" {
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
	if got := strings.Join(cfg.Currencies.AllowedCurrencies, ","); got != "EUR,MXN,USD" {
		t.Fatalf("default allowed currencies = %q", got)
	}
}

func TestLoadServiceConfigComponentSettings(t *testing.T) {
	for key, value := range map[string]string{
		"DATABASE_DSN":                 "postgres://localhost:54322/postgres",
		"DATABASE_MAX_CONNS":           "7",
		"DATABASE_MIN_CONNS":           "2",
		"DATABASE_HEALTH_CHECK_PERIOD": "11s",
		"PROVIDER_BASE_URL":            "http://rates.test/api",
		"PROVIDER_TIMEOUT":             "4s",
		"POLL_INTERVAL":                "125ms",
		"WORKER_COUNT":                 "2",
		"JOB_LEASE_DURATION":           "9s",
		"RETRY_BASE":                   "2s",
		"RETRY_MAX":                    "7s",
		"MAX_ATTEMPTS":                 "3",
		"SHUTDOWN_TIMEOUT":             "15s",
	} {
		t.Setenv(key, value)
	}
	cfg, err := LoadServiceConfig()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		got, want any
	}{
		{"Postgres.DSN", cfg.Postgres.DSN, "postgres://localhost:54322/postgres"},
		{"Postgres.MaxConns", cfg.Postgres.MaxConns, int32(7)},
		{"Postgres.MinConns", cfg.Postgres.MinConns, int32(2)},
		{"Postgres.HealthCheckPeriod", cfg.Postgres.HealthCheckPeriod, 11 * time.Second},
		{"Frankfurter.BaseURL", cfg.Frankfurter.BaseURL, "http://rates.test/api"},
		{"FrankfurterHTTP.Timeout", cfg.FrankfurterHTTP.Timeout, 4 * time.Second},
		{"Worker.PollInterval", cfg.Worker.PollInterval, 125 * time.Millisecond},
		{"ClaimPending.LeaseDuration", cfg.ClaimPending.LeaseDuration, 9 * time.Second},
		{"RetryJob.RetryBase", cfg.RetryJob.RetryBase, 2 * time.Second},
		{"RetryJob.RetryMax", cfg.RetryJob.RetryMax, 7 * time.Second},
		{"RetryJob.MaxAttempts", cfg.RetryJob.MaxAttempts, 3},
		{"Runtime.WorkerCount", cfg.Runtime.WorkerCount, 2},
		{"Runtime.ShutdownTimeout", cfg.Runtime.ShutdownTimeout, 15 * time.Second},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}
