package job

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

const jobColumns = `id, pair, status,
 error_message, attempts, lease_until, next_attempt_at, created_at, updated_at`

func jobScanFields(m *jobModel) []any {
	return []any{&m.ID, &m.Pair, &m.Status, &m.ErrorMessage,
		&m.Attempts, &m.LeaseUntil, &m.NextAttemptAt, &m.CreatedAt, &m.UpdatedAt}
}

func scanJob(row pgx.Row) (*domain.Job, error) {
	var m jobModel
	if err := row.Scan(jobScanFields(&m)...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return m.toDomain(), nil
}

func (r *Repository) Create(ctx context.Context, job *domain.Job) (*domain.Job, error) {
	tx, err := postgres.RequireTransaction(ctx)
	if err != nil {
		return nil, err
	}
	m := jobToModel(job)
	row := tx.QueryRow(ctx, `
  INSERT INTO quote_jobs (id, pair, status, created_at, updated_at)
  VALUES ($1, $2, $3, $4, $5)
  RETURNING `+jobColumns+`
	`, m.ID, m.Pair, m.Status, m.CreatedAt, m.UpdatedAt)
	stored, err := scanJob(row)
	if err != nil {
		return nil, fmt.Errorf("create quote job: %w", err)
	}
	return stored, nil
}

func (r *Repository) GetByID(ctx context.Context, id domain.JobID) (*domain.Job, error) {
	return scanJob(repopostgres.QueryExecutor(ctx, r.pool).QueryRow(ctx,
		`SELECT `+jobColumns+` FROM quote_jobs WHERE id = $1`, uuid.UUID(id)))
}

func (r *Repository) GetByIDForUpdate(ctx context.Context, id domain.JobID) (*domain.Job, error) {
	tx, err := postgres.RequireTransaction(ctx)
	if err != nil {
		return nil, err
	}
	return scanJob(tx.QueryRow(ctx, `SELECT `+jobColumns+` FROM quote_jobs WHERE id = $1 FOR UPDATE`, uuid.UUID(id)))
}

func (r *Repository) LockNextAvailable(ctx context.Context) (*domain.Job, error) {
	tx, err := postgres.RequireTransaction(ctx)
	if err != nil {
		return nil, err
	}
	job, err := scanJob(tx.QueryRow(ctx, `
  SELECT `+jobColumns+` FROM quote_jobs
  WHERE (status = 'pending' AND next_attempt_at <= now())
     OR (status = 'processing' AND (lease_until IS NULL OR lease_until <= now()))
  ORDER BY next_attempt_at, created_at
  FOR UPDATE SKIP LOCKED
  LIMIT 1
	`))
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}
	return job, err
}

func (r *Repository) Save(ctx context.Context, job *domain.Job, expectedAttempt int) error {
	tx, err := postgres.RequireTransaction(ctx)
	if err != nil {
		return err
	}
	m := jobToModel(job)
	result, err := tx.Exec(ctx, `
		UPDATE quote_jobs
		SET status = $3, error_message = $4, attempts = $5,
		    lease_until = $6, next_attempt_at = $7, updated_at = $8
		WHERE id = $1 AND attempts = $2
		  AND $5 >= $2
		  AND (status = 'processing'
		       OR (status = 'pending' AND $3 = 'processing' AND $5 > $2))
	`, m.ID, expectedAttempt, m.Status, m.ErrorMessage, m.Attempts,
		m.LeaseUntil, m.NextAttemptAt, m.UpdatedAt)
	if err != nil {
		return fmt.Errorf("save quote job: %w", err)
	}
	if result.RowsAffected() != 1 {
		return domain.ErrClaimLost
	}
	return nil
}
