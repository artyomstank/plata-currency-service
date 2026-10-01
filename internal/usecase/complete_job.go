package usecase

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"currency-quotes/internal/domain"
)

type QuoteWriter interface {
	Save(context.Context, *domain.Quote) error
}

type CompleteJob struct {
	jobs   JobUpdater
	quotes QuoteWriter
	tx     TransactionManager
}

func NewCompleteJob(jobs JobUpdater, quotes QuoteWriter, tx TransactionManager) *CompleteJob {
	return &CompleteJob{jobs: jobs, quotes: quotes, tx: tx}
}

type CompleteJobInput struct {
	JobID      domain.JobID
	LeaseToken uuid.UUID
	Price      decimal.Decimal
	SourceTime time.Time
}

func (uc *CompleteJob) Execute(ctx context.Context, input CompleteJobInput) error {
	return uc.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		job, err := loadClaim(txCtx, uc.jobs, input.JobID, input.LeaseToken)
		if err != nil {
			return err
		}
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
