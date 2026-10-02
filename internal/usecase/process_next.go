package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"currency-quotes/internal/domain"
)

type RateProvider interface {
	FetchRate(context.Context, string, string) (decimal.Decimal, time.Time, error)
}

type ProcessNext struct {
	claim    *ClaimPending
	complete *CompleteJob
	retry    *RetryJob
	provider RateProvider
}

func NewProcessNext(claim *ClaimPending, complete *CompleteJob, retry *RetryJob, provider RateProvider) *ProcessNext {
	return &ProcessNext{claim: claim, complete: complete, retry: retry, provider: provider}
}

func (uc *ProcessNext) Execute(ctx context.Context) (bool, error) {
	job, err := uc.claim.Execute(ctx)
	if err != nil || job == nil {
		return false, err
	}
	base, quote, _ := strings.Cut(job.Pair, "/")
	if _, err := domain.NormalizePair(job.Pair, []string{base, quote}); err != nil {
		releaseErr := uc.retry.releaseJob(ctx, RetryJobInput{job.ID, job.Attempts}, true, "stored currency pair is invalid")
		return true, errors.Join(err, releaseErr)
	}
	price, sourceTime, err := uc.provider.FetchRate(ctx, base, quote)
	if err != nil {
		retryErr := uc.retry.Execute(ctx, RetryJobInput{job.ID, job.Attempts})
		return true, errors.Join(fmt.Errorf("fetch rate for job %s: %w", job.ID, err), retryErr)
	}
	err = uc.complete.Execute(ctx, CompleteJobInput{job.ID, job.Attempts, price, sourceTime})
	if errors.Is(err, domain.ErrInvalidQuote) || errors.Is(err, domain.ErrInvalidPair) {
		err = errors.Join(err, uc.retry.Execute(ctx, RetryJobInput{job.ID, job.Attempts}))
	}
	if err != nil {
		return true, fmt.Errorf("complete job %s: %w", job.ID, err)
	}
	return true, nil
}
