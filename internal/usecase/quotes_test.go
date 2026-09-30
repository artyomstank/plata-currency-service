package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"currency-quotes/internal/domain"
)

type jobsStub struct {
	create    func(context.Context, string, string) (*domain.Job, bool, error)
	createJob func(context.Context, *domain.Job) (*domain.Job, bool, error)
	get       func(context.Context, domain.JobID) (*domain.Job, error)
	lock      func(context.Context) (*domain.Job, error)
	getLocked func(context.Context, domain.JobID) (*domain.Job, error)
	save      func(context.Context, *domain.Job, JobUpdate) error
}

func (s jobsStub) Create(ctx context.Context, job *domain.Job) (*domain.Job, bool, error) {
	if s.createJob != nil {
		return s.createJob(ctx, job)
	}
	return s.create(ctx, job.Pair, job.IdempotencyKey)
}
func (s jobsStub) LockNextAvailable(ctx context.Context) (*domain.Job, error) { return s.lock(ctx) }
func (s jobsStub) GetByIDForUpdate(ctx context.Context, id domain.JobID) (*domain.Job, error) {
	return s.getLocked(ctx, id)
}
func (s jobsStub) Save(ctx context.Context, job *domain.Job, update JobUpdate) error {
	return s.save(ctx, job, update)
}

func (s jobsStub) GetByID(ctx context.Context, id domain.JobID) (*domain.Job, error) {
	if s.get == nil {
		panic("unexpected GetByID call")
	}
	return s.get(ctx, id)
}

type quotesStub struct {
	byJob  func(context.Context, domain.JobID) (*domain.QuoteValue, error)
	latest func(context.Context, string) (*domain.QuoteValue, error)
	save   func(context.Context, *domain.Quote) error
}

func (s quotesStub) Save(ctx context.Context, quote *domain.Quote) error { return s.save(ctx, quote) }

func (s quotesStub) GetByJobID(ctx context.Context, id domain.JobID) (*domain.QuoteValue, error) {
	if s.byJob != nil {
		return s.byJob(ctx, id)
	}
	panic("unexpected GetByJobID call")
}

func (s quotesStub) GetLatest(ctx context.Context, pair string) (*domain.QuoteValue, error) {
	if s.latest != nil {
		return s.latest(ctx, pair)
	}
	panic("unexpected GetLatest call")
}

func TestRequestUpdateIsIdempotent(t *testing.T) {
	t.Parallel()

	id := domain.NewJobID()
	jobs := jobsStub{create: func(_ context.Context, pair, key string) (*domain.Job, bool, error) {
		if pair != "EUR/MXN" {
			t.Fatalf("pair = %q, want EUR/MXN", pair)
		}
		if key != "request-1" {
			t.Fatalf("key = %q, want request-1", key)
		}
		return &domain.Job{ID: id, Pair: pair, Status: domain.JobStatusPending}, false, nil
	}}

	result, err := newTestUseCase(jobs, quotesStub{}).RequestUpdate(context.Background(), RequestQuoteUpdateInput{Pair: "eur/mxn", IdempotencyKey: "request-1"})
	if err != nil {
		t.Fatalf("RequestUpdate() unexpected error: %v", err)
	}
	if result.Created {
		t.Fatal("RequestUpdate() Created = true, want false for repeated key")
	}
	if result.Job.ID != id {
		t.Fatalf("RequestUpdate() ID = %s, want %s", result.Job.ID, id)
	}
}

func TestRequestUpdateRejectsKeyUsedForAnotherPair(t *testing.T) {
	t.Parallel()

	jobs := jobsStub{create: func(_ context.Context, _, _ string) (*domain.Job, bool, error) {
		return &domain.Job{ID: domain.NewJobID(), Pair: "USD/MXN", Status: domain.JobStatusPending}, false, nil
	}}

	_, err := newTestUseCase(jobs, quotesStub{}).RequestUpdate(context.Background(), RequestQuoteUpdateInput{Pair: "EUR/MXN", IdempotencyKey: "request-1"})
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("RequestUpdate() error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestGetJobResultMapsMissingJob(t *testing.T) {
	t.Parallel()

	jobs := jobsStub{
		create: func(context.Context, string, string) (*domain.Job, bool, error) {
			panic("unexpected Create call")
		},
		get: func(context.Context, domain.JobID) (*domain.Job, error) {
			return nil, domain.ErrNotFound
		},
	}

	_, err := newTestUseCase(jobs, quotesStub{}).GetJobResult(context.Background(), GetQuoteUpdateInput{JobID: domain.NewJobID()})
	if !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("GetJobResult() error = %v, want ErrJobNotFound", err)
	}
}

func TestGetLatestMapsMissingQuote(t *testing.T) {
	t.Parallel()

	jobs := jobsStub{create: func(context.Context, string, string) (*domain.Job, bool, error) {
		panic("unexpected Create call")
	}}
	quotes := quotesStub{latest: func(context.Context, string) (*domain.QuoteValue, error) {
		return nil, domain.ErrNotFound
	}}

	_, err := newTestUseCase(jobs, quotes).GetLatest(context.Background(), GetLatestQuoteInput{Pair: "EUR/MXN"})
	if !errors.Is(err, ErrQuoteNotFound) {
		t.Fatalf("GetLatest() error = %v, want ErrQuoteNotFound", err)
	}
}

type transactionFunc func(context.Context, func(context.Context) error) error

func (f transactionFunc) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	return f(ctx, fn)
}

func newTestUseCase(jobs JobsRepository, quotes QuotesRepository) *QuotesUseCase {
	return New(jobs, quotes, transactionFunc(func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }), nil, Config{
		AllowedCurrencies: []string{"EUR", "MXN", "USD"}, LeaseDuration: 30 * time.Second,
		MaxAttempts: 5, RetryBase: time.Second, RetryMax: 30 * time.Second,
	})
}
