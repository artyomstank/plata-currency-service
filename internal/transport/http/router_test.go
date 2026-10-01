package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"currency-quotes/internal/domain"
	httphandler "currency-quotes/internal/transport/http/handler"
	"currency-quotes/internal/usecase"
)

type jobsStub struct {
	create func(context.Context, string, string) (*domain.Job, bool, error)
	get    func(context.Context, domain.JobID) (*domain.Job, error)
}

func (s jobsStub) Create(ctx context.Context, job *domain.Job) (*domain.Job, bool, error) {
	return s.create(ctx, job.Pair, job.IdempotencyKey)
}
func (s jobsStub) GetByIDForUpdate(context.Context, domain.JobID) (*domain.Job, error) {
	panic("unexpected row lock")
}
func (s jobsStub) LockNextAvailable(context.Context) (*domain.Job, error) { panic("unexpected claim") }
func (s jobsStub) Save(context.Context, *domain.Job, usecase.JobUpdate) error {
	panic("unexpected job save")
}

func (s jobsStub) GetByID(ctx context.Context, id domain.JobID) (*domain.Job, error) {
	return s.get(ctx, id)
}

type quotesStub struct {
	byJob  func(context.Context, domain.JobID) (*domain.QuoteValue, error)
	latest func(context.Context, string) (*domain.QuoteValue, error)
}

func (s quotesStub) Save(context.Context, *domain.Quote) error { panic("unexpected quote save") }

func (s quotesStub) GetByJobID(ctx context.Context, id domain.JobID) (*domain.QuoteValue, error) {
	return s.byJob(ctx, id)
}

func (s quotesStub) GetLatest(ctx context.Context, pair string) (*domain.QuoteValue, error) {
	return s.latest(ctx, pair)
}

const maxRequestBody = 1 << 20

func transportConfig(timeout time.Duration) Config {
	return Config{RequestTimeout: timeout, MaxBodyBytes: maxRequestBody}
}

func newTestHandler(jobs jobsStub, quotes quotesStub) http.Handler {
	return New(newTestUseCase(jobs, quotes, []string{"EUR", "MXN", "USD"}), slog.New(slog.NewTextHandler(io.Discard, nil)),
		func(context.Context) error { return nil }, transportConfig(time.Second))
}

func call(t *testing.T, handler http.Handler, method, path, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Idempotency-Key", "request-1")
	request.Header.Set("X-Request-ID", "test-request")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json; body: %s", got, recorder.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v; body: %s", err, recorder.Body.String())
	}
	return recorder, response
}

func TestRequestUpdateIdempotency(t *testing.T) {
	var stored *domain.Job
	handler := newTestHandler(jobsStub{create: func(_ context.Context, pair, key string) (*domain.Job, bool, error) {
		if key != "request-1" {
			t.Fatalf("idempotency key = %q", key)
		}
		if stored != nil {
			return stored, false, nil
		}
		stored = &domain.Job{ID: domain.NewJobID(), Pair: pair, Status: domain.JobStatusPending}
		return stored, true, nil
	}}, quotesStub{})

	for _, status := range []int{http.StatusAccepted, http.StatusOK} {
		recorder, response := call(t, handler, http.MethodPost, "/v1/quote-updates", `{"pair":" eur/mxn "}`)
		if recorder.Code != status || response["jobId"] != stored.ID.String() || response["status"] != "JOB_STATUS_PENDING" {
			t.Fatalf("response = %d %v, want status %d and original job", recorder.Code, response, status)
		}
		if stored.Pair != "EUR/MXN" {
			t.Fatalf("stored pair = %q", stored.Pair)
		}
	}
	recorder, response := call(t, handler, http.MethodPost, "/v1/quote-updates", `{"pair":"USD/MXN"}`)
	if recorder.Code != http.StatusConflict || response["code"] != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("conflict response = %d %v", recorder.Code, response)
	}
}

func TestRequestUpdateRejectsInvalidBodyBeforeCreatingJob(t *testing.T) {
	handler := newTestHandler(jobsStub{create: func(context.Context, string, string) (*domain.Job, bool, error) {
		t.Fatal("invalid request reached repository")
		return nil, false, nil
	}}, quotesStub{})
	for _, body := range []string{"", "{", `{"pair":42}`, `{"pair":"EUR/MXN","extra":true}`, `{"pair":"EUR/MXN"} {}`, `{"pair":"EUR/GBP"}`, `{"pair":"USD/USD"}`} {
		t.Run(body, func(t *testing.T) {
			recorder, response := call(t, handler, http.MethodPost, "/v1/quote-updates", body)
			if recorder.Code != http.StatusBadRequest || response["code"] != "INVALID_ARGUMENT" {
				t.Fatalf("response = %d %v", recorder.Code, response)
			}
			if response["requestId"] != "test-request" || recorder.Header().Get("X-Request-ID") != "test-request" {
				t.Fatalf("request ID not preserved: %v", response)
			}
		})
	}
	body := `{"pair":"` + strings.Repeat("x", maxRequestBody) + `"}`
	recorder, _ := call(t, handler, http.MethodPost, "/v1/quote-updates", body)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body status = %d", recorder.Code)
	}
}

func TestGetUpdateResultFields(t *testing.T) {
	id := domain.NewJobID()
	createdAt := time.Date(2026, 9, 30, 12, 0, 0, 123456000, time.FixedZone("MSK", 3*60*60))
	price := decimal.RequireFromString("19.123456789012345678")
	for _, status := range []domain.JobStatus{domain.JobStatusPending, domain.JobStatusProcessing, domain.JobStatusDone, domain.JobStatusFailed} {
		t.Run(string(status), func(t *testing.T) {
			handler := newTestHandler(jobsStub{get: func(_ context.Context, jobID domain.JobID) (*domain.Job, error) {
				if jobID != id {
					t.Fatalf("job ID = %s, want %s", jobID, id)
				}
				return &domain.Job{ID: id, Pair: "EUR/MXN", Status: status, ErrorMessage: "provider unavailable"}, nil
			}}, quotesStub{byJob: func(context.Context, domain.JobID) (*domain.QuoteValue, error) {
				return &domain.QuoteValue{Price: price, CreatedAt: createdAt}, nil
			}})
			recorder, response := call(t, handler, http.MethodGet, "/v1/quote-updates/"+id.String(), "")
			if recorder.Code != http.StatusOK || response["jobId"] != id.String() || response["pair"] != "EUR/MXN" {
				t.Fatalf("response = %d %v", recorder.Code, response)
			}
			wantStatus := "JOB_STATUS_" + strings.ToUpper(string(status))
			if response["status"] != wantStatus {
				t.Fatalf("status = %v, want %s", response["status"], wantStatus)
			}
			if status == domain.JobStatusDone {
				if response["price"] != price.String() || response["updatedAt"] != "2026-09-30T09:00:00.123456Z" {
					t.Fatalf("completed result = %v", response)
				}
			} else {
				for _, field := range []string{"price", "updatedAt"} {
					if _, present := response[field]; present {
						t.Fatalf("unfinished result contains %s: %v", field, response)
					}
				}
			}
			if status == domain.JobStatusFailed {
				if response["errorMessage"] != "provider unavailable" {
					t.Fatalf("failed result = %v", response)
				}
			} else if _, present := response["errorMessage"]; present {
				t.Fatalf("non-failed result contains errorMessage: %v", response)
			}
		})
	}
}

func TestGetLatestPreservesDecimalPrecision(t *testing.T) {
	handler := newTestHandler(jobsStub{}, quotesStub{latest: func(_ context.Context, pair string) (*domain.QuoteValue, error) {
		if pair != "EUR/MXN" {
			t.Fatalf("pair = %q", pair)
		}
		return &domain.QuoteValue{
			Pair: pair, Price: decimal.RequireFromString("19.123456789012345678"),
			CreatedAt: time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC),
		}, nil
	}})
	recorder, response := call(t, handler, http.MethodGet, "/v1/quotes/latest?pair=eur%2Fmxn", "")
	if recorder.Code != http.StatusOK || response["price"] != "19.123456789012345678" || response["updatedAt"] != "2026-09-30T09:00:00Z" {
		t.Fatalf("response = %d %v", recorder.Code, response)
	}
}

func TestRequestTimeoutReachesRepository(t *testing.T) {
	uc := newTestUseCase(jobsStub{}, quotesStub{latest: func(ctx context.Context, _ string) (*domain.QuoteValue, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}, []string{"EUR", "MXN", "USD"})
	handler := New(uc, slog.New(slog.NewTextHandler(io.Discard, nil)), func(context.Context) error { return nil }, transportConfig(time.Millisecond))
	recorder, response := call(t, handler, http.MethodGet, "/v1/quotes/latest?pair=EUR%2FMXN", "")
	if recorder.Code != http.StatusGatewayTimeout || response["code"] != "GATEWAY_TIMEOUT" {
		t.Fatalf("response = %d %v", recorder.Code, response)
	}
}

func TestHealthEndpoints(t *testing.T) {
	for _, available := range []bool{true, false} {
		checked := false
		handler := New(nil, slog.New(slog.NewTextHandler(io.Discard, nil)), func(ctx context.Context) error {
			checked = true
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("readiness check has no deadline")
			}
			if !available {
				return errors.New("database unavailable")
			}
			return nil
		}, transportConfig(time.Second))
		recorder, _ := call(t, handler, http.MethodGet, "/healthz", "")
		if recorder.Code != http.StatusOK || checked {
			t.Fatal("liveness depends on the database")
		}
		recorder, response := call(t, handler, http.MethodGet, "/readyz", "")
		wantStatus, wantBody := http.StatusOK, "ready"
		if !available {
			wantStatus, wantBody = http.StatusServiceUnavailable, "not_ready"
		}
		if !checked || recorder.Code != wantStatus || response["status"] != wantBody {
			t.Fatalf("readiness = %d %v, checked = %v", recorder.Code, response, checked)
		}
	}
}

func TestRoutingErrorsAreJSON(t *testing.T) {
	handler := newTestHandler(jobsStub{}, quotesStub{})
	for _, tc := range []struct {
		method, path string
		status       int
		code, allow  string
	}{
		{http.MethodGet, "/v1/unknown", 404, "NOT_FOUND", ""},
		{http.MethodGet, "/v1/quote-updates", 405, "METHOD_NOT_ALLOWED", "POST"},
		{http.MethodPost, "/v1/quotes/latest", 405, "METHOD_NOT_ALLOWED", "GET, HEAD"},
	} {
		recorder, response := call(t, handler, tc.method, tc.path, "")
		if recorder.Code != tc.status || response["code"] != tc.code || recorder.Header().Get("Allow") != tc.allow {
			t.Fatalf("response for %s %s = %d %v Allow=%q", tc.method, tc.path, recorder.Code, response, recorder.Header().Get("Allow"))
		}
	}
}

func TestHTTPUsesConfiguredCurrencies(t *testing.T) {
	currencies := []string{"CHF", "JPY"}
	jobs := jobsStub{create: func(_ context.Context, pair, _ string) (*domain.Job, bool, error) {
		if pair != "CHF/JPY" {
			t.Fatalf("unconfigured pair reached repository: %s", pair)
		}
		return &domain.Job{ID: domain.NewJobID(), Pair: pair, Status: domain.JobStatusPending}, true, nil
	}}
	quotes := quotesStub{latest: func(_ context.Context, pair string) (*domain.QuoteValue, error) {
		if pair != "CHF/JPY" {
			t.Fatalf("unconfigured pair reached repository: %s", pair)
		}
		return &domain.QuoteValue{Pair: pair, Price: decimal.NewFromInt(100), CreatedAt: time.Now()}, nil
	}}
	handler := New(newTestUseCase(jobs, quotes, currencies),
		slog.New(slog.NewTextHandler(io.Discard, nil)), func(context.Context) error { return nil }, transportConfig(time.Second))
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{http.MethodPost, "/v1/quote-updates", `{"pair":"chf/jpy"}`, http.StatusAccepted},
		{http.MethodPost, "/v1/quote-updates", `{"pair":"EUR/USD"}`, http.StatusBadRequest},
		{http.MethodGet, "/v1/quotes/latest?pair=chf%2Fjpy", "", http.StatusOK},
		{http.MethodGet, "/v1/quotes/latest?pair=EUR%2FMXN", "", http.StatusBadRequest},
	} {
		recorder, response := call(t, handler, tc.method, tc.path, tc.body)
		if recorder.Code != tc.status {
			t.Fatalf("configured request %s %s = %d %v", tc.method, tc.path, recorder.Code, response)
		}
		if tc.status == http.StatusBadRequest && response["code"] != "INVALID_ARGUMENT" {
			t.Fatalf("validation error = %v", response)
		}
	}
}

func TestHandlerPanicReturnsJSONAndLogs500(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, nil))
	uc := newTestUseCase(jobsStub{}, quotesStub{latest: func(context.Context, string) (*domain.QuoteValue, error) {
		panic("private database panic")
	}}, []string{"EUR", "MXN", "USD"})
	handler := New(uc, log, func(context.Context) error { return nil }, transportConfig(time.Second))
	recorder, response := call(t, handler, http.MethodGet, "/v1/quotes/latest?pair=EUR%2FMXN", "")
	if recorder.Code != http.StatusInternalServerError || response["code"] != "INTERNAL_ERROR" || response["requestId"] != "test-request" {
		t.Fatalf("panic response = %d %v", recorder.Code, response)
	}
	if strings.Contains(recorder.Body.String(), "private database panic") {
		t.Fatal("panic details leaked to client")
	}
	decoder := json.NewDecoder(&logs)
	found := false
	for decoder.More() {
		var entry map[string]any
		if err := decoder.Decode(&entry); err != nil {
			t.Fatal(err)
		}
		if entry["msg"] == "http request" {
			found = true
			if entry["status"] != float64(http.StatusInternalServerError) || entry["request_id"] != "test-request" {
				t.Fatalf("incorrect panic access log: %v", entry)
			}
		}
	}
	if !found {
		t.Fatal("access log missing after handler panic")
	}
}

func TestOversizedTrailingBodyDoesNotCreateJob(t *testing.T) {
	handler := newTestHandler(jobsStub{create: func(context.Context, string, string) (*domain.Job, bool, error) {
		t.Fatal("oversized request reached repository")
		return nil, false, nil
	}}, quotesStub{})
	body := `{"pair":"EUR/MXN"}` + strings.Repeat(" ", maxRequestBody)
	recorder, response := call(t, handler, http.MethodPost, "/v1/quote-updates", body)
	if recorder.Code != http.StatusRequestEntityTooLarge || response["code"] != "INVALID_ARGUMENT" {
		t.Fatalf("oversized trailing body response = %d %v", recorder.Code, response)
	}
}

type testTransactionManager struct{}

func (testTransactionManager) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}
func newTestUseCase(jobs jobsStub, quotes quotesStub, currencies []string) *httphandler.UseCases {
	return &httphandler.UseCases{
		RequestUpdate: usecase.NewRequestUpdate(jobs, testTransactionManager{}, usecase.CurrencyConfig{AllowedCurrencies: currencies}),
		GetJobResult:  usecase.NewGetJobResult(jobs, quotes, testTransactionManager{}),
		GetLatest:     usecase.NewGetLatest(quotes, usecase.CurrencyConfig{AllowedCurrencies: currencies}),
	}
}
