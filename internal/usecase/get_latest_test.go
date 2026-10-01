package usecase

import (
	"context"
	"errors"
	"testing"

	"currency-quotes/internal/domain"
)

func TestGetLatestMapsMissingQuote(t *testing.T) {
	t.Parallel()

	quotes := quotesStub{latest: func(context.Context, string) (*domain.QuoteValue, error) {
		return nil, domain.ErrNotFound
	}}

	_, err := NewGetLatest(quotes, testCurrencies).Execute(context.Background(), GetLatestQuoteInput{Pair: "EUR/MXN"})
	if !errors.Is(err, ErrQuoteNotFound) {
		t.Fatalf("GetLatest() error = %v, want ErrQuoteNotFound", err)
	}
}
