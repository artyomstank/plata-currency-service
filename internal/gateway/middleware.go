package gateway

import (
	"log/slog"
	"net/http"
	"time"
)

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"duration", time.Since(start),
		)
	})
}

// authMiddleware — заготовка под будущую аутентификацию на шлюзе.
// Добавляется без изменений в бизнес-логике сервиса.
//
// func authMiddleware(next http.Handler) http.Handler {
// 	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
// 		token := r.Header.Get("Authorization")
// 		if token == "" {
// 			http.Error(w, "unauthorized", http.StatusUnauthorized)
// 			return
// 		}
// 		next.ServeHTTP(w, r)
// 	})
// }
