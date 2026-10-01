package usecase

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type TransactionManager interface {
	WithinTransaction(context.Context, func(context.Context) error) error
}

type JobUpdate struct {
	ExpectedLeaseToken uuid.UUID
	LeaseUntil         time.Time
	NextAttemptAt      time.Time
}
