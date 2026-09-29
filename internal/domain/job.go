// Package domain содержит внутренние доменные типы, не зависящие от
// сгенерированного protobuf-кода и транспорта.
package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var (
	ErrClaimLost = errors.New("job claim lost")
	ErrNotFound  = errors.New("not found")
)

type JobStatus string

const (
	JobStatusPending    JobStatus = "pending"
	JobStatusProcessing JobStatus = "processing"
	JobStatusDone       JobStatus = "done"
	JobStatusFailed     JobStatus = "failed"
)

type Job struct {
	ID           uuid.UUID
	Pair         string
	Status       JobStatus
	ErrorMessage string
	Attempts     int
	LeaseToken   uuid.UUID
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type QuoteValue struct {
	ID         uuid.UUID
	JobID      uuid.UUID
	Pair       string
	Price      decimal.Decimal
	SourceTime time.Time
	CreatedAt  time.Time
}
