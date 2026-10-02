package usecase

import (
	"context"
	"errors"
	"testing"

	"currency-quotes/internal/domain"
)

func TestRequestUpdateCreatesJobForEachExecution(t *testing.T) {
	t.Parallel()
	jobs := jobsStub{createJob: func(_ context.Context, job *domain.Job) (*domain.Job, error) { return job, nil }}
	uc := NewRequestUpdate(jobs, directTransaction(), testCurrencies)
	first, err := uc.Execute(context.Background(), RequestQuoteUpdateInput{Pair: "eur/mxn"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := uc.Execute(context.Background(), RequestQuoteUpdateInput{Pair: "EUR/MXN"})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID {
		t.Fatal("independent executions returned the same job")
	}
	if first.Pair != "EUR/MXN" || second.Pair != "EUR/MXN" {
		t.Fatalf("pairs = %q, %q", first.Pair, second.Pair)
	}
}

func TestRequestUpdatePreservesRepositoryError(t *testing.T) {
	t.Parallel()
	failure := errors.New("insert failed")
	jobs := jobsStub{createJob: func(context.Context, *domain.Job) (*domain.Job, error) { return nil, failure }}
	_, err := NewRequestUpdate(jobs, directTransaction(), testCurrencies).Execute(context.Background(), RequestQuoteUpdateInput{Pair: "EUR/MXN"})
	if !errors.Is(err, failure) {
		t.Fatalf("error = %v, want repository error", err)
	}
}
