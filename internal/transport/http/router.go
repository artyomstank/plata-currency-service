package http

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"currency-quotes/internal/transport/http/handler"
	"currency-quotes/pkg/httpserver/middleware"
)

type Config struct {
	RequestTimeout time.Duration
	MaxBodyBytes   int64
}

func New(uc *handler.UseCases, log *slog.Logger, ready func(context.Context) error, cfg Config, store middleware.IdempotencyStore, tx middleware.TransactionManager) http.Handler {
	h := handler.New(uc, log, ready, writeJSON)
	errors := &ErrorHandler{log: log}
	router := chi.NewRouter()
	router.Use(
		middleware.Recoverer(log, func(w http.ResponseWriter, r *http.Request) { errors.Handle(w, r, errPanic) }),
		middleware.RequestID,
		middleware.Logger(log),
		middleware.Timeout(cfg.RequestTimeout),
		middleware.BodyLimit(cfg.MaxBodyBytes),
	)

	router.With(middleware.Idempotency(store, tx, errors.Handle)).Post("/v1/quote-updates", errors.Adapt(h.RequestUpdate))
	router.Get("/v1/quote-updates/{job_id}", errors.Adapt(h.GetUpdate))
	router.Head("/v1/quote-updates/{job_id}", errors.Adapt(h.GetUpdate))
	router.Get("/v1/quotes/latest", errors.Adapt(h.GetLatest))
	router.Head("/v1/quotes/latest", errors.Adapt(h.GetLatest))
	router.Get("/healthz", errors.Adapt(h.Health))
	router.Head("/healthz", errors.Adapt(h.Health))
	router.Get("/readyz", errors.Adapt(h.Readiness))
	router.Head("/readyz", errors.Adapt(h.Readiness))
	router.NotFound(func(w http.ResponseWriter, r *http.Request) {
		errors.Handle(w, r, errRouteNotFound)
	})
	router.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		var allowed []string
		path := r.URL.RawPath
		if path == "" {
			path = r.URL.Path
		}
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
			if router.Match(chi.NewRouteContext(), method, path) {
				allowed = append(allowed, method)
			}
		}
		if len(allowed) > 0 {
			w.Header().Set("Allow", strings.Join(allowed, ", "))
		}
		errors.Handle(w, r, errMethodNotAllowed)
	})
	return router
}
