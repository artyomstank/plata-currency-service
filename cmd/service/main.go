package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"currency-quotes/internal/app"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx, log); err != nil {
		log.Error("service stopped", "err", err)
		os.Exit(1)
	}
}
