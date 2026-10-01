package handler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"currency-quotes/internal/domain"
	"currency-quotes/internal/usecase"
	"currency-quotes/pkg/httpserver/middleware"
)

type requestUpdateFunc func(context.Context, usecase.RequestQuoteUpdateInput) (*usecase.RequestUpdateResult, error)

func (fn requestUpdateFunc) Execute(ctx context.Context, input usecase.RequestQuoteUpdateInput) (*usecase.RequestUpdateResult, error) {
	return fn(ctx, input)
}

func TestRequestUpdatePreservesInputContextAndErrors(t *testing.T) {
	failure := errors.New("operation failed")
	for _, failUseCase := range []bool{true, false} {
		t.Run(map[bool]string{true: "usecase error", false: "response writer error"}[failUseCase], func(t *testing.T) {
			jobID := domain.NewJobID()
			scenarios := &UseCases{RequestUpdate: requestUpdateFunc(func(ctx context.Context, input usecase.RequestQuoteUpdateInput) (*usecase.RequestUpdateResult, error) {
				if input.Pair != "eur/mxn" || input.IdempotencyKey != "key-1" || middleware.RequestIDFromContext(ctx) != "request-1" {
					t.Fatalf("input = %+v, request ID = %q", input, middleware.RequestIDFromContext(ctx))
				}
				if failUseCase {
					return nil, failure
				}
				return &usecase.RequestUpdateResult{Job: &domain.Job{ID: jobID, Status: domain.JobStatusPending}, Created: true}, nil
			})}
			wrote := false
			h := New(scenarios, slog.New(slog.NewTextHandler(io.Discard, nil)), nil,
				func(w http.ResponseWriter, r *http.Request, status int, value any) error {
					wrote = true
					response, ok := value.(updateResponse)
					if !ok || status != http.StatusAccepted || response.JobID != jobID.String() || response.Status != "JOB_STATUS_PENDING" {
						t.Fatalf("response = %d %+v", status, value)
					}
					return failure
				})
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"pair":"eur/mxn"}`))
			request.Header.Set("Idempotency-Key", "key-1")
			request.Header.Set("X-Request-ID", "request-1")
			middleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := h.RequestUpdate(w, r); !errors.Is(err, failure) {
					t.Fatalf("error = %v", err)
				}
			})).ServeHTTP(httptest.NewRecorder(), request)
			if wrote == failUseCase {
				t.Fatalf("response writer called = %v", wrote)
			}
		})
	}
}
