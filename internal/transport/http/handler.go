// Package http exposes the quote API directly over HTTP/JSON.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"currency-quotes/internal/domain"
	"currency-quotes/internal/usecase"
)

type Handler struct {
	uc  *usecase.QuotesUseCase
	log *slog.Logger
}

func New(uc *usecase.QuotesUseCase, log *slog.Logger, ready func(context.Context) error, timeout time.Duration) http.Handler {
	h := &Handler{uc: uc, log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/quote-updates", h.method(http.MethodPost, h.requestUpdate))
	mux.HandleFunc("/v1/quote-updates/{job_id}", h.method(http.MethodGet, h.getUpdate))
	mux.HandleFunc("/v1/quotes/latest", h.method(http.MethodGet, h.getLatest))
	mux.HandleFunc("/healthz", h.method(http.MethodGet, func(w http.ResponseWriter, r *http.Request) {
		h.writeJSON(w, r, http.StatusOK, map[string]string{"status": "ok"})
	}))
	mux.HandleFunc("/readyz", h.method(http.MethodGet, func(w http.ResponseWriter, r *http.Request) {
		if err := ready(r.Context()); err != nil {
			h.log.WarnContext(r.Context(), "readiness check failed", "err", err)
			h.writeJSON(w, r, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
			return
		}
		h.writeJSON(w, r, http.StatusOK, map[string]string{"status": "ready"})
	}))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		h.writeError(w, r, http.StatusNotFound, "NOT_FOUND", "route not found")
	})
	return withMiddlewares(log, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		mux.ServeHTTP(w, r.WithContext(ctx))
	}))
}

func (h *Handler) method(method string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method && !(method == http.MethodGet && r.Method == http.MethodHead) {
			allow := method
			if method == http.MethodGet {
				allow += ", HEAD"
			}
			w.Header().Set("Allow", allow)
			h.writeError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed")
			return
		}
		next(w, r)
	}
}

type updateResponse struct {
	JobID  string `json:"jobId"`
	Status string `json:"status"`
}

type jobResponse struct {
	updateResponse
	Pair         string  `json:"pair"`
	Price        *string `json:"price,omitempty"`
	UpdatedAt    *string `json:"updatedAt,omitempty"`
	ErrorMessage *string `json:"errorMessage,omitempty"`
}

type latestResponse struct {
	Pair      string `json:"pair"`
	Price     string `json:"price"`
	UpdatedAt string `json:"updatedAt"`
}

func (h *Handler) requestUpdate(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Pair string `json:"pair"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&request)
	if err == nil {
		var extra any
		if err = decoder.Decode(&extra); err == io.EOF {
			err = nil
		} else if err == nil {
			err = errors.New("multiple JSON values")
		}
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.writeError(w, r, http.StatusRequestEntityTooLarge, "INVALID_ARGUMENT", "request body is too large")
		} else {
			h.writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid JSON request body")
		}
		return
	}

	result, err := h.uc.RequestUpdate(r.Context(), request.Pair, r.Header.Get("Idempotency-Key"))
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	statusCode := http.StatusOK
	if result.Created {
		statusCode = http.StatusAccepted
	}
	h.writeJSON(w, r, statusCode, updateResponse{
		JobID: result.Job.ID.String(), Status: publicStatus(result.Job.Status),
	})
}

func (h *Handler) getUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := domain.ParseJobID(r.PathValue("job_id"))
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid job_id")
		return
	}
	result, err := h.uc.GetJobResult(r.Context(), id)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	response := jobResponse{
		updateResponse: updateResponse{JobID: result.Job.ID.String(), Status: publicStatus(result.Job.Status)},
		Pair:           result.Job.Pair,
	}
	if result.Job.Status == domain.JobStatusFailed {
		response.ErrorMessage = &result.Job.ErrorMessage
	}
	if result.Value != nil {
		price := result.Value.Price.String()
		updatedAt := result.Value.CreatedAt.UTC().Format(time.RFC3339Nano)
		response.Price = &price
		response.UpdatedAt = &updatedAt
	}
	h.writeJSON(w, r, http.StatusOK, response)
}

func (h *Handler) getLatest(w http.ResponseWriter, r *http.Request) {
	value, err := h.uc.GetLatest(r.Context(), r.URL.Query().Get("pair"))
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	h.writeJSON(w, r, http.StatusOK, latestResponse{
		Pair: value.Pair, Price: value.Price.String(),
		UpdatedAt: value.CreatedAt.UTC().Format(time.RFC3339Nano),
	})
}

func publicStatus(status domain.JobStatus) string {
	// Keep the existing HTTP response values after removing protobuf.
	switch status {
	case domain.JobStatusPending:
		return "JOB_STATUS_PENDING"
	case domain.JobStatusProcessing:
		return "JOB_STATUS_PROCESSING"
	case domain.JobStatusDone:
		return "JOB_STATUS_DONE"
	case domain.JobStatusFailed:
		return "JOB_STATUS_FAILED"
	default:
		return "JOB_STATUS_UNSPECIFIED"
	}
}

func (h *Handler) handleError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, usecase.ErrInvalidPair), errors.Is(err, usecase.ErrInvalidIdempotency):
		h.writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
	case errors.Is(err, usecase.ErrIdempotencyConflict):
		h.writeError(w, r, http.StatusConflict, "IDEMPOTENCY_CONFLICT", err.Error())
	case errors.Is(err, usecase.ErrJobNotFound):
		h.writeError(w, r, http.StatusNotFound, "NOT_FOUND", "job not found")
	case errors.Is(err, usecase.ErrQuoteNotFound):
		h.writeError(w, r, http.StatusNotFound, "NOT_FOUND", "no quote for pair")
	case errors.Is(err, context.DeadlineExceeded):
		// This code is retained for compatibility with existing HTTP clients.
		h.writeError(w, r, http.StatusGatewayTimeout, "GATEWAY_TIMEOUT", "request timed out")
	default:
		h.log.ErrorContext(r.Context(), "quote request failed", "err", err)
		h.writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error")
	}
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	h.writeJSON(w, r, status, struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"requestId,omitempty"`
	}{code, message, requestIDFromContext(r.Context())})
}

func (h *Handler) writeJSON(w http.ResponseWriter, r *http.Request, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		h.log.WarnContext(r.Context(), "write HTTP response failed", "err", err)
	}
}
