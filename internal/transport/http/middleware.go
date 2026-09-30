package http

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

type contextKey string

const requestIDKey contextKey = "request-id"
const maxRequestBody = 1 << 20

func recovererMiddleware(log *slog.Logger, errors *ErrorHandler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			recorder := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
			defer func() {
				if value := recover(); value != nil {
					if value == http.ErrAbortHandler {
						panic(value)
					}
					if recorder.Header().Get("X-Request-ID") == "" {
						recorder.Header().Set("X-Request-ID", uuid.NewString())
					}
					log.ErrorContext(r.Context(), "HTTP panic", "request_id", responseRequestID(recorder, r), "panic", value, "stack", string(debug.Stack()))
					errors.Handle(recorder, r, errPanic)
				}
			}()
			next.ServeHTTP(recorder, r)
		})
	}
}

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" || len(requestID) > 128 {
			requestID = uuid.NewString()
		}
		r.Header.Set("X-Request-ID", requestID)
		w.Header().Set("X-Request-ID", requestID)
		ctx := context.WithValue(r.Context(), requestIDKey, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func requestLoggerMiddleware(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			startedAt := time.Now()
			recorder, ok := w.(chimiddleware.WrapResponseWriter)
			if !ok {
				recorder = chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
			}
			completed := false
			defer func() {
				status := recorder.Status()
				if status == 0 {
					status = http.StatusOK
					if !completed {
						// The outer recoverer will render 500 after stack unwinding.
						status = http.StatusInternalServerError
					}
				}
				log.InfoContext(r.Context(), "http request",
					"request_id", requestIDFromContext(r.Context()),
					"method", r.Method, "path", r.URL.Path,
					"status", status, "duration", time.Since(startedAt),
				)
			}()
			next.ServeHTTP(recorder, r)
			completed = true
		})
	}
}

func requestTimeoutMiddleware(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func maxBodyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
		}
		next.ServeHTTP(w, r)
	})
}

func requestIDFromContext(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDKey).(string)
	return requestID
}
