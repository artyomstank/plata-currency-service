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
)

type JobsRepo struct {
	pool *pgxpool.Pool
}

func NewJobsRepo(pool *pgxpool.Pool) *JobsRepo {
	return &JobsRepo{pool: pool}
}

// CreateJob creates a pending job. If an idempotency key already exists, the
// original job is returned and created is false.
func (r *JobsRepo) CreateJob(ctx context.Context, pair, idempotencyKey string) (*domain.Job, bool, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO quote_jobs (pair, status, idempotency_key)
		VALUES ($1, 'pending', NULLIF($2, ''))
		ON CONFLICT (idempotency_key) WHERE idempotency_key IS NOT NULL
		DO UPDATE SET idempotency_key = EXCLUDED.idempotency_key
		RETURNING id, pair, status, coalesce(error_message, ''), attempts,
		          created_at, updated_at, (xmax = 0) AS created
	`, pair, idempotencyKey)

	var j domain.Job
	var created bool
	if err := row.Scan(
		&j.ID,
		&j.Pair,
		&j.Status,
		&j.ErrorMessage,
		&j.Attempts,
		&j.CreatedAt,
		&j.UpdatedAt,
		&created,
	); err != nil {
		return nil, false, fmt.Errorf("create quote job: %w", err)
	}
	return &j, created, nil
}

// ClaimNextPending атомарно забирает одну задачу в работу (SKIP LOCKED).
// Это позволяет запускать несколько инстансов сервиса одновременно без
// дублирования обработки одной и той же задачи.
func (r *JobsRepo) ClaimNextPending(ctx context.Context, leaseDuration time.Duration) (*domain.Job, error) {
	leaseSeconds := max(int64(leaseDuration/time.Second), 1)
	row := r.pool.QueryRow(ctx, `
		UPDATE quote_jobs
		SET status = 'processing',
		    attempts = attempts + 1,
		    lease_token = gen_random_uuid(),
		    lease_until = now() + ($1 * interval '1 second'),
		    updated_at = now()
		WHERE id = (
			SELECT id FROM quote_jobs
			WHERE (status = 'pending' AND next_attempt_at <= now())
			   OR (status = 'processing' AND (lease_until IS NULL OR lease_until <= now()))
			ORDER BY next_attempt_at, created_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING id, pair, status, attempts, lease_token, created_at, updated_at
	`, leaseSeconds)

	var j domain.Job
	err := row.Scan(&j.ID, &j.Pair, &j.Status, &j.Attempts, &j.LeaseToken, &j.CreatedAt, &j.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

// Complete stores the quote and marks its job done in one transaction. The
// lease token prevents a stale worker from completing a reclaimed job.
func (r *JobsRepo) Complete(ctx context.Context, id, leaseToken uuid.UUID, value domain.QuoteValue) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin completion transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	result, err := tx.Exec(ctx, `
		UPDATE quote_jobs
		SET status = 'done', error_message = NULL, lease_token = NULL,
		    lease_until = NULL, updated_at = now()
		WHERE id = $1 AND status = 'processing' AND lease_token = $2
	`, id, leaseToken)
	if err != nil {
		return fmt.Errorf("mark quote job done: %w", err)
	}
	if result.RowsAffected() != 1 {
		return domain.ErrClaimLost
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO quote_values (job_id, pair, price, rate_time)
		VALUES ($1, $2, $3, $4)
	`, id, value.Pair, value.Price.String(), value.SourceTime); err != nil {
		return fmt.Errorf("insert quote value: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit quote completion: %w", err)
	}
	return nil
}

// RetryOrFail releases the current lease. Failed jobs are returned to pending
// until maxAttempts is reached; nextAttemptAt implements persistent backoff.
func (r *JobsRepo) RetryOrFail(
	ctx context.Context,
	id, leaseToken uuid.UUID,
	attempts, maxAttempts int,
	nextAttemptAt time.Time,
	publicError string,
) error {
	status := domain.JobStatusPending
	if attempts >= maxAttempts {
		status = domain.JobStatusFailed
	}

	result, err := r.pool.Exec(ctx, `
		UPDATE quote_jobs
		SET status = $3, error_message = $4, next_attempt_at = $5,
		    lease_token = NULL, lease_until = NULL, updated_at = now()
		WHERE id = $1 AND status = 'processing' AND lease_token = $2
	`, id, leaseToken, status, publicError, nextAttemptAt)
	if err != nil {
		return fmt.Errorf("release quote job: %w", err)
	}
	if result.RowsAffected() != 1 {
		return domain.ErrClaimLost
	}
	return nil
}

func (r *JobsRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Job, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, pair, status, coalesce(error_message, ''), attempts, created_at, updated_at
		FROM quote_jobs WHERE id = $1
	`, id)

	var j domain.Job
	err := row.Scan(&j.ID, &j.Pair, &j.Status, &j.ErrorMessage, &j.Attempts, &j.CreatedAt, &j.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}
