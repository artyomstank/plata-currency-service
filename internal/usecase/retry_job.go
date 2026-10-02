package usecase

import (
	"context"
	"time"

	"currency-quotes/internal/domain"
)

const publicProviderError = "quote provider is temporarily unavailable"

type RetryConfig struct {
	MaxAttempts int
	RetryBase   time.Duration
	RetryMax    time.Duration
}
type RetryJob struct {
	jobs   JobUpdater
	tx     TransactionManager
	config RetryConfig
	now    func() time.Time
}

func NewRetryJob(jobs JobUpdater, tx TransactionManager, config RetryConfig) *RetryJob {
	return &RetryJob{jobs: jobs, tx: tx, config: config, now: time.Now}
}

type RetryJobInput struct {
	JobID           domain.JobID
	ExpectedAttempt int
}

func (uc *RetryJob) Execute(ctx context.Context, input RetryJobInput) error {
	return uc.releaseJob(ctx, input, false, publicProviderError)
}

func (uc *RetryJob) releaseJob(ctx context.Context, input RetryJobInput, permanent bool, publicMessage string) error {
	return uc.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		job, err := loadClaim(txCtx, uc.jobs, input.JobID, input.ExpectedAttempt)
		if err != nil {
			return err
		}
		if permanent {
			err = job.Fail(publicMessage)
		} else {
			err = job.RetryOrFail(uc.config.MaxAttempts, publicMessage, uc.now().Add(uc.retryDelay(job.Attempts)))
		}
		if err != nil {
			return err
		}
		return uc.jobs.Save(txCtx, job, input.ExpectedAttempt)
	})
}

func (uc *RetryJob) retryDelay(attempt int) time.Duration {
	delay := uc.config.RetryBase
	for i := 1; i < attempt; i++ {
		if delay >= uc.config.RetryMax || delay > uc.config.RetryMax/2 {
			return uc.config.RetryMax
		}
		delay *= 2
	}
	return min(delay, uc.config.RetryMax)
}
