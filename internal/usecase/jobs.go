package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"currency-quotes/internal/domain"
)

const publicProviderError = "quote provider is temporarily unavailable"

type CompleteJobInput struct {
	JobID      domain.JobID
	LeaseToken uuid.UUID
	Price      decimal.Decimal
	SourceTime time.Time
}

type RetryJobInput struct {
	JobID      domain.JobID
	LeaseToken uuid.UUID
}

// ClaimPending locks and starts one available job in a short transaction.
// An expired processing lease can be reclaimed after a worker crashes.
func (uc *QuotesUseCase) ClaimPending(ctx context.Context) (*domain.Job, error) {
	var claimed *domain.Job
	err := uc.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		job, err := uc.jobs.LockNextAvailable(txCtx)
		if err != nil {
			return fmt.Errorf("lock available job: %w", err)
		}
		if job == nil {
			return nil
		}
		expectedToken := job.LeaseToken
		if job.Status == domain.JobStatusProcessing {
			err = job.Reclaim()
		} else {
			err = job.Start()
		}
		if err != nil {
			return err
		}
		job.LeaseToken = uuid.New()
		if err := uc.jobs.Save(txCtx, job, JobUpdate{
			ExpectedLeaseToken: expectedToken,
			LeaseUntil:         uc.now().Add(uc.config.LeaseDuration),
		}); err != nil {
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

// CompleteJob stores the quote and domain status change atomically. The lease
// is checked against the locked database row, not the worker's stale copy.
func (uc *QuotesUseCase) CompleteJob(ctx context.Context, input CompleteJobInput) error {
	return uc.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		job, err := uc.loadClaim(txCtx, input.JobID, input.LeaseToken)
		if err != nil {
			return err
		}
		// The current admission list applies to new requests. A previously
		// accepted job can finish even if configuration has changed since.
		quote, err := domain.NewQuote(job.ID, job.Pair, input.Price, input.SourceTime, strings.Split(job.Pair, "/"))
		if err != nil {
			return err
		}
		if err := job.Complete(quote); err != nil {
			return err
		}
		if err := uc.jobs.Save(txCtx, job, JobUpdate{ExpectedLeaseToken: input.LeaseToken}); err != nil {
			return fmt.Errorf("save completed job: %w", err)
		}
		if err := uc.quotes.Save(txCtx, quote); err != nil {
			return fmt.Errorf("save quote: %w", err)
		}
		return nil
	})
}

// RetryJob delegates the retry/failed decision to the domain and persists
// the next attempt time together with releasing the lease.
func (uc *QuotesUseCase) RetryJob(ctx context.Context, input RetryJobInput) error {
	return uc.releaseJob(ctx, input, false, publicProviderError)
}

// ProcessNext is the worker scenario. No database transaction is held while
// FetchRate performs network I/O. The boolean reports whether a job was claimed.
func (uc *QuotesUseCase) ProcessNext(ctx context.Context) (bool, error) {
	job, err := uc.ClaimPending(ctx)
	if err != nil || job == nil {
		return false, err
	}
	base, quote, _ := strings.Cut(job.Pair, "/")
	if _, err := domain.NormalizePair(job.Pair, []string{base, quote}); err != nil {
		releaseErr := uc.releaseJob(ctx, RetryJobInput{job.ID, job.LeaseToken}, true, "stored currency pair is invalid")
		return true, errors.Join(err, releaseErr)
	}
	price, sourceTime, err := uc.provider.FetchRate(ctx, base, quote)
	if err != nil {
		retryErr := uc.RetryJob(ctx, RetryJobInput{job.ID, job.LeaseToken})
		return true, errors.Join(fmt.Errorf("fetch rate for job %s: %w", job.ID, err), retryErr)
	}
	err = uc.CompleteJob(ctx, CompleteJobInput{job.ID, job.LeaseToken, price, sourceTime})
	if errors.Is(err, domain.ErrInvalidQuote) || errors.Is(err, domain.ErrInvalidPair) {
		// Invalid provider data is treated like a provider failure, so an
		// adapter bug cannot keep reclaiming this job indefinitely.
		err = errors.Join(err, uc.RetryJob(ctx, RetryJobInput{job.ID, job.LeaseToken}))
	}
	if err != nil {
		return true, fmt.Errorf("complete job %s: %w", job.ID, err)
	}
	return true, nil
}

func (uc *QuotesUseCase) loadClaim(ctx context.Context, id domain.JobID, token uuid.UUID) (*domain.Job, error) {
	if id == (domain.JobID{}) {
		return nil, domain.ErrInvalidJobID
	}
	if token == uuid.Nil {
		return nil, domain.ErrClaimLost
	}
	job, err := uc.jobs.GetByIDForUpdate(ctx, id)
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

func (uc *QuotesUseCase) releaseJob(ctx context.Context, input RetryJobInput, permanent bool, publicMessage string) error {
	return uc.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		job, err := uc.loadClaim(txCtx, input.JobID, input.LeaseToken)
		if err != nil {
			return err
		}
		if permanent {
			err = job.Fail(publicMessage)
		} else {
			err = job.RetryOrFail(uc.config.MaxAttempts, publicMessage)
		}
		if err != nil {
			return err
		}
		return uc.jobs.Save(txCtx, job, JobUpdate{
			ExpectedLeaseToken: input.LeaseToken,
			NextAttemptAt:      uc.now().Add(uc.retryDelay(job.Attempts)),
		})
	})
}

func (uc *QuotesUseCase) retryDelay(attempt int) time.Duration {
	delay := uc.config.RetryBase
	for i := 1; i < attempt; i++ {
		if delay >= uc.config.RetryMax || delay > uc.config.RetryMax/2 {
			return uc.config.RetryMax
		}
		delay *= 2
	}
	return min(delay, uc.config.RetryMax)
}
