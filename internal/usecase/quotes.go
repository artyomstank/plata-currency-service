// Package usecase содержит бизнес-логику сервиса котировок, не зависящую от
// HTTP-транспорта — это позволяет тестировать её unit-тестами без сети и БД.
package usecase

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"currency-quotes/internal/domain"
)

var (
	ErrInvalidPair         = domain.ErrInvalidPair
	ErrInvalidIdempotency  = errors.New("invalid idempotency key")
	ErrIdempotencyConflict = errors.New("idempotency key is already used for another pair")
	ErrJobNotFound         = errors.New("quote update not found")
	ErrQuoteNotFound       = errors.New("quote not found")
)

type QuotesUseCase struct {
	jobs       JobsRepository
	quotes     QuotesRepository
	currencies []string
}

type JobsRepository interface {
	CreateJob(ctx context.Context, pair, idempotencyKey string) (*domain.Job, bool, error)
	GetByID(ctx context.Context, id domain.JobID) (*domain.Job, error)
}

type QuotesRepository interface {
	GetByJobID(ctx context.Context, jobID domain.JobID) (*domain.QuoteValue, error)
	GetLatest(ctx context.Context, pair string) (*domain.QuoteValue, error)
}

func New(jobs JobsRepository, quotes QuotesRepository, allowedCurrencies []string) *QuotesUseCase {
	return &QuotesUseCase{jobs: jobs, quotes: quotes, currencies: slices.Clone(allowedCurrencies)}
}

type RequestUpdateResult struct {
	Job     *domain.Job
	Created bool
}

func (uc *QuotesUseCase) RequestUpdate(ctx context.Context, rawPair, idempotencyKey string) (*RequestUpdateResult, error) {
	pair, err := domain.NormalizePair(rawPair, uc.currencies)
	if err != nil {
		return nil, err
	}
	if len(idempotencyKey) > 128 {
		return nil, fmt.Errorf("%w: maximum length is 128 characters", ErrInvalidIdempotency)
	}

	job, created, err := uc.jobs.CreateJob(ctx, pair, idempotencyKey)
	if err != nil {
		return nil, err
	}
	if job.Pair != pair {
		return nil, ErrIdempotencyConflict
	}
	return &RequestUpdateResult{Job: job, Created: created}, nil
}

type JobResult struct {
	Job   *domain.Job
	Value *domain.QuoteValue // nil, если задача ещё не завершена успехом
}

func (uc *QuotesUseCase) GetJobResult(ctx context.Context, id domain.JobID) (*JobResult, error) {
	job, err := uc.jobs.GetByID(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, ErrJobNotFound
	}
	if err != nil {
		return nil, err
	}

	result := &JobResult{Job: job}
	if job.Status == domain.JobStatusDone {
		v, err := uc.quotes.GetByJobID(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("load value for completed quote update: %w", err)
		}
		result.Value = v
	}
	return result, nil
}

func (uc *QuotesUseCase) GetLatest(ctx context.Context, rawPair string) (*domain.QuoteValue, error) {
	pair, err := domain.NormalizePair(rawPair, uc.currencies)
	if err != nil {
		return nil, err
	}
	value, err := uc.quotes.GetLatest(ctx, pair)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, ErrQuoteNotFound
	}
	return value, err
}
