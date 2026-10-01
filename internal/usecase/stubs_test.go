package usecase

import (
	"context"

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

type transactionFunc func(context.Context, func(context.Context) error) error

func (f transactionFunc) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	return f(ctx, fn)
}

func directTransaction() TransactionManager {
	return transactionFunc(func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })
}

var testCurrencies = []string{"EUR", "MXN", "USD"}
