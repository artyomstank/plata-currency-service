package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"

	"currency-quotes/internal/config"
	"currency-quotes/internal/provider"
	"currency-quotes/internal/storage"
	transportgrpc "currency-quotes/internal/transport/grpc"
	"currency-quotes/internal/usecase"
	"currency-quotes/internal/worker"

	quotesv1 "currency-quotes/internal/genpb/quotes/v1"
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

	pool, err := storage.NewPool(ctx, storage.PoolConfig{
		DSN:               cfg.DatabaseDSN,
		MaxConns:          cfg.DatabaseMaxConns,
		MinConns:          cfg.DatabaseMinConns,
		HealthCheckPeriod: cfg.DatabaseHealthCheckPeriod,
	})
	if err != nil {
		log.Error("db connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	jobsRepo := storage.NewJobsRepo(pool)
	quotesRepo := storage.NewQuotesRepo(pool)
	uc := usecase.New(jobsRepo, quotesRepo)

	rateProvider := provider.NewHTTPProvider(cfg.ProviderBaseURL, cfg.ProviderTimeout)
	workerConfig := worker.Config{
		PollInterval:  cfg.PollInterval,
		LeaseDuration: cfg.JobLeaseDuration,
		RetryBase:     cfg.RetryBase,
		RetryMax:      cfg.RetryMax,
		MaxAttempts:   cfg.MaxAttempts,
	}

	for i := 0; i < cfg.WorkerCount; i++ {
		w := worker.New(jobsRepo, rateProvider, workerConfig, log)
		go w.Run(ctx)
	}

	grpcServer := grpc.NewServer(grpc.UnaryInterceptor(transportgrpc.LoggingUnaryServerInterceptor(log)))
	quotesv1.RegisterQuotesServiceServer(grpcServer, transportgrpc.New(uc, log))
	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

	lis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		log.Error("listen failed", "err", err)
		os.Exit(1)
	}

	go func() {
		<-ctx.Done()
		log.Info("shutting down grpc server")
		healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
		gracefulStop(grpcServer, cfg.ShutdownTimeout)
	}()

	log.Info("grpc server started", "addr", cfg.GRPCAddr)
	if err := grpcServer.Serve(lis); err != nil {
		log.Error("serve failed", "err", err)
		os.Exit(1)
	}
}

func gracefulStop(server *grpc.Server, timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(done)
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		server.Stop()
	}
}
