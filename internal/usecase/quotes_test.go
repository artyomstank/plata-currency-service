package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"currency-quotes/internal/domain"
)

type jobsStub struct {
	create func(context.Context, string, string) (*domain.Job, bool, error)
	get    func(context.Context, uuid.UUID) (*domain.Job, error)
}

func (s jobsStub) CreateJob(ctx context.Context, pair, key string) (*domain.Job, bool, error) {
	return s.create(ctx, pair, key)
}

func (s jobsStub) GetByID(ctx context.Context, id uuid.UUID) (*domain.Job, error) {
	if s.get == nil {
		panic("unexpected GetByID call")
	}
	return s.get(ctx, id)
}

type quotesStub struct {
	byJob  func(context.Context, uuid.UUID) (*domain.QuoteValue, error)
	latest func(context.Context, string) (*domain.QuoteValue, error)
}

func (s quotesStub) GetByJobID(ctx context.Context, id uuid.UUID) (*domain.QuoteValue, error) {
	if s.byJob != nil {
		return s.byJob(ctx, id)
	}
	panic("unexpected GetByJobID call")
}

func (s quotesStub) GetLatest(ctx context.Context, pair string) (*domain.QuoteValue, error) {
	if s.latest != nil {
		return s.latest(ctx, pair)
	}
	panic("unexpected GetLatest call")
}

func TestNormalizePair(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "normalizes case and spaces", input: " eur/mxn ", want: "EUR/MXN"},
		{name: "supports reverse pair", input: "MXN/USD", want: "MXN/USD"},
		{name: "missing separator", input: "EURMXN", wantErr: true},
		{name: "unsupported currency", input: "EUR/GBP", wantErr: true},
		{name: "same currency", input: "USD/USD", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizePair(tt.input)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidPair) {
					t.Fatalf("NormalizePair() error = %v, want ErrInvalidPair", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizePair() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("NormalizePair() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRequestUpdateIsIdempotent(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	jobs := jobsStub{create: func(_ context.Context, pair, key string) (*domain.Job, bool, error) {
		if pair != "EUR/MXN" {
			t.Fatalf("pair = %q, want EUR/MXN", pair)
		}
		if key != "request-1" {
			t.Fatalf("key = %q, want request-1", key)
		}
		return &domain.Job{ID: id, Pair: pair, Status: domain.JobStatusPending}, false, nil
	}}

	result, err := New(jobs, quotesStub{}).RequestUpdate(context.Background(), "eur/mxn", "request-1")
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
		return &domain.Job{ID: uuid.New(), Pair: "USD/MXN", Status: domain.JobStatusPending}, false, nil
	}}

	_, err := New(jobs, quotesStub{}).RequestUpdate(context.Background(), "EUR/MXN", "request-1")
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("RequestUpdate() error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestGetJobResultMapsMissingJob(t *testing.T) {
	t.Parallel()

	jobs := jobsStub{
		create: func(context.Context, string, string) (*domain.Job, bool, error) {
			panic("unexpected CreateJob call")
		},
		get: func(context.Context, uuid.UUID) (*domain.Job, error) {
			return nil, domain.ErrNotFound
		},
	}

	_, err := New(jobs, quotesStub{}).GetJobResult(context.Background(), uuid.New())
	if !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("GetJobResult() error = %v, want ErrJobNotFound", err)
	}
}

func TestGetLatestMapsMissingQuote(t *testing.T) {
	t.Parallel()

	jobs := jobsStub{create: func(context.Context, string, string) (*domain.Job, bool, error) {
		panic("unexpected CreateJob call")
	}}
	quotes := quotesStub{latest: func(context.Context, string) (*domain.QuoteValue, error) {
		return nil, domain.ErrNotFound
	}}

	_, err := New(jobs, quotes).GetLatest(context.Background(), "EUR/MXN")
	if !errors.Is(err, ErrQuoteNotFound) {
		t.Fatalf("GetLatest() error = %v, want ErrQuoteNotFound", err)
	}
}
