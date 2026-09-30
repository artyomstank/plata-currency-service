package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"currency-quotes/internal/domain"
)

type ServiceConfig struct {
	HTTPAddr                  string
	AllowedCurrencies         []string
	RequestTimeout            time.Duration
	ReadTimeout               time.Duration
	WriteTimeout              time.Duration
	IdleTimeout               time.Duration
	DatabaseDSN               string
	DatabaseMaxConns          int32
	DatabaseMinConns          int32
	DatabaseHealthCheckPeriod time.Duration
	PollInterval              time.Duration
	WorkerCount               int
	JobLeaseDuration          time.Duration
	RetryBase                 time.Duration
	RetryMax                  time.Duration
	MaxAttempts               int
	ProviderBaseURL           string
	ProviderTimeout           time.Duration
	ShutdownTimeout           time.Duration
}

func LoadServiceConfig() (ServiceConfig, error) {
	cfg := ServiceConfig{
		HTTPAddr:                  env("HTTP_ADDR", ":8080"),
		RequestTimeout:            3 * time.Second,
		ReadTimeout:               5 * time.Second,
		WriteTimeout:              10 * time.Second,
		IdleTimeout:               60 * time.Second,
		DatabaseDSN:               env("DATABASE_DSN", ""),
		DatabaseMaxConns:          10,
		DatabaseMinConns:          1,
		DatabaseHealthCheckPeriod: 30 * time.Second,
		PollInterval:              500 * time.Millisecond,
		WorkerCount:               3,
		JobLeaseDuration:          30 * time.Second,
		RetryBase:                 time.Second,
		RetryMax:                  30 * time.Second,
		MaxAttempts:               5,
		ProviderBaseURL:           env("PROVIDER_BASE_URL", "https://api.frankfurter.app"),
		ProviderTimeout:           5 * time.Second,
		ShutdownTimeout:           10 * time.Second,
	}

	var err error
	cfg.AllowedCurrencies, err = domain.NormalizeCurrencies(strings.Split(env("ALLOWED_CURRENCIES", "EUR,MXN,USD"), ","))
	if err != nil {
		return ServiceConfig{}, fmt.Errorf("ALLOWED_CURRENCIES: %w", err)
	}
	if cfg.RequestTimeout, err = durationEnv("HTTP_REQUEST_TIMEOUT", cfg.RequestTimeout); err != nil {
		return ServiceConfig{}, err
	}
	if cfg.ReadTimeout, err = durationEnv("HTTP_READ_TIMEOUT", cfg.ReadTimeout); err != nil {
		return ServiceConfig{}, err
	}
	if cfg.WriteTimeout, err = durationEnv("HTTP_WRITE_TIMEOUT", cfg.WriteTimeout); err != nil {
		return ServiceConfig{}, err
	}
	if cfg.IdleTimeout, err = durationEnv("HTTP_IDLE_TIMEOUT", cfg.IdleTimeout); err != nil {
		return ServiceConfig{}, err
	}
	if cfg.DatabaseMaxConns, err = int32Env("DATABASE_MAX_CONNS", cfg.DatabaseMaxConns); err != nil {
		return ServiceConfig{}, err
	}
	if cfg.DatabaseMinConns, err = int32Env("DATABASE_MIN_CONNS", cfg.DatabaseMinConns); err != nil {
		return ServiceConfig{}, err
	}
	if cfg.DatabaseHealthCheckPeriod, err = durationEnv("DATABASE_HEALTH_CHECK_PERIOD", cfg.DatabaseHealthCheckPeriod); err != nil {
		return ServiceConfig{}, err
	}
	if cfg.PollInterval, err = durationEnv("POLL_INTERVAL", cfg.PollInterval); err != nil {
		return ServiceConfig{}, err
	}
	if cfg.WorkerCount, err = intEnv("WORKER_COUNT", cfg.WorkerCount); err != nil {
		return ServiceConfig{}, err
	}
	if cfg.JobLeaseDuration, err = durationEnv("JOB_LEASE_DURATION", cfg.JobLeaseDuration); err != nil {
		return ServiceConfig{}, err
	}
	if cfg.RetryBase, err = durationEnv("RETRY_BASE", cfg.RetryBase); err != nil {
		return ServiceConfig{}, err
	}
	if cfg.RetryMax, err = durationEnv("RETRY_MAX", cfg.RetryMax); err != nil {
		return ServiceConfig{}, err
	}
	if cfg.MaxAttempts, err = intEnv("MAX_ATTEMPTS", cfg.MaxAttempts); err != nil {
		return ServiceConfig{}, err
	}
	if cfg.ProviderTimeout, err = durationEnv("PROVIDER_TIMEOUT", cfg.ProviderTimeout); err != nil {
		return ServiceConfig{}, err
	}
	if cfg.ShutdownTimeout, err = durationEnv("SHUTDOWN_TIMEOUT", cfg.ShutdownTimeout); err != nil {
		return ServiceConfig{}, err
	}

	if cfg.HTTPAddr == "" {
		return ServiceConfig{}, fmt.Errorf("HTTP_ADDR must not be empty")
	}
	if cfg.DatabaseDSN == "" {
		return ServiceConfig{}, fmt.Errorf("DATABASE_DSN must not be empty")
	}
	if cfg.DatabaseMinConns > cfg.DatabaseMaxConns {
		return ServiceConfig{}, fmt.Errorf("DATABASE_MIN_CONNS must not exceed DATABASE_MAX_CONNS")
	}
	if cfg.WorkerCount < 1 {
		return ServiceConfig{}, fmt.Errorf("WORKER_COUNT must be at least 1")
	}
	if cfg.MaxAttempts < 1 {
		return ServiceConfig{}, fmt.Errorf("MAX_ATTEMPTS must be at least 1")
	}
	if cfg.RetryBase > cfg.RetryMax {
		return ServiceConfig{}, fmt.Errorf("RETRY_BASE must not exceed RETRY_MAX")
	}
	if cfg.JobLeaseDuration <= cfg.ProviderTimeout {
		return ServiceConfig{}, fmt.Errorf("JOB_LEASE_DURATION must exceed PROVIDER_TIMEOUT")
	}
	if err := validateHTTPURL(cfg.ProviderBaseURL); err != nil {
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
