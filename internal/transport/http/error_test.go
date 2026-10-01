package http

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"currency-quotes/internal/domain"
)

func TestHTTPErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		path   string
		err    error
		status int
		code   string
	}{
		{"invalid ID", "/v1/quote-updates/not-a-uuid", nil, 400, "INVALID_ARGUMENT"},
		{"empty ID", "/v1/quote-updates/00000000-0000-0000-0000-000000000000", nil, 400, "INVALID_ARGUMENT"},
		{"missing job", "/v1/quote-updates/" + domain.NewJobID().String(), domain.ErrNotFound, 404, "NOT_FOUND"},
		{"missing quote", "/v1/quotes/latest?pair=EUR%2FMXN", domain.ErrNotFound, 404, "NOT_FOUND"},
		{"invalid pair", "/v1/quotes/latest?pair=GBP%2FMXN", nil, 400, "INVALID_ARGUMENT"},
		{"database error", "/v1/quotes/latest?pair=EUR%2FMXN", errors.New("private database details"), 500, "INTERNAL_ERROR"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := newTestHandler(jobsStub{get: func(context.Context, domain.JobID) (*domain.Job, error) {
				return nil, tc.err
			}}, quotesStub{latest: func(context.Context, string) (*domain.QuoteValue, error) {
				return nil, tc.err
			}})
			recorder, response := call(t, handler, http.MethodGet, tc.path, "")
			if recorder.Code != tc.status || response["code"] != tc.code || response["requestId"] != "test-request" {
				t.Fatalf("response = %d %v", recorder.Code, response)
			}
			if strings.Contains(recorder.Body.String(), "private database details") {
				t.Fatal("response leaks internal error")
			}
		})
	}
}

func TestExpiredRequestCannotWriteSuccessfulResponse(t *testing.T) {
	uc := newTestUseCase(jobsStub{}, quotesStub{latest: func(ctx context.Context, pair string) (*domain.QuoteValue, error) {
		<-ctx.Done()
		return &domain.QuoteValue{Pair: pair, Price: decimal.NewFromInt(1), CreatedAt: time.Now()}, nil
	}}, []string{"EUR", "MXN", "USD"})
	handler := New(uc, slog.New(slog.NewTextHandler(io.Discard, nil)), func(context.Context) error { return nil }, transportConfig(time.Millisecond))
	recorder, response := call(t, handler, http.MethodGet, "/v1/quotes/latest?pair=EUR%2FMXN", "")
	if recorder.Code != http.StatusGatewayTimeout || response["code"] != "GATEWAY_TIMEOUT" {
		t.Fatalf("expired request response = %d %v", recorder.Code, response)
	}
}
