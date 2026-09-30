// Package worker polls application scenarios without accessing infrastructure.
package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"currency-quotes/internal/domain"
)

type JobProcessor interface {
	ProcessNext(context.Context) (bool, error)
}

type Config struct{ PollInterval time.Duration }

type Worker struct {
	processor JobProcessor
	config    Config
	log       *slog.Logger
}

func New(processor JobProcessor, config Config, log *slog.Logger) *Worker {
	return &Worker{processor: processor, config: config, log: log}
}

// Run polls until cancellation, with one immediate poll at startup.
func (w *Worker) Run(ctx context.Context) {
	w.ProcessOne(ctx)
	ticker := time.NewTicker(w.config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.ProcessOne(ctx)
		}
	}
}

func (w *Worker) ProcessOne(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	claimed, err := w.processor.ProcessNext(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return claimed
		}
		if errors.Is(err, domain.ErrClaimLost) {
			w.log.WarnContext(ctx, "job claim lost", "err", err)
		} else {
			w.log.ErrorContext(ctx, "process quote job failed", "claimed", claimed, "err", err)
		}
	}
	return claimed
}
