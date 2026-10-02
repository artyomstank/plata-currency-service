//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"currency-quotes/internal/domain"
	idempotencyrepo "currency-quotes/internal/repo/postgres/idempotency"
	transporthttp "currency-quotes/internal/transport/http"
	"currency-quotes/internal/transport/http/handler"
	"currency-quotes/internal/usecase"
	"currency-quotes/migrations"
	"currency-quotes/pkg/httpserver/middleware"
	"currency-quotes/pkg/postgres"
)

func integrationHTTP(uc *integrationCases, pool *pgxpool.Pool, store middleware.IdempotencyStore) http.Handler {
	return transporthttp.New(&handler.UseCases{RequestUpdate: uc.RequestUpdate, GetJobResult: uc.GetJobResult, GetLatest: uc.GetLatest},
		slog.New(slog.NewTextHandler(io.Discard, nil)), pool.Ping,
		transporthttp.Config{RequestTimeout: time.Second, MaxBodyBytes: 1 << 20}, store, postgres.NewTransactionManager(pool))
}

func integrationPost(router http.Handler, key, body, requestID string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/v1/quote-updates", strings.NewReader(body))
	request.Header.Set("Idempotency-Key", key)
	request.Header.Set("X-Request-ID", requestID)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func integrationCounts(t *testing.T, pool *pgxpool.Pool, wantJobs, wantResponses int) {
	t.Helper()
	var jobs, responses int
	if err := pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM quote_jobs), (SELECT count(*) FROM http_idempotency)`).Scan(&jobs, &responses); err != nil {
		t.Fatal(err)
	}
	if jobs != wantJobs {
		t.Errorf("jobs = %d, want %d", jobs, wantJobs)
	}
	if responses != wantResponses {
		t.Errorf("responses = %d, want %d", responses, wantResponses)
	}
}

func TestIntegrationHTTPIdempotencyReplaysOriginalResponseAfterCompletion(t *testing.T) {
	uc, pool := integrationUseCase(t)
	first := integrationPost(integrationHTTP(uc, pool, idempotencyrepo.New()), "key", `{"pair":"EUR/MXN"}`, "first")
	if first.Code != http.StatusAccepted {
		t.Fatalf("first response = %d %s", first.Code, first.Body.String())
	}
	var status int
	var body []byte
	if err := pool.QueryRow(context.Background(), `SELECT status_code, response_body FROM http_idempotency WHERE key = 'key'`).Scan(&status, &body); err != nil {
		t.Fatal(err)
	}
	if status != first.Code || !bytes.Equal(body, first.Body.Bytes()) {
		t.Fatalf("stored response = %d %s", status, body)
	}
	job, err := uc.ClaimPending.Execute(context.Background())
	if err != nil || job == nil {
		t.Fatalf("claim=%v error=%v", job, err)
	}
	if err := uc.CompleteJob.Execute(context.Background(), integrationCompletion(job)); err != nil {
		t.Fatal(err)
	}
	router := integrationHTTP(uc, pool, idempotencyrepo.New())
	for _, body := range []string{`{"pair":"USD/MXN"}`, `{`} {
		replayed := integrationPost(router, "key", body, "second")
		if replayed.Code != first.Code {
			t.Errorf("replayed status = %d, want %d", replayed.Code, first.Code)
		}
		if !bytes.Equal(replayed.Body.Bytes(), first.Body.Bytes()) {
			t.Errorf("replayed body = %s, want %s", replayed.Body.String(), first.Body.String())
		}
		if replayed.Header().Get("X-Request-ID") != "second" {
			t.Errorf("replayed request ID = %q", replayed.Header().Get("X-Request-ID"))
		}
		if replayed.Header().Get("Content-Type") != first.Header().Get("Content-Type") {
			t.Error("replayed content type differs from original")
		}
	}
	integrationCounts(t, pool, 1, 1)
	result, err := uc.GetJobResult.Execute(context.Background(), usecase.GetQuoteUpdateInput{JobID: job.ID})
	if err != nil || result.Job.Status != domain.JobStatusDone {
		t.Fatalf("current job = %v, error = %v", result, err)
	}
	ids := make(map[string]bool)
	for range 2 {
		response := integrationPost(router, "", `{"pair":"EUR/MXN"}`, "without-key")
		if response.Code != http.StatusAccepted {
			t.Fatalf("without key response = %d %s", response.Code, response.Body.String())
		}
		var value struct{ JobID string }
		if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		ids[value.JobID] = true
	}
	if len(ids) != 2 {
		t.Error("requests without keys did not create independent jobs")
	}
	integrationCounts(t, pool, 3, 1)
}

type failingResponseStore struct {
	*idempotencyrepo.Repository
}

func (s failingResponseStore) Save(ctx context.Context, key string, response *middleware.StoredResponse) error {
	if err := s.Repository.Save(ctx, key, response); err != nil {
		return err
	}
	return errors.New("injected response persistence failure")
}

func TestIntegrationHTTPIdempotencyRollsBackJobAndResponseTogether(t *testing.T) {
	uc, pool := integrationUseCase(t)
	failing := integrationHTTP(uc, pool, failingResponseStore{idempotencyrepo.New()})
	response := integrationPost(failing, "key", `{"pair":"EUR/MXN"}`, "failed")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "jobId") || strings.Contains(response.Body.String(), "injected") {
		t.Error("uncommitted response or private error leaked")
	}
	integrationCounts(t, pool, 0, 0)
	retried := integrationPost(integrationHTTP(uc, pool, idempotencyrepo.New()), "key", `{"pair":"EUR/MXN"}`, "retried")
	if retried.Code != http.StatusAccepted {
		t.Fatalf("retry response = %d %s", retried.Code, retried.Body.String())
	}
	integrationCounts(t, pool, 1, 1)
}

func TestIntegrationHTTPIdempotencyDoesNotReserveKeyForInvalidRequest(t *testing.T) {
	uc, pool := integrationUseCase(t)
	router := integrationHTTP(uc, pool, idempotencyrepo.New())
	for _, body := range []string{`{`, `{"pair":"CAD/MXN"}`} {
		response := integrationPost(router, "key", body, "invalid")
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid response = %d %s", response.Code, response.Body.String())
		}
		integrationCounts(t, pool, 0, 0)
	}
	response := integrationPost(router, "key", `{"pair":"EUR/MXN"}`, "valid")
	if response.Code != http.StatusAccepted {
		t.Fatalf("valid response = %d %s", response.Code, response.Body.String())
	}
	integrationCounts(t, pool, 1, 1)
}

type failingRequestUpdate struct {
	uc     *usecase.RequestUpdate
	cancel context.CancelFunc
}

func (f failingRequestUpdate) Execute(ctx context.Context, input usecase.RequestQuoteUpdateInput) (*domain.Job, error) {
	job, err := f.uc.Execute(ctx, input)
	if err != nil {
		return nil, err
	}
	if f.cancel != nil {
		f.cancel()
		return job, nil
	}
	panic("private panic after job creation")
}

func TestIntegrationHTTPIdempotencyRollsBackOnPanicAndCancellation(t *testing.T) {
	for _, cancelRequest := range []bool{false, true} {
		t.Run(fmt.Sprint("cancel=", cancelRequest), func(t *testing.T) {
			uc, pool := integrationUseCase(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			failing := failingRequestUpdate{uc: uc.RequestUpdate}
			if cancelRequest {
				failing.cancel = cancel
			}
			router := transporthttp.New(&handler.UseCases{RequestUpdate: failing},
				slog.New(slog.NewTextHandler(io.Discard, nil)), pool.Ping,
				transporthttp.Config{RequestTimeout: time.Second, MaxBodyBytes: 1 << 20}, idempotencyrepo.New(), postgres.NewTransactionManager(pool))
			request := httptest.NewRequest(http.MethodPost, "/v1/quote-updates", strings.NewReader(`{"pair":"EUR/MXN"}`)).WithContext(ctx)
			request.Header.Set("Idempotency-Key", "key")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
			integrationCounts(t, pool, 0, 0)
			retried := integrationPost(integrationHTTP(uc, pool, idempotencyrepo.New()), "key", `{"pair":"EUR/MXN"}`, "retry")
			if retried.Code != http.StatusAccepted {
				t.Fatalf("retry = %d %s", retried.Code, retried.Body.String())
			}
			integrationCounts(t, pool, 1, 1)
		})
	}
}

func TestIntegrationHTTPIdempotencyMigrationPreservesExistingKeys(t *testing.T) {
	uc, pool := integrationUseCase(t)
	job := integrationClaim(t, uc)
	ctx := context.Background()
	if err := uc.CompleteJob.Execute(ctx, integrationCompletion(job)); err != nil {
		t.Fatal(err)
	}
	expected := &middleware.StoredResponse{StatusCode: 202, Header: http.Header{"Content-Type": {"application/json"}}, Body: []byte(fmt.Sprintf("{\"jobId\":\"%s\",\"status\":\"JOB_STATUS_PENDING\"}\n", job.ID))}
	store := idempotencyrepo.New()
	tx := postgres.NewTransactionManager(pool)
	if err := tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if _, err := store.Lock(ctx, "legacy-key"); err != nil {
			return err
		}
		return store.Save(ctx, "legacy-key", expected)
	}); err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("../../../migrations/0004_http_idempotency.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(down)); err != nil {
		t.Fatal(err)
	}
	var key string
	if err := pool.QueryRow(ctx, `SELECT idempotency_key FROM quote_jobs WHERE id = $1`, job.ID.String()).Scan(&key); err != nil || key != "legacy-key" {
		t.Fatalf("restored key = %q, error = %v", key, err)
	}
	up, err := migrations.Files.ReadFile("0004_http_idempotency.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, strings.ReplaceAll(string(up), "CREATE TABLE ", "CREATE TEMP TABLE ")); err != nil {
		t.Fatal(err)
	}
	if err := tx.WithinTransaction(ctx, func(ctx context.Context) error {
		stored, err := store.Lock(ctx, "legacy-key")
		if err != nil {
			return err
		}
		if stored == nil || stored.StatusCode != expected.StatusCode || !bytes.Equal(stored.Body, expected.Body) || stored.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("migrated response = %+v", stored)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	integrationCounts(t, pool, 1, 1)
}
