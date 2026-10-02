package usecase

import (
	"context"
	"fmt"
	"time"

	"currency-quotes/internal/domain"
)

type JobClaimer interface {
	LockNextAvailable(context.Context) (*domain.Job, error)
	Save(ctx context.Context, job *domain.Job, expectedAttempt int) error
}

type ClaimPendingConfig struct {
	LeaseDuration time.Duration
}

type ClaimPending struct {
	jobs          JobClaimer
	tx            TransactionManager
	leaseDuration time.Duration
	now           func() time.Time
}

func NewClaimPending(jobs JobClaimer, tx TransactionManager, cfg ClaimPendingConfig) *ClaimPending {
	return &ClaimPending{jobs: jobs, tx: tx, leaseDuration: cfg.LeaseDuration, now: time.Now}
}

func (uc *ClaimPending) Execute(ctx context.Context) (*domain.Job, error) {
	var claimed *domain.Job
	err := uc.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		job, err := uc.jobs.LockNextAvailable(txCtx)
		if err != nil {
			return fmt.Errorf("lock available job: %w", err)
		}
		if job == nil {
			return nil
		}
		expectedAttempt := job.Attempts
		leaseUntil := uc.now().Add(uc.leaseDuration)
		if job.Status == domain.JobStatusProcessing {
			err = job.Reclaim(leaseUntil)
		} else {
			err = job.Start(leaseUntil)
		}
		if err != nil {
			return err
		}
		if err := uc.jobs.Save(txCtx, job, expectedAttempt); err != nil {
			return fmt.Errorf("save job claim: %w", err)
		}
		claimed = job
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}
