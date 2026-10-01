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
	"currency-quotes/internal/usecase"
	"currency-quotes/pkg/postgres"
)

type Repository struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const jobColumns = `id, pair, coalesce(idempotency_key, ''), status,
 coalesce(error_message, ''), attempts,
 coalesce(lease_token, '00000000-0000-0000-0000-000000000000'::uuid), created_at, updated_at`

func jobScanFields(m *jobModel) []any {
	return []any{&m.ID, &m.Pair, &m.IdempotencyKey, &m.Status, &m.ErrorMessage,
		&m.Attempts, &m.LeaseToken, &m.CreatedAt, &m.UpdatedAt}
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

func (r *Repository) Create(ctx context.Context, job *domain.Job) (*domain.Job, bool, error) {
	tx, err := postgres.RequireTransaction(ctx)
	if err != nil {
		return nil, false, err
	}
	m := jobToModel(job)
	row := tx.QueryRow(ctx, `
  INSERT INTO quote_jobs (id, pair, status, idempotency_key, created_at, updated_at)
  VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6)
  ON CONFLICT (idempotency_key) WHERE idempotency_key IS NOT NULL
  DO UPDATE SET idempotency_key = EXCLUDED.idempotency_key
  RETURNING `+jobColumns+`, (xmax = 0) AS created
	`, m.ID, m.Pair, m.Status, m.IdempotencyKey, m.CreatedAt, m.UpdatedAt)
	var stored jobModel
	var created bool
	fields := append(jobScanFields(&stored), &created)
	if err := row.Scan(fields...); err != nil {
		return nil, false, fmt.Errorf("create quote job: %w", err)
	}
	return stored.toDomain(), created, nil
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

func (r *Repository) Save(ctx context.Context, job *domain.Job, update usecase.JobUpdate) error {
	tx, err := postgres.RequireTransaction(ctx)
	if err != nil {
		return err
	}
	m := jobToModel(job)
	result, err := tx.Exec(ctx, `
		UPDATE quote_jobs
		SET status = $3, error_message = NULLIF($4, ''), attempts = $5,
		    lease_token = $6, lease_until = $7,
		    next_attempt_at = COALESCE($8, next_attempt_at), updated_at = $9
		WHERE id = $1 AND lease_token IS NOT DISTINCT FROM $2::uuid
		  AND status IN ('pending', 'processing')
	`, m.ID, nullableUUID(update.ExpectedLeaseToken), m.Status, m.ErrorMessage, m.Attempts,
		nullableUUID(m.LeaseToken), nullableTime(update.LeaseUntil), nullableTime(update.NextAttemptAt), m.UpdatedAt)
	if err != nil {
		return fmt.Errorf("save quote job: %w", err)
	}
	if result.RowsAffected() != 1 {
		return domain.ErrClaimLost
	}
	return nil
}
