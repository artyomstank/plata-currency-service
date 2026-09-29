// Package domain содержит внутренние доменные типы, не зависящие от
// сгенерированного protobuf-кода и транспорта.
package domain

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
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
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type QuoteValue struct {
	ID        uuid.UUID
	JobID     uuid.UUID
	Pair      string
	Price     decimal.Decimal
	RateTime  time.Time
	CreatedAt time.Time
}
