package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PoolConfig struct {
	DSN               string
	MaxConns          int32
	MinConns          int32
	HealthCheckPeriod time.Duration
}

func NewPool(ctx context.Context, poolConfig PoolConfig) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(poolConfig.DSN)
	if err != nil {
		return nil, fmt.Errorf("parse database DSN: %w", err)
	}
	cfg.MaxConns = poolConfig.MaxConns
	cfg.MinConns = poolConfig.MinConns
	cfg.HealthCheckPeriod = poolConfig.HealthCheckPeriod
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}
