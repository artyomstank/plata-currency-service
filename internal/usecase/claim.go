package usecase

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"currency-quotes/internal/domain"
)

type ClaimedJobs interface {
	GetByIDForUpdate(context.Context, domain.JobID) (*domain.Job, error)
	Save(context.Context, *domain.Job, JobUpdate) error
}

func loadClaim(ctx context.Context, jobs ClaimedJobs, id domain.JobID, token uuid.UUID) (*domain.Job, error) {
	if id == (domain.JobID{}) {
		return nil, domain.ErrInvalidJobID
	}
	if token == uuid.Nil {
		return nil, domain.ErrClaimLost
	}
	job, err := jobs.GetByIDForUpdate(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, ErrJobNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lock claimed job: %w", err)
	}
	if job.Status != domain.JobStatusProcessing || job.LeaseToken != token {
		return nil, domain.ErrClaimLost
	}
	return job, nil
}
