package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"currency-quotes/internal/config"
	"currency-quotes/internal/provider/frankfurter"
	jobrepo "currency-quotes/internal/repo/postgres/job"
	quoterepo "currency-quotes/internal/repo/postgres/quote"
	transporthttp "currency-quotes/internal/transport/http"
	httphandler "currency-quotes/internal/transport/http/handler"
	"currency-quotes/internal/usecase"
	"currency-quotes/internal/worker"
	"currency-quotes/pkg/httpclient"
	"currency-quotes/pkg/httpserver"
	"currency-quotes/pkg/postgres"
)

type application struct {
	server          *http.Server
	processor       worker.JobProcessor
	log             *slog.Logger
	workerCount     int
	workerConfig    worker.Config
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
	pool, err := postgres.NewPool(ctx, cfg.Postgres)
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}
	sourceHTTP := httpclient.New(cfg.FrankfurterHTTP,
		httpclient.Logging(log, "Frankfurter HTTP request"),
		httpclient.Headers(http.Header{"Accept": {"application/json"}, "User-Agent": {"currency-service/frankfurter"}}),
	)
	sourceClient, err := frankfurter.NewClient(cfg.Frankfurter, sourceHTTP)
	if err != nil {
		sourceHTTP.CloseIdleConnections()
		pool.Close()
		return nil, err
	}
	source := frankfurter.NewAdapter(sourceClient)
	jobs, quotes := jobrepo.New(pool), quoterepo.New(pool)
	tx := postgres.NewTransactionManager(pool)
	claim := usecase.NewClaimPending(jobs, tx, cfg.ClaimPending)
	complete := usecase.NewCompleteJob(jobs, quotes, tx)
	retry := usecase.NewRetryJob(jobs, tx, cfg.RetryJob)
	scenarios := &httphandler.UseCases{
		RequestUpdate: usecase.NewRequestUpdate(jobs, tx, cfg.Currencies),
		GetJobResult:  usecase.NewGetJobResult(jobs, quotes, tx),
		GetLatest:     usecase.NewGetLatest(quotes, cfg.Currencies),
	}
	return &application{
		server:    httpserver.New(cfg.HTTPServer, transporthttp.New(scenarios, log, pool.Ping, cfg.HTTPTransport)),
		processor: usecase.NewProcessNext(claim, complete, retry, source), log: log,
		workerCount: cfg.Runtime.WorkerCount, workerConfig: cfg.Worker, shutdownTimeout: cfg.Runtime.ShutdownTimeout,
		closeResources: func() { sourceHTTP.CloseIdleConnections(); pool.Close() },
	}, nil
}
