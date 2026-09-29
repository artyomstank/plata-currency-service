package storage

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"currency-quotes/internal/domain"
)

var ErrNotFound = errors.New("not found")

type JobsRepo struct {
	pool *pgxpool.Pool
}

func NewJobsRepo(pool *pgxpool.Pool) *JobsRepo {
	return &JobsRepo{pool: pool}
}

// CreateJob создаёт новую задачу на обновление котировки со статусом pending.
func (r *JobsRepo) CreateJob(ctx context.Context, pair string) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(ctx,
		`INSERT INTO quote_jobs (pair, status) VALUES ($1, 'pending') RETURNING id`,
		pair,
	).Scan(&id)
	return id, err
}

// ClaimNextPending атомарно забирает одну задачу в работу (SKIP LOCKED).
// Это позволяет запускать несколько инстансов сервиса одновременно без
// дублирования обработки одной и той же задачи.
func (r *JobsRepo) ClaimNextPending(ctx context.Context) (*domain.Job, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE quote_jobs
		SET status = 'processing', updated_at = now()
		WHERE id = (
			SELECT id FROM quote_jobs
			WHERE status = 'pending'
			ORDER BY created_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING id, pair, status, attempts, created_at, updated_at
	`)

	var j domain.Job
	err := row.Scan(&j.ID, &j.Pair, &j.Status, &j.Attempts, &j.CreatedAt, &j.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

func (r *JobsRepo) MarkDone(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE quote_jobs SET status = 'done', updated_at = now() WHERE id = $1`, id)
	return err
}

func (r *JobsRepo) MarkFailed(ctx context.Context, id uuid.UUID, errMsg string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE quote_jobs
		SET status = 'failed', error_message = $2, attempts = attempts + 1, updated_at = now()
		WHERE id = $1
	`, id, errMsg)
	return err
}

func (r *JobsRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Job, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, pair, status, coalesce(error_message, ''), attempts, created_at, updated_at
		FROM quote_jobs WHERE id = $1
	`, id)

	var j domain.Job
	err := row.Scan(&j.ID, &j.Pair, &j.Status, &j.ErrorMessage, &j.Attempts, &j.CreatedAt, &j.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}
