package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"currency-quotes/internal/domain"
)

type queueStub struct {
	job            *domain.Job
	completedValue *domain.QuoteValue
	retry          *retryCall
}

type retryCall struct {
	attempts      int
	maxAttempts   int
	nextAttemptAt time.Time
	publicError   string
}

func (q *queueStub) ClaimNextPending(context.Context, time.Duration) (*domain.Job, error) {
	return q.job, nil
}

func (q *queueStub) Complete(_ context.Context, _ domain.JobID, _ uuid.UUID, value domain.QuoteValue) error {
	q.completedValue = &value
	return nil
}

func (q *queueStub) RetryOrFail(
	_ context.Context,
	_ domain.JobID, _ uuid.UUID,
	attempts, maxAttempts int,
	nextAttemptAt time.Time,
	publicError string,
) error {
	q.retry = &retryCall{
		attempts:      attempts,
		maxAttempts:   maxAttempts,
		nextAttemptAt: nextAttemptAt,
		publicError:   publicError,
	}
	return nil
}

type providerStub struct {
	price    decimal.Decimal
	rateTime time.Time
	err      error
}

func (p providerStub) FetchRate(context.Context, string, string) (decimal.Decimal, time.Time, error) {
	return p.price, p.rateTime, p.err
}

func TestProcessOneCompletesJob(t *testing.T) {
	t.Parallel()

	job := &domain.Job{
		ID:         domain.NewJobID(),
		Pair:       "EUR/MXN",
		Status:     domain.JobStatusProcessing,
		Attempts:   1,
		LeaseToken: uuid.New(),
	}
	queue := &queueStub{job: job}
	rateTime := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	w := newTestWorker(queue, providerStub{
		price:    decimal.RequireFromString("20.12"),
		rateTime: rateTime,
	})

	if !w.ProcessOne(context.Background()) {
		t.Fatal("ProcessOne() = false, want true")
	}
	if queue.completedValue == nil {
		t.Fatal("Complete() was not called")
	}
	if queue.completedValue.Pair != job.Pair || !queue.completedValue.SourceTime.Equal(rateTime) {
		t.Fatalf("Complete() value = %+v", queue.completedValue)
	}
	if queue.retry != nil {
		t.Fatal("RetryOrFail() was called for successful provider response")
	}
}

func TestProcessOneSchedulesProviderRetry(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	queue := &queueStub{job: &domain.Job{
		ID:         domain.NewJobID(),
		Pair:       "EUR/MXN",
		Status:     domain.JobStatusProcessing,
		Attempts:   3,
		LeaseToken: uuid.New(),
	}}
	w := newTestWorker(queue, providerStub{err: errors.New("provider secret details")})
	w.now = func() time.Time { return now }

	w.ProcessOne(context.Background())

	if queue.retry == nil {
		t.Fatal("RetryOrFail() was not called")
	}
	if queue.retry.publicError != publicProviderError {
		t.Fatalf("public error = %q, want %q", queue.retry.publicError, publicProviderError)
	}
	if queue.retry.nextAttemptAt != now.Add(4*time.Second) {
		t.Fatalf("next attempt = %s, want %s", queue.retry.nextAttemptAt, now.Add(4*time.Second))
	}
}

func newTestWorker(queue Queue, provider providerStub) *Worker {
	return New(queue, provider, Config{
		PollInterval:  time.Second,
		LeaseDuration: 10 * time.Second,
		RetryBase:     time.Second,
		RetryMax:      30 * time.Second,
		MaxAttempts:   5,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}
