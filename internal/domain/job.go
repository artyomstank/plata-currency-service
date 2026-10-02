package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrClaimLost         = errors.New("job claim lost")
	ErrNotFound          = errors.New("not found")
	ErrInvalidJobID      = errors.New("invalid job ID")
	ErrInvalidJob        = errors.New("invalid job")
	ErrInvalidTransition = errors.New("invalid job status transition")
)

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

type Job struct {
	ID            JobID
	Pair          string
	Status        JobStatus
	ErrorMessage  string
	Attempts      int
	LeaseUntil    time.Time
	NextAttemptAt time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func NewJob(rawPair string, allowedCurrencies []string) (*Job, error) {
	pair, err := NormalizePair(rawPair, allowedCurrencies)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return &Job{
		ID: NewJobID(), Pair: pair,
		Status: JobStatusPending, NextAttemptAt: now, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (j *Job) Start(leaseUntil time.Time) error {
	if err := j.requireStatus(JobStatusPending, JobStatusProcessing); err != nil {
		return err
	}
	if leaseUntil.IsZero() {
		return fmt.Errorf("%w: lease deadline must not be empty", ErrInvalidJob)
	}
	j.Attempts++
	j.Status = JobStatusProcessing
	j.LeaseUntil = leaseUntil.UTC()
	j.UpdatedAt = time.Now().UTC()
	return nil
}

func (j *Job) Reclaim(leaseUntil time.Time) error {
	if err := j.requireStatus(JobStatusProcessing, JobStatusProcessing); err != nil {
		return err
	}
	if leaseUntil.IsZero() {
		return fmt.Errorf("%w: lease deadline must not be empty", ErrInvalidJob)
	}
	j.Attempts++
	j.LeaseUntil = leaseUntil.UTC()
	j.UpdatedAt = time.Now().UTC()
	return nil
}

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
	j.LeaseUntil = time.Time{}
	j.UpdatedAt = time.Now().UTC()
	return nil
}

func (j *Job) RetryOrFail(maxAttempts int, publicMessage string, nextAttemptAt time.Time) error {
	if err := j.requireStatus(JobStatusProcessing, JobStatusPending); err != nil {
		return err
	}
	if maxAttempts < 1 {
		return fmt.Errorf("%w: max attempts must be at least 1", ErrInvalidJob)
	}
	if strings.TrimSpace(publicMessage) == "" {
		return fmt.Errorf("%w: failure message must not be empty", ErrInvalidJob)
	}
	if nextAttemptAt.IsZero() {
		return fmt.Errorf("%w: next attempt time must not be empty", ErrInvalidJob)
	}
	status := JobStatusPending
	if j.Attempts >= maxAttempts {
		status = JobStatusFailed
	}
	j.Status = status
	j.ErrorMessage = publicMessage
	j.LeaseUntil = time.Time{}
	j.NextAttemptAt = nextAttemptAt.UTC()
	j.UpdatedAt = time.Now().UTC()
	return nil
}

func (j *Job) Fail(publicMessage string) error {
	if err := j.requireStatus(JobStatusProcessing, JobStatusFailed); err != nil {
		return err
	}
	if strings.TrimSpace(publicMessage) == "" {
		return fmt.Errorf("%w: failure message must not be empty", ErrInvalidJob)
	}
	j.Status = JobStatusFailed
	j.ErrorMessage = publicMessage
	j.LeaseUntil = time.Time{}
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
