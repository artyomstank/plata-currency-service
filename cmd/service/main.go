package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"currency-quotes/internal/config"
	"currency-quotes/internal/provider"
	"currency-quotes/internal/storage"
	transporthttp "currency-quotes/internal/transport/http"
	"currency-quotes/internal/usecase"
	"currency-quotes/internal/worker"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.LoadServiceConfig()
	if err != nil {
		log.Error("invalid configuration", "err", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, cfg, log); err != nil {
		log.Error("service stopped", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg config.ServiceConfig, log *slog.Logger) error {
	pool, err := storage.NewPool(ctx, storage.PoolConfig{
		DSN: cfg.DatabaseDSN, MaxConns: cfg.DatabaseMaxConns,
		MinConns: cfg.DatabaseMinConns, HealthCheckPeriod: cfg.DatabaseHealthCheckPeriod,
	})
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer pool.Close()

	jobs := storage.NewJobsRepo(pool)
	rateProvider := provider.NewHTTPProvider(cfg.ProviderBaseURL, cfg.ProviderTimeout)
	uc := usecase.New(jobs, storage.NewQuotesRepo(pool), storage.NewTransactionManager(pool), rateProvider, usecase.Config{
		AllowedCurrencies: cfg.AllowedCurrencies, LeaseDuration: cfg.JobLeaseDuration,
		MaxAttempts: cfg.MaxAttempts, RetryBase: cfg.RetryBase, RetryMax: cfg.RetryMax,
	})
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           transporthttp.New(uc, log, pool.Ping, cfg.RequestTimeout),
		ReadHeaderTimeout: cfg.ReadTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		MaxHeaderBytes:    1 << 20,
	}
	listener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen HTTP: %w", err)
	}

	workerCtx, stopWorkers := context.WithCancel(ctx)
	defer stopWorkers()
	var workers sync.WaitGroup
	workerConfig := worker.Config{PollInterval: cfg.PollInterval}
	for range cfg.WorkerCount {
		w := worker.New(uc, workerConfig, log)
		workers.Go(func() { w.Run(workerCtx) })
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(listener) }()
	log.Info("HTTP service started", "addr", cfg.HTTPAddr)
	select {
	case <-ctx.Done():
	case err = <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
	}

	log.Info("shutting down HTTP service")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if shutdownErr := srv.Shutdown(shutdownCtx); shutdownErr != nil {
		err = errors.Join(err, fmt.Errorf("shutdown HTTP: %w", shutdownErr))
		if closeErr := srv.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close HTTP: %w", closeErr))
		}
	}
	stopWorkers()
	done := make(chan struct{})
	go func() {
		workers.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-shutdownCtx.Done():
		err = errors.Join(err, fmt.Errorf("stop workers: %w", shutdownCtx.Err()))
	}
	return err
}
