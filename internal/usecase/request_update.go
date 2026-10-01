package usecase

import (
	"context"
	"fmt"
	"slices"

	"currency-quotes/internal/domain"
)

type JobCreator interface {
	Create(context.Context, *domain.Job) (*domain.Job, bool, error)
}

type CurrencyConfig struct {
	AllowedCurrencies []string
}

type RequestUpdate struct {
	jobs       JobCreator
	tx         TransactionManager
	currencies []string
}

func NewRequestUpdate(jobs JobCreator, tx TransactionManager, cfg CurrencyConfig) *RequestUpdate {
	return &RequestUpdate{jobs: jobs, tx: tx, currencies: slices.Clone(cfg.AllowedCurrencies)}
}

type RequestQuoteUpdateInput struct {
	Pair           string
	IdempotencyKey string
}
type RequestUpdateResult struct {
	Job     *domain.Job
	Created bool
}

func (uc *RequestUpdate) Execute(ctx context.Context, input RequestQuoteUpdateInput) (*RequestUpdateResult, error) {
	job, err := domain.NewJob(input.Pair, input.IdempotencyKey, uc.currencies)
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
