package usecase

import (
	"context"
	"errors"
	"slices"

	"currency-quotes/internal/domain"
)

type LatestQuoteReader interface {
	GetLatest(context.Context, string) (*domain.Quote, error)
}

type GetLatest struct {
	quotes     LatestQuoteReader
	currencies []string
}

func NewGetLatest(quotes LatestQuoteReader, cfg CurrencyConfig) *GetLatest {
	return &GetLatest{quotes: quotes, currencies: slices.Clone(cfg.AllowedCurrencies)}
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
