package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"currency-quotes/internal/config"
	"currency-quotes/internal/gateway"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := config.LoadGatewayConfig()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	handler, err := gateway.NewHandler(ctx, cfg.BackendAddr)
	if err != nil {
		log.Error("gateway init failed", "err", err)
		os.Exit(1)
	}

	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: handler}

	go func() {
		<-ctx.Done()
		log.Info("shutting down http gateway")
		_ = srv.Close()
	}()

	log.Info("http gateway started", "addr", cfg.HTTPAddr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error("serve failed", "err", err)
		os.Exit(1)
	}
}
