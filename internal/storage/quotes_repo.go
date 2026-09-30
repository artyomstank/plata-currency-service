package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

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

type quoteModel struct {
	ID         uuid.UUID
	JobID      uuid.UUID
	Pair       string
	Price      string
	SourceTime time.Time
	CreatedAt  time.Time
}

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

func (r *QuotesRepo) Save(ctx context.Context, quote *domain.Quote) error {
	tx, err := requireTransaction(ctx)
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

func (r *QuotesRepo) GetByJobID(ctx context.Context, jobID domain.JobID) (*domain.QuoteValue, error) {
	row := queryExecutor(ctx, r.pool).QueryRow(ctx, `
		SELECT id, job_id, pair, price, rate_time, created_at
		FROM quote_values WHERE job_id = $1
	`, uuid.UUID(jobID))
	return scanQuoteValue(row)
}

func (r *QuotesRepo) GetLatest(ctx context.Context, pair string) (*domain.QuoteValue, error) {
	row := queryExecutor(ctx, r.pool).QueryRow(ctx, `
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
