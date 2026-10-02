package http

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"currency-quotes/internal/domain"
	"currency-quotes/internal/transport/http/handler"
	"currency-quotes/internal/usecase"
	"currency-quotes/pkg/httpserver/middleware"
)

var (
	errRouteNotFound    = errors.New("route not found")
	errMethodNotAllowed = errors.New("method not allowed")
	errPanic            = errors.New("HTTP handler panic")
)

type errorResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId,omitempty"`
}

type handlerFunc func(http.ResponseWriter, *http.Request) error

type ErrorHandler struct {
	log *slog.Logger
}

func (h *ErrorHandler) Adapt(handler handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := handler(w, r); err != nil {
			h.Handle(w, r, err)
		}
	}
}

func (h *ErrorHandler) Handle(w http.ResponseWriter, r *http.Request, err error) {
	status, code, message := http.StatusInternalServerError, "INTERNAL_ERROR", "internal error"
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		status, code, message = http.StatusRequestEntityTooLarge, "INVALID_ARGUMENT", "request body is too large"
	case errors.Is(err, handler.ErrInvalidJSON):
		status, code, message = http.StatusBadRequest, "INVALID_ARGUMENT", "invalid JSON request body"
	case errors.Is(err, domain.ErrInvalidJobID):
		status, code, message = http.StatusBadRequest, "INVALID_ARGUMENT", "invalid job_id"
	case errors.Is(err, domain.ErrInvalidPair), errors.Is(err, middleware.ErrInvalidIdempotencyKey):
		status, code, message = http.StatusBadRequest, "INVALID_ARGUMENT", err.Error()
	case errors.Is(err, usecase.ErrJobNotFound):
		status, code, message = http.StatusNotFound, "NOT_FOUND", "job not found"
	case errors.Is(err, usecase.ErrQuoteNotFound):
		status, code, message = http.StatusNotFound, "NOT_FOUND", "no quote for pair"
	case errors.Is(err, errRouteNotFound):
		status, code, message = http.StatusNotFound, "NOT_FOUND", "route not found"
	case errors.Is(err, errMethodNotAllowed):
		status, code, message = http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed"
	case errors.Is(err, context.DeadlineExceeded):
		status, code, message = http.StatusGatewayTimeout, "GATEWAY_TIMEOUT", "request timed out"
	case errors.Is(err, errPanic):
	default:
		h.log.ErrorContext(r.Context(), "HTTP request failed", "request_id", responseRequestID(w, r), "err", err)
	}
	if recorder, ok := w.(chimiddleware.WrapResponseWriter); ok && recorder.Status() != 0 {
		return
	}
	if err := writeJSON(w, r, status, errorResponse{Code: code, Message: message, RequestID: responseRequestID(w, r)}); err != nil {
		h.log.WarnContext(r.Context(), "write HTTP error response failed", "err", err)
	}
}

func writeJSON(w http.ResponseWriter, r *http.Request, status int, value any) error {
	if status < http.StatusBadRequest {
		if err := r.Context().Err(); err != nil {
			return err
		}
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, err = w.Write(append(data, '\n'))
	return err
}

func responseRequestID(w http.ResponseWriter, r *http.Request) string {
	if id := middleware.RequestIDFromContext(r.Context()); id != "" {
		return id
	}
	return w.Header().Get("X-Request-ID")
}
