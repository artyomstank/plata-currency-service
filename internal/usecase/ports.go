package usecase

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"currency-quotes/internal/domain"
)

// TransactionManager supplies a context shared by both repositories. The
// implementation commits on success and rolls back on error or panic.
type TransactionManager interface {
	WithinTransaction(context.Context, func(context.Context) error) error
}

type JobsRepository interface {
	// Create returns the original job on an idempotency-key conflict.
	Create(context.Context, *domain.Job) (*domain.Job, bool, error)
	GetByID(context.Context, domain.JobID) (*domain.Job, error)
	GetByIDForUpdate(context.Context, domain.JobID) (*domain.Job, error)
	// LockNextAvailable locks a due pending job or an expired processing lease.
	// It returns nil when the queue is empty and does not change job status.
	LockNextAvailable(context.Context) (*domain.Job, error)
	Save(context.Context, *domain.Job, JobUpdate) error
}

// JobUpdate contains queue coordination metadata, not business rules.
type JobUpdate struct {
	ExpectedLeaseToken uuid.UUID
	LeaseUntil         time.Time
	NextAttemptAt      time.Time
}

type QuotesRepository interface {
	Save(context.Context, *domain.Quote) error
	GetByJobID(context.Context, domain.JobID) (*domain.Quote, error)
	GetLatest(context.Context, string) (*domain.Quote, error)
}

type RateProvider interface {
	FetchRate(context.Context, string, string) (decimal.Decimal, time.Time, error)
}
