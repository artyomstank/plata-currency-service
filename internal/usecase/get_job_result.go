package usecase

import (
	"context"
	"errors"
	"fmt"

	"currency-quotes/internal/domain"
)

type JobReader interface {
	GetByID(context.Context, domain.JobID) (*domain.Job, error)
}

type QuoteByJobReader interface {
	GetByJobID(context.Context, domain.JobID) (*domain.Quote, error)
}

type GetJobResult struct {
	jobs   JobReader
	quotes QuoteByJobReader
	tx     TransactionManager
}

func NewGetJobResult(jobs JobReader, quotes QuoteByJobReader, tx TransactionManager) *GetJobResult {
	return &GetJobResult{jobs: jobs, quotes: quotes, tx: tx}
}

type GetQuoteUpdateInput struct{ JobID domain.JobID }
type JobResult struct {
	Job   *domain.Job
	Value *domain.Quote
}

func (uc *GetJobResult) Execute(ctx context.Context, input GetQuoteUpdateInput) (*JobResult, error) {
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
