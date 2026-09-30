// Package worker implements background processing of quote update jobs.
package worker

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"currency-quotes/internal/domain"
	"currency-quotes/internal/provider"
)

const publicProviderError = "quote provider is temporarily unavailable"

type Queue interface {
	ClaimNextPending(ctx context.Context, leaseDuration time.Duration) (*domain.Job, error)
	Complete(ctx context.Context, id domain.JobID, leaseToken uuid.UUID, value domain.QuoteValue) error
	RetryOrFail(
		ctx context.Context,
		id domain.JobID,
		leaseToken uuid.UUID,
		attempts, maxAttempts int,
		nextAttemptAt time.Time,
		publicError string,
	) error
}

type Config struct {
	PollInterval  time.Duration
	LeaseDuration time.Duration
	RetryBase     time.Duration
	RetryMax      time.Duration
	MaxAttempts   int
}

type Worker struct {
	queue    Queue
	provider provider.RateProvider
	config   Config
	log      *slog.Logger
	now      func() time.Time
}

func New(queue Queue, p provider.RateProvider, config Config, log *slog.Logger) *Worker {
	return &Worker{
		queue:    queue,
		provider: p,
		config:   config,
		log:      log,
		now:      time.Now,
	}
}

// Run polls the persistent queue until ctx is cancelled. It performs one
// immediate poll so a newly started instance does not wait for the first tick.
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

// ProcessOne processes at most one available job and reports whether a job was
// claimed. It is exported to make the worker deterministic in unit tests.
func (w *Worker) ProcessOne(ctx context.Context) bool {
	job, err := w.queue.ClaimNextPending(ctx, w.config.LeaseDuration)
	if err != nil {
		w.log.Error("claim job failed", "err", err)
		return false
	}
	if job == nil {
		return false
	}

	base, quote, ok := splitPair(job.Pair)
	if !ok {
		w.release(ctx, job, w.config.MaxAttempts, "stored currency pair is invalid")
		return true
	}

	price, sourceTime, err := w.provider.FetchRate(ctx, base, quote)
	if err != nil {
		w.log.Warn("fetch rate failed", "job_id", job.ID, "pair", job.Pair, "attempt", job.Attempts, "err", err)
		w.release(ctx, job, job.Attempts, publicProviderError)
		return true
	}

	err = w.queue.Complete(ctx, job.ID, job.LeaseToken, domain.QuoteValue{
		JobID:      job.ID,
		Pair:       job.Pair,
		Price:      price,
		SourceTime: sourceTime,
	})
	if err != nil {
		if errors.Is(err, domain.ErrClaimLost) {
			w.log.Warn("job claim expired before completion", "job_id", job.ID)
			return true
		}
		w.log.Error("complete quote job failed", "job_id", job.ID, "err", err)
		return true
	}

	w.log.Info("quote job completed", "job_id", job.ID, "pair", job.Pair, "attempt", job.Attempts)
	return true
}

func (w *Worker) release(ctx context.Context, job *domain.Job, attempts int, publicError string) {
	nextAttemptAt := w.now().Add(w.retryDelay(attempts))
	if err := w.queue.RetryOrFail(
		ctx,
		job.ID,
		job.LeaseToken,
		attempts,
		w.config.MaxAttempts,
		nextAttemptAt,
		publicError,
	); err != nil {
		if errors.Is(err, domain.ErrClaimLost) {
			w.log.Warn("job claim expired before release", "job_id", job.ID)
			return
		}
		w.log.Error("release quote job failed", "job_id", job.ID, "err", err)
	}
}

func (w *Worker) retryDelay(attempt int) time.Duration {
	if attempt <= 1 {
		return w.config.RetryBase
	}

	delay := w.config.RetryBase
	for range attempt - 1 {
		if delay >= w.config.RetryMax/2 {
			return w.config.RetryMax
		}
		delay *= 2
	}
	return min(delay, w.config.RetryMax)
}

func splitPair(pair string) (base, quote string, ok bool) {
	parts := strings.Split(pair, "/")
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}
