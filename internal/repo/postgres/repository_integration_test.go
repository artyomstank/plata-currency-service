//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"currency-quotes/internal/domain"
	jobrepo "currency-quotes/internal/repo/postgres/job"
	quoterepo "currency-quotes/internal/repo/postgres/quote"
	"currency-quotes/internal/usecase"
	"currency-quotes/migrations"
	"currency-quotes/pkg/postgres"
)

type integrationCases struct {
	RequestUpdate *usecase.RequestUpdate
	GetJobResult  *usecase.GetJobResult
	GetLatest     *usecase.GetLatest
	ClaimPending  *usecase.ClaimPending
	CompleteJob   *usecase.CompleteJob
	RetryJob      *usecase.RetryJob
}

func integrationUseCase(t *testing.T) (*integrationCases, *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig("postgres://postgres:postgres@localhost:54322/postgres?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 1
	cfg.ConnConfig.RuntimeParams["search_path"] = "pg_temp"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("local PostgreSQL localhost:54322 is required: %v", err)
	}
	names, err := fs.Glob(migrations.Files, "*.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		data, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		sql := strings.ReplaceAll(string(data), "CREATE TABLE ", "CREATE TEMP TABLE ")
		sql = strings.ReplaceAll(sql, "CREATE EXTENSION IF NOT EXISTS pgcrypto;", "")
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatalf("temporary schema from %s: %v", name, err)
		}
	}
	jobs, quotes, tx := jobrepo.New(pool), quoterepo.New(pool), postgres.NewTransactionManager(pool)
	uc := &integrationCases{
		RequestUpdate: usecase.NewRequestUpdate(jobs, tx, usecase.CurrencyConfig{AllowedCurrencies: []string{"EUR", "MXN", "USD"}}),
		GetJobResult:  usecase.NewGetJobResult(jobs, quotes, tx),
		GetLatest:     usecase.NewGetLatest(quotes, usecase.CurrencyConfig{AllowedCurrencies: []string{"EUR", "MXN", "USD"}}),
		ClaimPending:  usecase.NewClaimPending(jobs, tx, usecase.ClaimPendingConfig{LeaseDuration: 30 * time.Second}),
		CompleteJob:   usecase.NewCompleteJob(jobs, quotes, tx),
		RetryJob:      usecase.NewRetryJob(jobs, tx, usecase.RetryConfig{MaxAttempts: 2, RetryBase: time.Second, RetryMax: 30 * time.Second}),
	}
	return uc, pool
}

func integrationClaim(t *testing.T, uc *integrationCases) *domain.Job {
	t.Helper()
	ctx := context.Background()
	if _, err := uc.RequestUpdate.Execute(ctx, usecase.RequestQuoteUpdateInput{Pair: "EUR/MXN"}); err != nil {
		t.Fatal(err)
	}
	job, err := uc.ClaimPending.Execute(ctx)
	if err != nil || job == nil {
		t.Fatalf("claim=%v err=%v", job, err)
	}
	return job
}

func integrationCompletion(job *domain.Job) usecase.CompleteJobInput {
	return usecase.CompleteJobInput{JobID: job.ID, ExpectedAttempt: job.Attempts, Price: decimal.RequireFromString("19.123456789012345678"), SourceTime: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
}

func TestIntegrationQuoteLifecycle(t *testing.T) {
	uc, _ := integrationUseCase(t)
	ctx := context.Background()
	first, err := uc.RequestUpdate.Execute(ctx, usecase.RequestQuoteUpdateInput{Pair: "eur/mxn"})
	if err != nil {
		t.Fatal(err)
	}
	if first.ErrorMessage != "" {
		t.Errorf("initial error message = %q, want empty", first.ErrorMessage)
	}
	job, err := uc.ClaimPending.Execute(ctx)
	if err != nil || job == nil || job.ID != first.ID || job.Attempts != 1 {
		t.Fatalf("claim=%v err=%v", job, err)
	}
	if next, err := uc.ClaimPending.Execute(ctx); err != nil || next != nil {
		t.Fatalf("active lease claimed: %v err=%v", next, err)
	}
	input := integrationCompletion(job)
	if err := uc.CompleteJob.Execute(ctx, input); err != nil {
		t.Fatal(err)
	}
	result, err := uc.GetJobResult.Execute(ctx, usecase.GetQuoteUpdateInput{JobID: job.ID})
	if err != nil {
		t.Fatal(err)
	}
	if result.Job.Status != domain.JobStatusDone || result.Value == nil || result.Value.JobID != job.ID || !result.Value.Price.Equal(input.Price) {
		t.Fatalf("result=%+v", result)
	}
	latest, err := uc.GetLatest.Execute(ctx, usecase.GetLatestQuoteInput{Pair: job.Pair})
	if err != nil || latest.ID != result.Value.ID {
		t.Fatalf("latest=%v err=%v", latest, err)
	}
	if err := uc.CompleteJob.Execute(ctx, input); !errors.Is(err, domain.ErrClaimLost) {
		t.Fatalf("duplicate completion=%v", err)
	}
}

func TestIntegrationQuoteInsertFailureRollsBackJobStatus(t *testing.T) {
	uc, pool := integrationUseCase(t)
	job := integrationClaim(t, uc)
	input := integrationCompletion(job)
	input.Price = decimal.RequireFromString("100000000000000000000")
	if err := uc.CompleteJob.Execute(context.Background(), input); err == nil {
		t.Fatal("expected numeric overflow")
	}
	stored, err := jobrepo.New(pool).GetByID(context.Background(), job.ID)
	if err != nil || stored.Status != domain.JobStatusProcessing || stored.Attempts != job.Attempts || !stored.LeaseUntil.Equal(job.LeaseUntil.Truncate(time.Microsecond)) {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
	if _, err := quoterepo.New(pool).GetByJobID(context.Background(), job.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("quote persisted after rollback: %v", err)
	}
	if err := uc.CompleteJob.Execute(context.Background(), integrationCompletion(job)); err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationReclaimRejectsStaleWorker(t *testing.T) {
	uc, pool := integrationUseCase(t)
	old := integrationClaim(t, uc)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE quote_jobs SET lease_until = now() - interval '1 second' WHERE id = $1`, uuid.UUID(old.ID)); err != nil {
		t.Fatal(err)
	}
	current, err := uc.ClaimPending.Execute(ctx)
	if err != nil || current == nil || current.ID != old.ID || current.Attempts != 2 || current.Attempts == old.Attempts {
		t.Fatalf("reclaimed=%+v err=%v", current, err)
	}
	if err := uc.CompleteJob.Execute(ctx, integrationCompletion(old)); !errors.Is(err, domain.ErrClaimLost) {
		t.Fatalf("stale completion=%v", err)
	}
	if err := uc.RetryJob.Execute(ctx, usecase.RetryJobInput{JobID: old.ID, ExpectedAttempt: old.Attempts}); !errors.Is(err, domain.ErrClaimLost) {
		t.Fatalf("stale release=%v", err)
	}
	if err := uc.CompleteJob.Execute(ctx, integrationCompletion(current)); err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationRetryBackoffAndAttemptLimit(t *testing.T) {
	uc, pool := integrationUseCase(t)
	job := integrationClaim(t, uc)
	ctx := context.Background()
	var errorIsNull bool
	if err := pool.QueryRow(ctx, `SELECT error_message IS NULL FROM quote_jobs WHERE id = $1`, uuid.UUID(job.ID)).Scan(&errorIsNull); err != nil {
		t.Fatal(err)
	}
	if !errorIsNull {
		t.Error("initial error message was not stored as NULL")
	}
	if job.ErrorMessage != "" {
		t.Errorf("decoded initial error message = %q, want empty", job.ErrorMessage)
	}
	if err := uc.RetryJob.Execute(ctx, usecase.RetryJobInput{JobID: job.ID, ExpectedAttempt: job.Attempts}); err != nil {
		t.Fatal(err)
	}
	if next, err := uc.ClaimPending.Execute(ctx); err != nil || next != nil {
		t.Fatalf("backoff ignored: job=%v err=%v", next, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE quote_jobs SET next_attempt_at = now() WHERE id = $1`, uuid.UUID(job.ID)); err != nil {
		t.Fatal(err)
	}
	job, err := uc.ClaimPending.Execute(ctx)
	if err != nil || job == nil || job.Attempts != 2 {
		t.Fatalf("job=%v err=%v", job, err)
	}
	if job.ErrorMessage != "quote provider is temporarily unavailable" {
		t.Errorf("decoded retry error message = %q", job.ErrorMessage)
	}
	if err := pool.QueryRow(ctx, `SELECT error_message IS NULL FROM quote_jobs WHERE id = $1`, uuid.UUID(job.ID)).Scan(&errorIsNull); err != nil {
		t.Fatal(err)
	}
	if errorIsNull {
		t.Error("retry error message was stored as NULL")
	}
	if err := uc.RetryJob.Execute(ctx, usecase.RetryJobInput{JobID: job.ID, ExpectedAttempt: job.Attempts}); err != nil {
		t.Fatal(err)
	}
	result, err := uc.GetJobResult.Execute(ctx, usecase.GetQuoteUpdateInput{JobID: job.ID})
	if err != nil || result.Job.Status != domain.JobStatusFailed || !result.Job.LeaseUntil.IsZero() || result.Value != nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if next, err := uc.ClaimPending.Execute(ctx); err != nil || next != nil {
		t.Fatalf("terminal job claimed: %v err=%v", next, err)
	}
}

func TestIntegrationSaveRejectsReleasedAttempt(t *testing.T) {
	uc, pool := integrationUseCase(t)
	job := integrationClaim(t, uc)
	ctx := context.Background()
	quote, err := domain.NewQuote(job.ID, job.Pair, decimal.RequireFromString("20.12"), time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), []string{"EUR", "MXN"})
	if err != nil {
		t.Fatal(err)
	}
	completed := *job
	if err := completed.Complete(quote); err != nil {
		t.Fatal(err)
	}
	released := *job
	if err := released.RetryOrFail(2, "provider unavailable", time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	repository := jobrepo.New(pool)
	tx := postgres.NewTransactionManager(pool)
	if err := tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		return repository.Save(txCtx, &completed, job.Attempts-1)
	}); !errors.Is(err, domain.ErrClaimLost) {
		t.Fatalf("wrong expected attempt error = %v", err)
	}
	decreased := *job
	decreased.Attempts--
	if err := tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		return repository.Save(txCtx, &decreased, job.Attempts)
	}); !errors.Is(err, domain.ErrClaimLost) {
		t.Fatalf("decreased attempt error = %v", err)
	}
	if err := uc.RetryJob.Execute(ctx, usecase.RetryJobInput{JobID: job.ID, ExpectedAttempt: job.Attempts}); err != nil {
		t.Fatal(err)
	}
	before, err := repository.GetByID(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, stale := range []*domain.Job{&completed, &released, job} {
		t.Run(string(stale.Status), func(t *testing.T) {
			if err := tx.WithinTransaction(ctx, func(txCtx context.Context) error {
				return repository.Save(txCtx, stale, job.Attempts)
			}); !errors.Is(err, domain.ErrClaimLost) {
				t.Fatalf("released save error = %v", err)
			}
		})
	}
	stored, err := repository.GetByID(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != domain.JobStatusPending || stored.Attempts != job.Attempts || !stored.LeaseUntil.IsZero() || !stored.NextAttemptAt.Equal(before.NextAttemptAt) {
		t.Fatalf("released job changed: %+v", stored)
	}
}

func TestIntegrationExpiredLeaseCanCompleteWithoutReclaim(t *testing.T) {
	uc, pool := integrationUseCase(t)
	job := integrationClaim(t, uc)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE quote_jobs SET lease_until = now() - interval '1 second' WHERE id = $1`, uuid.UUID(job.ID)); err != nil {
		t.Fatal(err)
	}
	if err := uc.CompleteJob.Execute(ctx, integrationCompletion(job)); err != nil {
		t.Fatal(err)
	}
	result, err := uc.GetJobResult.Execute(ctx, usecase.GetQuoteUpdateInput{JobID: job.ID})
	if err != nil {
		t.Fatal(err)
	}
	if result.Job.Status != domain.JobStatusDone || result.Job.Attempts != job.Attempts || !result.Job.LeaseUntil.IsZero() || result.Value == nil {
		t.Fatalf("expired completion result = %+v", result)
	}
}
