// Package usecase содержит бизнес-логику сервиса котировок, не зависящую от
// транспорта (gRPC) — это позволяет тестировать её unit-тестами без сети и БД.
package usecase

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"currency-quotes/internal/domain"
	"currency-quotes/internal/storage"
)

// AllowedCurrencies — намеренно ограниченный набор валют по условию задачи.
var AllowedCurrencies = map[string]bool{
	"USD": true,
	"EUR": true,
	"MXN": true,
}

type QuotesUseCase struct {
	jobs   *storage.JobsRepo
	quotes *storage.QuotesRepo
}

func New(jobs *storage.JobsRepo, quotes *storage.QuotesRepo) *QuotesUseCase {
	return &QuotesUseCase{jobs: jobs, quotes: quotes}
}

// NormalizePair приводит код пары к каноническому виду "USD/MXN" и проверяет,
// что обе валюты поддерживаются и различны.
func NormalizePair(raw string) (string, error) {
	parts := strings.Split(strings.ToUpper(strings.TrimSpace(raw)), "/")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid pair format, expected BASE/QUOTE")
	}
	base, quote := parts[0], parts[1]
	if !AllowedCurrencies[base] || !AllowedCurrencies[quote] {
		return "", fmt.Errorf("unsupported currency in pair %s", raw)
	}
	if base == quote {
		return "", fmt.Errorf("base and quote currency must differ")
	}
	return base + "/" + quote, nil
}

func (uc *QuotesUseCase) RequestUpdate(ctx context.Context, rawPair string) (uuid.UUID, error) {
	pair, err := NormalizePair(rawPair)
	if err != nil {
		return uuid.Nil, err
	}
	return uc.jobs.CreateJob(ctx, pair)
}

type JobResult struct {
	Job   *domain.Job
	Value *domain.QuoteValue // nil, если задача ещё не завершена успехом
}

func (uc *QuotesUseCase) GetJobResult(ctx context.Context, id uuid.UUID) (*JobResult, error) {
	job, err := uc.jobs.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	result := &JobResult{Job: job}
	if job.Status == domain.JobStatusDone {
		v, err := uc.quotes.GetByJobID(ctx, id)
		if err != nil {
			return nil, err
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
	return uc.quotes.GetLatest(ctx, pair)
}
