package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"currency-quotes/internal/domain"
)

type JobProcessor interface {
	Execute(context.Context) (bool, error)
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

func (w *Worker) Run(ctx context.Context, stop <-chan struct{}) {
	ticker := time.NewTicker(w.config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		default:
		}
		w.ProcessOne(ctx)
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *Worker) ProcessOne(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	claimed, err := w.processor.Execute(ctx)
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
