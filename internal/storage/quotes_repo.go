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

func (r *QuotesRepo) Insert(ctx context.Context, v domain.QuoteValue) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO quote_values (job_id, pair, price, rate_time)
		VALUES ($1, $2, $3, $4)
	`, v.JobID, v.Pair, v.Price, v.RateTime)
	return err
}

func (r *QuotesRepo) GetByJobID(ctx context.Context, jobID uuid.UUID) (*domain.QuoteValue, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, job_id, pair, price, rate_time, created_at
		FROM quote_values WHERE job_id = $1
	`, jobID)
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
	err := row.Scan(&v.ID, &v.JobID, &v.Pair, &v.Price, &v.RateTime, &v.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}
