package storage

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"currency-quotes/internal/domain"
)

type QuotesRepo struct {
	pool *pgxpool.Pool
}

func NewQuotesRepo(pool *pgxpool.Pool) *QuotesRepo {
	return &QuotesRepo{pool: pool}
}

func (r *QuotesRepo) GetByJobID(ctx context.Context, jobID domain.JobID) (*domain.QuoteValue, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, job_id, pair, price, rate_time, created_at
		FROM quote_values WHERE job_id = $1
	`, uuid.UUID(jobID))
	return scanQuoteValue(row)
}

func (r *QuotesRepo) GetLatest(ctx context.Context, pair string) (*domain.QuoteValue, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, job_id, pair, price, rate_time, created_at
		FROM quote_values
		WHERE pair = $1
		ORDER BY created_at DESC
		LIMIT 1
	`, pair)
	return scanQuoteValue(row)
}

func scanQuoteValue(row pgx.Row) (*domain.QuoteValue, error) {
	var v domain.QuoteValue
	var quoteID, jobID uuid.UUID
	var price string
	err := row.Scan(&quoteID, &jobID, &v.Pair, &price, &v.SourceTime, &v.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	v.ID = domain.QuoteID(quoteID)
	v.JobID = domain.JobID(jobID)
	v.Price, err = decimal.NewFromString(price)
	if err != nil {
		return nil, err
	}
	return &v, nil
}
