package config

import (
	"os"
	"time"
)

type ServiceConfig struct {
	GRPCAddr        string
	DatabaseDSN     string
	PollInterval    time.Duration
	WorkerCount     int
	ProviderBaseURL string
	ProviderAPIKey  string
}

type GatewayConfig struct {
	HTTPAddr    string
	BackendAddr string
}

func LoadServiceConfig() ServiceConfig {
	return ServiceConfig{
		GRPCAddr:        getEnv("GRPC_ADDR", ":9090"),
		DatabaseDSN:     getEnv("DATABASE_DSN", "postgres://postgres:postgres@localhost:5432/quotes?sslmode=disable"),
		PollInterval:    getDurationEnv("POLL_INTERVAL", 2*time.Second),
		WorkerCount:     3,
		ProviderBaseURL: getEnv("PROVIDER_BASE_URL", "https://api.exchangeratesapi.io/v1"),
		ProviderAPIKey:  getEnv("PROVIDER_API_KEY", ""),
	}
}

func LoadGatewayConfig() GatewayConfig {
	return GatewayConfig{
		HTTPAddr:    getEnv("HTTP_ADDR", ":8080"),
		BackendAddr: getEnv("BACKEND_ADDR", "service:9090"),
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getDurationEnv(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
