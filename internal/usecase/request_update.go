package usecase

import (
	"context"
	"fmt"
	"slices"

	"currency-quotes/internal/domain"
)

type JobCreator interface {
	Create(context.Context, *domain.Job) (*domain.Job, error)
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
	Pair string
}

func (uc *RequestUpdate) Execute(ctx context.Context, input RequestQuoteUpdateInput) (*domain.Job, error) {
	job, err := domain.NewJob(input.Pair, uc.currencies)
	if err != nil {
		return nil, err
	}
	var result *domain.Job
	err = uc.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		stored, err := uc.jobs.Create(txCtx, job)
		if err != nil {
			return fmt.Errorf("create quote update: %w", err)
		}
		result = stored
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
