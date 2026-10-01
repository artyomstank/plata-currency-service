package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"currency-quotes/internal/domain"
	"currency-quotes/internal/provider/frankfurter"
	transporthttp "currency-quotes/internal/transport/http"
	"currency-quotes/internal/usecase"
	"currency-quotes/internal/worker"
	"currency-quotes/pkg/httpclient"
	"currency-quotes/pkg/httpserver"
	"currency-quotes/pkg/postgres"
)

type ServiceConfig struct {
	Postgres        postgres.PoolConfig
	HTTPServer      httpserver.Config
	HTTPTransport   transporthttp.Config
	Frankfurter     frankfurter.ClientConfig
	FrankfurterHTTP httpclient.Config
	Worker          worker.Config
	Currencies      usecase.CurrencyConfig
	ClaimPending    usecase.ClaimPendingConfig
	RetryJob        usecase.RetryConfig
	Runtime         RuntimeConfig
}

type RuntimeConfig struct {
	WorkerCount     int
	ShutdownTimeout time.Duration
}

func LoadServiceConfig() (ServiceConfig, error) {
	cfg := ServiceConfig{
		Postgres: postgres.PoolConfig{
			DSN: env("DATABASE_DSN", ""), MaxConns: 10, MinConns: 1,
			HealthCheckPeriod: 30 * time.Second,
		},
		HTTPServer: httpserver.Config{
			Addr: env("HTTP_ADDR", ":8080"), ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second,
			IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20,
		},
		HTTPTransport:   transporthttp.Config{RequestTimeout: 3 * time.Second, MaxBodyBytes: 1 << 20},
		Frankfurter:     frankfurter.ClientConfig{BaseURL: env("PROVIDER_BASE_URL", "https://api.frankfurter.app")},
		FrankfurterHTTP: httpclient.Config{Timeout: 5 * time.Second},
		Worker:          worker.Config{PollInterval: 500 * time.Millisecond},
		ClaimPending:    usecase.ClaimPendingConfig{LeaseDuration: 30 * time.Second},
		RetryJob:        usecase.RetryConfig{MaxAttempts: 5, RetryBase: time.Second, RetryMax: 30 * time.Second},
		Runtime:         RuntimeConfig{WorkerCount: 3, ShutdownTimeout: 10 * time.Second},
	}

	var err error
	cfg.Currencies.AllowedCurrencies, err = domain.NormalizeCurrencies(strings.Split(env("ALLOWED_CURRENCIES", "EUR,MXN,USD"), ","))
	if err != nil {
		return ServiceConfig{}, fmt.Errorf("ALLOWED_CURRENCIES: %w", err)
	}
	if cfg.HTTPTransport.RequestTimeout, err = durationEnv("HTTP_REQUEST_TIMEOUT", cfg.HTTPTransport.RequestTimeout); err != nil {
		return ServiceConfig{}, err
	}
	if cfg.HTTPServer.ReadTimeout, err = durationEnv("HTTP_READ_TIMEOUT", cfg.HTTPServer.ReadTimeout); err != nil {
		return ServiceConfig{}, err
	}
	cfg.HTTPServer.ReadHeaderTimeout = cfg.HTTPServer.ReadTimeout
	if cfg.HTTPServer.WriteTimeout, err = durationEnv("HTTP_WRITE_TIMEOUT", cfg.HTTPServer.WriteTimeout); err != nil {
		return ServiceConfig{}, err
	}
	if cfg.HTTPServer.IdleTimeout, err = durationEnv("HTTP_IDLE_TIMEOUT", cfg.HTTPServer.IdleTimeout); err != nil {
		return ServiceConfig{}, err
	}
	if cfg.Postgres.MaxConns, err = int32Env("DATABASE_MAX_CONNS", cfg.Postgres.MaxConns); err != nil {
		return ServiceConfig{}, err
	}
	if cfg.Postgres.MinConns, err = int32Env("DATABASE_MIN_CONNS", cfg.Postgres.MinConns); err != nil {
		return ServiceConfig{}, err
	}
	if cfg.Postgres.HealthCheckPeriod, err = durationEnv("DATABASE_HEALTH_CHECK_PERIOD", cfg.Postgres.HealthCheckPeriod); err != nil {
		return ServiceConfig{}, err
	}
	if cfg.Worker.PollInterval, err = durationEnv("POLL_INTERVAL", cfg.Worker.PollInterval); err != nil {
		return ServiceConfig{}, err
	}
	if cfg.Runtime.WorkerCount, err = intEnv("WORKER_COUNT", cfg.Runtime.WorkerCount); err != nil {
		return ServiceConfig{}, err
	}
	if cfg.ClaimPending.LeaseDuration, err = durationEnv("JOB_LEASE_DURATION", cfg.ClaimPending.LeaseDuration); err != nil {
		return ServiceConfig{}, err
	}
	if cfg.RetryJob.RetryBase, err = durationEnv("RETRY_BASE", cfg.RetryJob.RetryBase); err != nil {
		return ServiceConfig{}, err
	}
	if cfg.RetryJob.RetryMax, err = durationEnv("RETRY_MAX", cfg.RetryJob.RetryMax); err != nil {
		return ServiceConfig{}, err
	}
	if cfg.RetryJob.MaxAttempts, err = intEnv("MAX_ATTEMPTS", cfg.RetryJob.MaxAttempts); err != nil {
		return ServiceConfig{}, err
	}
	if cfg.FrankfurterHTTP.Timeout, err = durationEnv("PROVIDER_TIMEOUT", cfg.FrankfurterHTTP.Timeout); err != nil {
		return ServiceConfig{}, err
	}
	if cfg.Runtime.ShutdownTimeout, err = durationEnv("SHUTDOWN_TIMEOUT", cfg.Runtime.ShutdownTimeout); err != nil {
		return ServiceConfig{}, err
	}

	if cfg.HTTPServer.Addr == "" {
		return ServiceConfig{}, fmt.Errorf("HTTP_ADDR must not be empty")
	}
	if cfg.Postgres.DSN == "" {
		return ServiceConfig{}, fmt.Errorf("DATABASE_DSN must not be empty")
	}
	if cfg.Postgres.MinConns > cfg.Postgres.MaxConns {
		return ServiceConfig{}, fmt.Errorf("DATABASE_MIN_CONNS must not exceed DATABASE_MAX_CONNS")
	}
	if cfg.Runtime.WorkerCount < 1 {
		return ServiceConfig{}, fmt.Errorf("WORKER_COUNT must be at least 1")
	}
	if cfg.RetryJob.MaxAttempts < 1 {
		return ServiceConfig{}, fmt.Errorf("MAX_ATTEMPTS must be at least 1")
	}
	if cfg.RetryJob.RetryBase > cfg.RetryJob.RetryMax {
		return ServiceConfig{}, fmt.Errorf("RETRY_BASE must not exceed RETRY_MAX")
	}
	if cfg.ClaimPending.LeaseDuration <= cfg.FrankfurterHTTP.Timeout {
		return ServiceConfig{}, fmt.Errorf("JOB_LEASE_DURATION must exceed PROVIDER_TIMEOUT")
	}
	if err := validateHTTPURL(cfg.Frankfurter.BaseURL); err != nil {
		return ServiceConfig{}, fmt.Errorf("PROVIDER_BASE_URL: %w", err)
	}
	return cfg, nil
}

func env(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return strings.TrimSpace(value)
	}
	return fallback
}

func intEnv(key string, fallback int) (int, error) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return parsed, nil
}

func int32Env(key string, fallback int32) (int32, error) {
	value, err := intEnv(key, int(fallback))
	if err != nil {
		return 0, err
	}
	const maxInt32 = int64(1<<31 - 1)
	if value < 1 || int64(value) > maxInt32 {
		return 0, fmt.Errorf("%s must be between 1 and %d", key, maxInt32)
	}
	return int32(value), nil
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration: %w", key, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be positive", key)
	}
	return parsed, nil
}

func validateHTTPURL(raw string) error {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("scheme must be http or https")
	}
	if parsed.Host == "" {
		return fmt.Errorf("host must not be empty")
	}
	return nil
}
