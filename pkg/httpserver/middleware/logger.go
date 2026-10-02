package middleware

import (
	"log/slog"
	"net/http"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

func Logger(log *slog.Logger) func(http.Handler) http.Handler {
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
						status = http.StatusInternalServerError
					}
				}
				log.InfoContext(r.Context(), "http request",
					"request_id", RequestIDFromContext(r.Context()),
					"method", r.Method, "path", r.URL.Path,
					"status", status, "duration", time.Since(startedAt),
				)
			}()
			next.ServeHTTP(recorder, r)
			completed = true
		})
	}
}
