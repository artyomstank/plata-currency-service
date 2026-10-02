package usecase

import (
	"context"
	"errors"
	"testing"

	"currency-quotes/internal/domain"
)

func TestGetJobResultMapsMissingJob(t *testing.T) {
	t.Parallel()

	jobs := jobsStub{
		create: func(context.Context, string) (*domain.Job, error) {
			panic("unexpected Create call")
		},
		get: func(context.Context, domain.JobID) (*domain.Job, error) {
			return nil, domain.ErrNotFound
		},
	}

	_, err := NewGetJobResult(jobs, quotesStub{}, directTransaction()).Execute(context.Background(), GetQuoteUpdateInput{JobID: domain.NewJobID()})
	if !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("GetJobResult() error = %v, want ErrJobNotFound", err)
	}
}
