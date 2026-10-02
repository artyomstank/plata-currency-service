package quote

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"currency-quotes/internal/domain"
)

func quoteToModel(quote *domain.Quote) quoteModel {
	return quoteModel{
		ID: uuid.UUID(quote.ID), JobID: uuid.UUID(quote.JobID), Pair: quote.Pair,
		Price: quote.Price.String(), SourceTime: quote.SourceTime, CreatedAt: quote.CreatedAt,
	}
}

func (m quoteModel) toDomain() (*domain.Quote, error) {
	price, err := decimal.NewFromString(m.Price)
	if err != nil {
		return nil, fmt.Errorf("decode quote price: %w", err)
	}
	return &domain.Quote{
		ID: domain.QuoteID(m.ID), JobID: domain.JobID(m.JobID), Pair: m.Pair,
		Price: price, SourceTime: m.SourceTime, CreatedAt: m.CreatedAt,
	}, nil
}
