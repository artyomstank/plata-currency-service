package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"

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
	cfg := config.LoadServiceConfig()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := storage.NewPool(ctx, cfg.DatabaseDSN)
	if err != nil {
		log.Error("db connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	jobsRepo := storage.NewJobsRepo(pool)
	quotesRepo := storage.NewQuotesRepo(pool)
	uc := usecase.New(jobsRepo, quotesRepo)

	rateProvider := provider.NewExchangeRatesAPIProvider(cfg.ProviderBaseURL, cfg.ProviderAPIKey)

	// Несколько воркеров безопасно делят очередь через SKIP LOCKED.
	for i := 0; i < cfg.WorkerCount; i++ {
		w := worker.New(jobsRepo, quotesRepo, rateProvider, cfg.PollInterval, log)
		go w.Run(ctx)
	}

	grpcServer := grpc.NewServer()
	quotesv1.RegisterQuotesServiceServer(grpcServer, transportgrpc.New(uc))

	lis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		log.Error("listen failed", "err", err)
		os.Exit(1)
	}

	go func() {
		<-ctx.Done()
		log.Info("shutting down grpc server")
		grpcServer.GracefulStop()
	}()

	log.Info("grpc server started", "addr", cfg.GRPCAddr)
	if err := grpcServer.Serve(lis); err != nil {
		log.Error("serve failed", "err", err)
		os.Exit(1)
	}
}
