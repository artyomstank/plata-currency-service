package usecase

import (
	"context"
	"errors"
	"slices"

	"currency-quotes/internal/domain"
)

type LatestQuotes interface {
	GetLatest(context.Context, string) (*domain.Quote, error)
}

type GetLatest struct {
	quotes     LatestQuotes
	currencies []string
}

func NewGetLatest(quotes LatestQuotes, currencies []string) *GetLatest {
	return &GetLatest{quotes: quotes, currencies: slices.Clone(currencies)}
}

type GetLatestQuoteInput struct{ Pair string }

func (uc *GetLatest) Execute(ctx context.Context, input GetLatestQuoteInput) (*domain.Quote, error) {
	pair, err := domain.NormalizePair(input.Pair, uc.currencies)
	if err != nil {
		return nil, err
	}
	value, err := uc.quotes.GetLatest(ctx, pair)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, ErrQuoteNotFound
	}
	return value, err
}
