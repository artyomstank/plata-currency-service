package usecase

import (
	"context"
	"errors"
	"testing"

	"currency-quotes/internal/domain"
)

func TestRequestUpdateIsIdempotent(t *testing.T) {
	t.Parallel()

	id := domain.NewJobID()
	jobs := jobsStub{create: func(_ context.Context, pair, key string) (*domain.Job, bool, error) {
		if pair != "EUR/MXN" {
			t.Fatalf("pair = %q, want EUR/MXN", pair)
		}
		if key != "request-1" {
			t.Fatalf("key = %q, want request-1", key)
		}
		return &domain.Job{ID: id, Pair: pair, Status: domain.JobStatusPending}, false, nil
	}}

	result, err := NewRequestUpdate(jobs, directTransaction(), testCurrencies).Execute(context.Background(), RequestQuoteUpdateInput{Pair: "eur/mxn", IdempotencyKey: "request-1"})
	if err != nil {
		t.Fatalf("RequestUpdate() unexpected error: %v", err)
	}
	if result.Created {
		t.Fatal("RequestUpdate() Created = true, want false for repeated key")
	}
	if result.Job.ID != id {
		t.Fatalf("RequestUpdate() ID = %s, want %s", result.Job.ID, id)
	}
}

func TestRequestUpdateRejectsKeyUsedForAnotherPair(t *testing.T) {
	t.Parallel()

	jobs := jobsStub{create: func(_ context.Context, _, _ string) (*domain.Job, bool, error) {
		return &domain.Job{ID: domain.NewJobID(), Pair: "USD/MXN", Status: domain.JobStatusPending}, false, nil
	}}

	_, err := NewRequestUpdate(jobs, directTransaction(), testCurrencies).Execute(context.Background(), RequestQuoteUpdateInput{Pair: "EUR/MXN", IdempotencyKey: "request-1"})
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("RequestUpdate() error = %v, want ErrIdempotencyConflict", err)
	}
}
