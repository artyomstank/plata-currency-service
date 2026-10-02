package usecase

import (
	"context"
	"errors"
	"fmt"

	"currency-quotes/internal/domain"
)

type JobUpdater interface {
	GetByIDForUpdate(context.Context, domain.JobID) (*domain.Job, error)
	Save(ctx context.Context, job *domain.Job, expectedAttempt int) error
}

func loadClaim(ctx context.Context, jobs JobUpdater, id domain.JobID, expectedAttempt int) (*domain.Job, error) {
	if id == (domain.JobID{}) {
		return nil, domain.ErrInvalidJobID
	}
	if expectedAttempt < 1 {
		return nil, domain.ErrClaimLost
	}
	job, err := jobs.GetByIDForUpdate(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, ErrJobNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lock claimed job: %w", err)
	}
	if job.Status != domain.JobStatusProcessing || job.Attempts != expectedAttempt {
		return nil, domain.ErrClaimLost
	}
	return job, nil
}
