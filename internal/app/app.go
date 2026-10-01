package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"currency-quotes/internal/config"
	"currency-quotes/internal/provider/frankfurter"
	"currency-quotes/internal/repo"
	transporthttp "currency-quotes/internal/transport/http"
	"currency-quotes/internal/usecase"
	"currency-quotes/internal/worker"
)

type application struct {
	server          *http.Server
	processor       worker.JobProcessor
	log             *slog.Logger
	workerCount     int
	pollInterval    time.Duration
	shutdownTimeout time.Duration
	closeResources  func()
}

func Run(ctx context.Context, log *slog.Logger) error {
	cfg, err := config.LoadServiceConfig()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	app, err := newApplication(ctx, cfg, log)
	if err != nil {
		return err
	}
	return app.run(ctx)
}

func newApplication(ctx context.Context, cfg config.ServiceConfig, log *slog.Logger) (*application, error) {
	pool, err := repo.NewPool(ctx, repo.PoolConfig{
		DSN: cfg.DatabaseDSN, MaxConns: cfg.DatabaseMaxConns,
		MinConns: cfg.DatabaseMinConns, HealthCheckPeriod: cfg.DatabaseHealthCheckPeriod,
	})
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}
	sourceHTTP := frankfurter.NewHTTPClient(cfg.ProviderTimeout, log)
	sourceClient, err := frankfurter.NewClient(cfg.ProviderBaseURL, sourceHTTP)
	if err != nil {
		sourceHTTP.CloseIdleConnections()
		pool.Close()
		return nil, err
	}
	source := frankfurter.NewAdapter(sourceClient)
	jobs, quotes := repo.NewJobsRepo(pool), repo.NewQuotesRepo(pool)
	tx := repo.NewTransactionManager(pool)
	claim := usecase.NewClaimPending(jobs, tx, cfg.JobLeaseDuration)
	complete := usecase.NewCompleteJob(jobs, quotes, tx)
	retry := usecase.NewRetryJob(jobs, tx, usecase.RetryConfig{MaxAttempts: cfg.MaxAttempts, RetryBase: cfg.RetryBase, RetryMax: cfg.RetryMax})
	scenarios := &transporthttp.UseCases{
		RequestUpdate: usecase.NewRequestUpdate(jobs, tx, cfg.AllowedCurrencies),
		GetJobResult:  usecase.NewGetJobResult(jobs, quotes, tx),
		GetLatest:     usecase.NewGetLatest(quotes, cfg.AllowedCurrencies),
	}
	return &application{
		server: &http.Server{
			Addr: cfg.HTTPAddr, Handler: transporthttp.New(scenarios, log, pool.Ping, cfg.RequestTimeout),
			ReadHeaderTimeout: cfg.ReadTimeout, ReadTimeout: cfg.ReadTimeout,
			WriteTimeout: cfg.WriteTimeout, IdleTimeout: cfg.IdleTimeout, MaxHeaderBytes: 1 << 20,
		},
		processor: usecase.NewProcessNext(claim, complete, retry, source), log: log,
		workerCount: cfg.WorkerCount, pollInterval: cfg.PollInterval, shutdownTimeout: cfg.ShutdownTimeout,
		closeResources: func() { sourceHTTP.CloseIdleConnections(); pool.Close() },
	}, nil
}
