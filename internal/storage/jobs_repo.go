package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"currency-quotes/internal/domain"
	"currency-quotes/internal/usecase"
)

type JobsRepo struct {
	pool *pgxpool.Pool
}

func NewJobsRepo(pool *pgxpool.Pool) *JobsRepo {
	return &JobsRepo{pool: pool}
}

// jobModel is the PostgreSQL representation; the domain never sees SQL types.
type jobModel struct {
	ID             uuid.UUID
	Pair           string
	IdempotencyKey string
	Status         string
	ErrorMessage   string
	Attempts       int
	LeaseToken     uuid.UUID
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func jobToModel(job *domain.Job) jobModel {
	return jobModel{
		ID: uuid.UUID(job.ID), Pair: job.Pair, IdempotencyKey: job.IdempotencyKey,
		Status: string(job.Status), ErrorMessage: job.ErrorMessage, Attempts: job.Attempts,
		LeaseToken: job.LeaseToken, CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt,
	}
}

func (m jobModel) toDomain() *domain.Job {
	return &domain.Job{
		ID: domain.JobID(m.ID), Pair: m.Pair, IdempotencyKey: m.IdempotencyKey,
		Status: domain.JobStatus(m.Status), ErrorMessage: m.ErrorMessage, Attempts: m.Attempts,
		LeaseToken: m.LeaseToken, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
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

// Create persists the domain constructor's identity and timestamps. Conflict
// handling remains atomic even with concurrent requests using the same key.
func (r *JobsRepo) Create(ctx context.Context, job *domain.Job) (*domain.Job, bool, error) {
	tx, err := requireTransaction(ctx)
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

func (r *JobsRepo) GetByID(ctx context.Context, id domain.JobID) (*domain.Job, error) {
	return scanJob(queryExecutor(ctx, r.pool).QueryRow(ctx,
		`SELECT `+jobColumns+` FROM quote_jobs WHERE id = $1`, uuid.UUID(id)))
}

func (r *JobsRepo) GetByIDForUpdate(ctx context.Context, id domain.JobID) (*domain.Job, error) {
	tx, err := requireTransaction(ctx)
	if err != nil {
		return nil, err
	}
	return scanJob(tx.QueryRow(ctx, `SELECT `+jobColumns+` FROM quote_jobs WHERE id = $1 FOR UPDATE`, uuid.UUID(id)))
}

// LockNextAvailable coordinates concurrent workers; business status and attempt
// changes are performed by the domain in the use case, then persisted by Save.
func (r *JobsRepo) LockNextAvailable(ctx context.Context) (*domain.Job, error) {
	tx, err := requireTransaction(ctx)
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

// Save uses the old lease token as a compare-and-set guard. Domain transitions
// clear the token; queue coordination data is persisted in the same update.
func (r *JobsRepo) Save(ctx context.Context, job *domain.Job, update usecase.JobUpdate) error {
	tx, err := requireTransaction(ctx)
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

func nullableUUID(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}
