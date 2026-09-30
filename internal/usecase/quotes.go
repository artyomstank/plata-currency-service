// Package usecase coordinates domain entities, repositories and the provider.
package usecase

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"currency-quotes/internal/domain"
)

var (
	ErrInvalidPair         = domain.ErrInvalidPair
	ErrInvalidIdempotency  = domain.ErrInvalidIdempotency
	ErrIdempotencyConflict = errors.New("idempotency key is already used for another pair")
	ErrJobNotFound         = errors.New("quote update not found")
	ErrQuoteNotFound       = errors.New("quote not found")
)

type QuotesUseCase struct {
	jobs     JobsRepository
	quotes   QuotesRepository
	tx       TransactionManager
	provider RateProvider
	config   Config
	now      func() time.Time
}

type Config struct {
	AllowedCurrencies []string
	LeaseDuration     time.Duration
	MaxAttempts       int
	RetryBase         time.Duration
	RetryMax          time.Duration
}

// New receives configuration already validated by the composition root.
func New(jobs JobsRepository, quotes QuotesRepository, tx TransactionManager, provider RateProvider, config Config) *QuotesUseCase {
	config.AllowedCurrencies = slices.Clone(config.AllowedCurrencies)
	return &QuotesUseCase{
		jobs: jobs, quotes: quotes, tx: tx, provider: provider,
		config: config, now: time.Now,
	}
}

type RequestQuoteUpdateInput struct {
	Pair           string
	IdempotencyKey string
}

type GetQuoteUpdateInput struct {
	JobID domain.JobID
}

type GetLatestQuoteInput struct {
	Pair string
}

type RequestUpdateResult struct {
	Job     *domain.Job
	Created bool
}

func (uc *QuotesUseCase) RequestUpdate(ctx context.Context, input RequestQuoteUpdateInput) (*RequestUpdateResult, error) {
	job, err := domain.NewJob(input.Pair, input.IdempotencyKey, uc.config.AllowedCurrencies)
	if err != nil {
		return nil, err
	}
	var result *RequestUpdateResult
	err = uc.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		stored, created, err := uc.jobs.Create(txCtx, job)
		if err != nil {
			return fmt.Errorf("create quote update: %w", err)
		}
		if stored.Pair != job.Pair {
			return ErrIdempotencyConflict
		}
		result = &RequestUpdateResult{Job: stored, Created: created}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

type JobResult struct {
	Job   *domain.Job
	Value *domain.Quote // nil until the job completes successfully
}

func (uc *QuotesUseCase) GetJobResult(ctx context.Context, input GetQuoteUpdateInput) (*JobResult, error) {
	if input.JobID == (domain.JobID{}) {
		return nil, domain.ErrInvalidJobID
	}
	var result *JobResult
	err := uc.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		job, err := uc.jobs.GetByID(txCtx, input.JobID)
		if errors.Is(err, domain.ErrNotFound) {
			return ErrJobNotFound
		}
		if err != nil {
			return fmt.Errorf("load quote update: %w", err)
		}
		result = &JobResult{Job: job}
		if job.Status == domain.JobStatusDone {
			result.Value, err = uc.quotes.GetByJobID(txCtx, input.JobID)
			if err != nil {
				return fmt.Errorf("load value for completed quote update: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (uc *QuotesUseCase) GetLatest(ctx context.Context, input GetLatestQuoteInput) (*domain.Quote, error) {
	pair, err := domain.NormalizePair(input.Pair, uc.config.AllowedCurrencies)
	if err != nil {
		return nil, err
	}
	value, err := uc.quotes.GetLatest(ctx, pair)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, ErrQuoteNotFound
	}
	return value, err
}
