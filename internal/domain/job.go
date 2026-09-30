// Package domain contains quote entities and their business rules.
// It has no dependencies on HTTP, PostgreSQL or transaction management.
package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrClaimLost          = errors.New("job claim lost")
	ErrNotFound           = errors.New("not found")
	ErrInvalidJobID       = errors.New("invalid job ID")
	ErrInvalidJob         = errors.New("invalid job")
	ErrInvalidIdempotency = errors.New("invalid idempotency key")
	ErrInvalidTransition  = errors.New("invalid job status transition")
)

const maxIdempotencyKeyLength = 128

// JobID identifies a job independently of quote identities.
type JobID uuid.UUID

func NewJobID() JobID {
	return JobID(uuid.New())
}

func ParseJobID(raw string) (JobID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return JobID{}, fmt.Errorf("%w: %w", ErrInvalidJobID, err)
	}
	if id == uuid.Nil {
		return JobID{}, ErrInvalidJobID
	}
	return JobID(id), nil
}

func (id JobID) String() string {
	return uuid.UUID(id).String()
}

type JobStatus string

const (
	JobStatusPending    JobStatus = "pending"
	JobStatusProcessing JobStatus = "processing"
	JobStatusDone       JobStatus = "done"
	JobStatusFailed     JobStatus = "failed"
)

// Job is a request to obtain a quote. Its fields remain public;
// business transitions are expressed through the lifecycle methods below.
// LeaseToken is storage coordination metadata, not a business transition guard.
type Job struct {
	ID             JobID
	Pair           string
	IdempotencyKey string
	Status         JobStatus
	ErrorMessage   string
	Attempts       int
	LeaseToken     uuid.UUID
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NewJob creates a pending job. An empty idempotency key is allowed;
// a non-empty key is preserved verbatim because it identifies the request.
func NewJob(rawPair, idempotencyKey string, allowedCurrencies []string) (*Job, error) {
	pair, err := NormalizePair(rawPair, allowedCurrencies)
	if err != nil {
		return nil, err
	}
	if len(idempotencyKey) > maxIdempotencyKeyLength {
		return nil, fmt.Errorf("%w: maximum length is %d bytes", ErrInvalidIdempotency, maxIdempotencyKeyLength)
	}
	now := time.Now().UTC()
	return &Job{
		ID: NewJobID(), Pair: pair, IdempotencyKey: idempotencyKey,
		Status: JobStatusPending, CreatedAt: now, UpdatedAt: now,
	}, nil
}

// Start begins one attempt: pending -> processing.
// Reclaiming an expired database lease is a separate storage operation.
func (j *Job) Start() error {
	if err := j.requireStatus(JobStatusPending, JobStatusProcessing); err != nil {
		return err
	}
	j.Attempts++
	j.Status = JobStatusProcessing
	j.UpdatedAt = time.Now().UTC()
	return nil
}

// Complete accepts only a valid quote belonging to this job:
// processing -> done. Persisting both entities atomically is the use case's job.
func (j *Job) Complete(quote *Quote) error {
	if err := j.requireStatus(JobStatusProcessing, JobStatusDone); err != nil {
		return err
	}
	if err := quote.Validate(); err != nil {
		return err
	}
	if quote.JobID != j.ID || quote.Pair != j.Pair {
		return fmt.Errorf("%w: quote does not belong to this job", ErrInvalidQuote)
	}
	j.Status = JobStatusDone
	j.ErrorMessage = ""
	j.LeaseToken = uuid.Nil
	j.UpdatedAt = time.Now().UTC()
	return nil
}

// RetryOrFail releases a failed attempt. It returns to pending while attempts
// remain, otherwise it enters failed. Scheduling/backoff belongs to the worker.
func (j *Job) RetryOrFail(maxAttempts int, publicMessage string) error {
	if err := j.requireStatus(JobStatusProcessing, JobStatusPending); err != nil {
		return err
	}
	if maxAttempts < 1 {
		return fmt.Errorf("%w: max attempts must be at least 1", ErrInvalidJob)
	}
	if strings.TrimSpace(publicMessage) == "" {
		return fmt.Errorf("%w: failure message must not be empty", ErrInvalidJob)
	}
	status := JobStatusPending
	if j.Attempts >= maxAttempts {
		status = JobStatusFailed
	}
	j.Status = status
	j.ErrorMessage = publicMessage
	j.LeaseToken = uuid.Nil
	j.UpdatedAt = time.Now().UTC()
	return nil
}

// Fail terminates an attempt with a permanent error: processing -> failed.
func (j *Job) Fail(publicMessage string) error {
	if err := j.requireStatus(JobStatusProcessing, JobStatusFailed); err != nil {
		return err
	}
	if strings.TrimSpace(publicMessage) == "" {
		return fmt.Errorf("%w: failure message must not be empty", ErrInvalidJob)
	}
	j.Status = JobStatusFailed
	j.ErrorMessage = publicMessage
	j.LeaseToken = uuid.Nil
	j.UpdatedAt = time.Now().UTC()
	return nil
}

func (j *Job) requireStatus(from, to JobStatus) error {
	if j == nil || j.ID == (JobID{}) {
		return fmt.Errorf("%w: job ID must not be empty", ErrInvalidJob)
	}
	if j.Status != from {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, j.Status, to)
	}
	if j.Attempts < 0 || (from == JobStatusProcessing && j.Attempts == 0) {
		return fmt.Errorf("%w: invalid attempt count", ErrInvalidJob)
	}
	return nil
}
