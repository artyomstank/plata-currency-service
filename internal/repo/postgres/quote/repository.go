package quote

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"currency-quotes/internal/domain"
	repopostgres "currency-quotes/internal/repo/postgres"
	"currency-quotes/pkg/postgres"
)

type Repository struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Save(ctx context.Context, quote *domain.Quote) error {
	tx, err := postgres.RequireTransaction(ctx)
	if err != nil {
		return err
	}
	m := quoteToModel(quote)
	_, err = tx.Exec(ctx, `
		INSERT INTO quote_values (id, job_id, pair, price, rate_time, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, m.ID, m.JobID, m.Pair, m.Price, m.SourceTime, m.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert quote value: %w", err)
	}
	return nil
}

func (r *Repository) GetByJobID(ctx context.Context, jobID domain.JobID) (*domain.QuoteValue, error) {
	row := repopostgres.QueryExecutor(ctx, r.pool).QueryRow(ctx, `
		SELECT id, job_id, pair, price, rate_time, created_at
		FROM quote_values WHERE job_id = $1
	`, uuid.UUID(jobID))
	return scanQuoteValue(row)
}

func (r *Repository) GetLatest(ctx context.Context, pair string) (*domain.QuoteValue, error) {
	row := repopostgres.QueryExecutor(ctx, r.pool).QueryRow(ctx, `
		SELECT id, job_id, pair, price, rate_time, created_at
		FROM quote_values
		WHERE pair = $1
		ORDER BY created_at DESC
		LIMIT 1
	`, pair)
	return scanQuoteValue(row)
}

func scanQuoteValue(row pgx.Row) (*domain.QuoteValue, error) {
	var m quoteModel
	err := row.Scan(&m.ID, &m.JobID, &m.Pair, &m.Price, &m.SourceTime, &m.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return m.toDomain()
}
