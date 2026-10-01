package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

func Recoverer(log *slog.Logger, onPanic func(http.ResponseWriter, *http.Request)) func(http.Handler) http.Handler {
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
					log.ErrorContext(r.Context(), "HTTP panic", "request_id", recorder.Header().Get("X-Request-ID"), "panic", value, "stack", string(debug.Stack()))
					onPanic(recorder, r)
				}
			}()
			next.ServeHTTP(recorder, r)
		})
	}
}
