// Package usecase содержит бизнес-логику сервиса котировок, не зависящую от
// транспорта (gRPC) — это позволяет тестировать её unit-тестами без сети и БД.
package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"currency-quotes/internal/domain"
)

var (
	ErrInvalidPair         = errors.New("invalid currency pair")
	ErrInvalidIdempotency  = errors.New("invalid idempotency key")
	ErrIdempotencyConflict = errors.New("idempotency key is already used for another pair")
	ErrJobNotFound         = errors.New("quote update not found")
	ErrQuoteNotFound       = errors.New("quote not found")
)

type QuotesUseCase struct {
	jobs   JobsRepository
	quotes QuotesRepository
}

type JobsRepository interface {
	CreateJob(ctx context.Context, pair, idempotencyKey string) (*domain.Job, bool, error)
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Job, error)
}

type QuotesRepository interface {
	GetByJobID(ctx context.Context, jobID uuid.UUID) (*domain.QuoteValue, error)
	GetLatest(ctx context.Context, pair string) (*domain.QuoteValue, error)
}

func New(jobs JobsRepository, quotes QuotesRepository) *QuotesUseCase {
	return &QuotesUseCase{jobs: jobs, quotes: quotes}
}

// NormalizePair приводит код пары к каноническому виду "USD/MXN" и проверяет,
// что обе валюты поддерживаются и различны.
func NormalizePair(raw string) (string, error) {
	parts := strings.Split(strings.ToUpper(strings.TrimSpace(raw)), "/")
	if len(parts) != 2 {
		return "", fmt.Errorf("%w: expected BASE/QUOTE", ErrInvalidPair)
	}
	base, quote := parts[0], parts[1]
	if !isAllowedCurrency(base) || !isAllowedCurrency(quote) {
		return "", fmt.Errorf("%w: supported currencies are EUR, MXN and USD", ErrInvalidPair)
	}
	if base == quote {
		return "", fmt.Errorf("%w: base and quote currency must differ", ErrInvalidPair)
	}
	return base + "/" + quote, nil
}

func isAllowedCurrency(code string) bool {
	switch code {
	case "EUR", "MXN", "USD":
		return true
	default:
		return false
	}
}

type RequestUpdateResult struct {
	Job     *domain.Job
	Created bool
}

func (uc *QuotesUseCase) RequestUpdate(ctx context.Context, rawPair, idempotencyKey string) (*RequestUpdateResult, error) {
	pair, err := NormalizePair(rawPair)
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

func (uc *QuotesUseCase) GetJobResult(ctx context.Context, id uuid.UUID) (*JobResult, error) {
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
	pair, err := NormalizePair(rawPair)
	if err != nil {
		return nil, err
	}
	value, err := uc.quotes.GetLatest(ctx, pair)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, ErrQuoteNotFound
	}
	return value, err
}
