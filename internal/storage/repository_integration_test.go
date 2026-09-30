//go:build integration

package storage

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
	"currency-quotes/internal/usecase"
	"currency-quotes/migrations"
)

// These tests use only localhost:54322/postgres and connection-local temporary
// tables. Permanent tables and application data are never changed.
func integrationUseCase(t *testing.T) (*usecase.QuotesUseCase, *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig("postgres://postgres:postgres@localhost:54322/postgres?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 1 // every transaction uses the session owning the temporary tables
	// If PostgreSQL drops a connection, a replacement session must never
	// fall back to permanent tables with the same names in public.
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
		sql := strings.ReplaceAll(string(data), "CREATE TABLE quote_", "CREATE TEMP TABLE quote_")
		// PostgreSQL 18 has gen_random_uuid built in; no shared extension changes.
		sql = strings.ReplaceAll(sql, "CREATE EXTENSION IF NOT EXISTS pgcrypto;", "")
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatalf("temporary schema from %s: %v", name, err)
		}
	}
	uc := usecase.New(NewJobsRepo(pool), NewQuotesRepo(pool), NewTransactionManager(pool), nil, usecase.Config{
		AllowedCurrencies: []string{"EUR", "MXN", "USD"}, LeaseDuration: 30 * time.Second,
		MaxAttempts: 2, RetryBase: time.Second, RetryMax: 30 * time.Second,
	})
	return uc, pool
}

func integrationClaim(t *testing.T, uc *usecase.QuotesUseCase) *domain.Job {
	t.Helper()
	ctx := context.Background()
	if _, err := uc.RequestUpdate(ctx, usecase.RequestQuoteUpdateInput{Pair: "EUR/MXN"}); err != nil {
		t.Fatal(err)
	}
	job, err := uc.ClaimPending(ctx)
	if err != nil || job == nil {
		t.Fatalf("claim=%v err=%v", job, err)
	}
	return job
}

func integrationCompletion(job *domain.Job) usecase.CompleteJobInput {
	return usecase.CompleteJobInput{JobID: job.ID, LeaseToken: job.LeaseToken, Price: decimal.RequireFromString("19.123456789012345678"), SourceTime: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
}

func TestIntegrationQuoteLifecycleAndIdempotency(t *testing.T) {
	uc, _ := integrationUseCase(t)
	ctx := context.Background()
	first, err := uc.RequestUpdate(ctx, usecase.RequestQuoteUpdateInput{Pair: "eur/mxn", IdempotencyKey: "key-1"})
	if err != nil || !first.Created {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	repeated, err := uc.RequestUpdate(ctx, usecase.RequestQuoteUpdateInput{Pair: "EUR/MXN", IdempotencyKey: "key-1"})
	if err != nil || repeated.Created || repeated.Job.ID != first.Job.ID {
		t.Fatalf("repeated=%+v err=%v", repeated, err)
	}
	if _, err := uc.RequestUpdate(ctx, usecase.RequestQuoteUpdateInput{Pair: "USD/MXN", IdempotencyKey: "key-1"}); !errors.Is(err, usecase.ErrIdempotencyConflict) {
		t.Fatalf("conflict=%v", err)
	}
	job, err := uc.ClaimPending(ctx)
	if err != nil || job == nil || job.ID != first.Job.ID || job.Attempts != 1 {
		t.Fatalf("claim=%v err=%v", job, err)
	}
	if next, err := uc.ClaimPending(ctx); err != nil || next != nil {
		t.Fatalf("active lease claimed: %v err=%v", next, err)
	}
	input := integrationCompletion(job)
	if err := uc.CompleteJob(ctx, input); err != nil {
		t.Fatal(err)
	}
	result, err := uc.GetJobResult(ctx, usecase.GetQuoteUpdateInput{JobID: job.ID})
	if err != nil {
		t.Fatal(err)
	}
	if result.Job.Status != domain.JobStatusDone || result.Value == nil || result.Value.JobID != job.ID || !result.Value.Price.Equal(input.Price) {
		t.Fatalf("result=%+v", result)
	}
	latest, err := uc.GetLatest(ctx, usecase.GetLatestQuoteInput{Pair: job.Pair})
	if err != nil || latest.ID != result.Value.ID {
		t.Fatalf("latest=%v err=%v", latest, err)
	}
	if err := uc.CompleteJob(ctx, input); !errors.Is(err, domain.ErrClaimLost) {
		t.Fatalf("duplicate completion=%v", err)
	}
}

func TestIntegrationQuoteInsertFailureRollsBackJobStatus(t *testing.T) {
	uc, pool := integrationUseCase(t)
	job := integrationClaim(t, uc)
	input := integrationCompletion(job)
	// A valid positive domain price exceeding NUMERIC(38,18) forces the INSERT
	// to fail after the job UPDATE, exercising a real cross-repository rollback.
	input.Price = decimal.RequireFromString("100000000000000000000")
	if err := uc.CompleteJob(context.Background(), input); err == nil {
		t.Fatal("expected numeric overflow")
	}
	stored, err := NewJobsRepo(pool).GetByID(context.Background(), job.ID)
	if err != nil || stored.Status != domain.JobStatusProcessing || stored.LeaseToken != job.LeaseToken {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
	if _, err := NewQuotesRepo(pool).GetByJobID(context.Background(), job.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("quote persisted after rollback: %v", err)
	}
	if err := uc.CompleteJob(context.Background(), integrationCompletion(job)); err != nil {
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
	current, err := uc.ClaimPending(ctx)
	if err != nil || current == nil || current.ID != old.ID || current.Attempts != 2 || current.LeaseToken == old.LeaseToken {
		t.Fatalf("reclaimed=%+v err=%v", current, err)
	}
	if err := uc.CompleteJob(ctx, integrationCompletion(old)); !errors.Is(err, domain.ErrClaimLost) {
		t.Fatalf("stale completion=%v", err)
	}
	if err := uc.RetryJob(ctx, usecase.RetryJobInput{JobID: old.ID, LeaseToken: old.LeaseToken}); !errors.Is(err, domain.ErrClaimLost) {
		t.Fatalf("stale release=%v", err)
	}
	if err := uc.CompleteJob(ctx, integrationCompletion(current)); err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationRetryBackoffAndAttemptLimit(t *testing.T) {
	uc, pool := integrationUseCase(t)
	job := integrationClaim(t, uc)
	ctx := context.Background()
	if err := uc.RetryJob(ctx, usecase.RetryJobInput{JobID: job.ID, LeaseToken: job.LeaseToken}); err != nil {
		t.Fatal(err)
	}
	if next, err := uc.ClaimPending(ctx); err != nil || next != nil {
		t.Fatalf("backoff ignored: job=%v err=%v", next, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE quote_jobs SET next_attempt_at = now() WHERE id = $1`, uuid.UUID(job.ID)); err != nil {
		t.Fatal(err)
	}
	job, err := uc.ClaimPending(ctx)
	if err != nil || job == nil || job.Attempts != 2 {
		t.Fatalf("job=%v err=%v", job, err)
	}
	if err := uc.RetryJob(ctx, usecase.RetryJobInput{JobID: job.ID, LeaseToken: job.LeaseToken}); err != nil {
		t.Fatal(err)
	}
	result, err := uc.GetJobResult(ctx, usecase.GetQuoteUpdateInput{JobID: job.ID})
	if err != nil || result.Job.Status != domain.JobStatusFailed || result.Job.LeaseToken != uuid.Nil || result.Value != nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if next, err := uc.ClaimPending(ctx); err != nil || next != nil {
		t.Fatalf("terminal job claimed: %v err=%v", next, err)
	}
}
