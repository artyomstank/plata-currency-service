// Package http exposes the quote API over HTTP/JSON.
package http

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"currency-quotes/internal/domain"
	"currency-quotes/internal/usecase"
)

// QuotesUseCase describes the application operations needed by this transport.
type QuotesUseCase interface {
	RequestUpdate(context.Context, usecase.RequestQuoteUpdateInput) (*usecase.RequestUpdateResult, error)
	GetJobResult(context.Context, usecase.GetQuoteUpdateInput) (*usecase.JobResult, error)
	GetLatest(context.Context, usecase.GetLatestQuoteInput) (*domain.Quote, error)
}

func New(uc QuotesUseCase, log *slog.Logger, ready func(context.Context) error, timeout time.Duration) http.Handler {
	h := &Handler{uc: uc, log: log, ready: ready}
	errors := &ErrorHandler{log: log}
	router := chi.NewRouter()
	router.Use(
		recovererMiddleware(log, errors),
		requestIDMiddleware,
		requestLoggerMiddleware(log),
		requestTimeoutMiddleware(timeout),
		maxBodyMiddleware,
	)

	router.Post("/v1/quote-updates", errors.Adapt(h.requestUpdate))
	router.Get("/v1/quote-updates/{job_id}", errors.Adapt(h.getUpdate))
	router.Head("/v1/quote-updates/{job_id}", errors.Adapt(h.getUpdate))
	router.Get("/v1/quotes/latest", errors.Adapt(h.getLatest))
	router.Head("/v1/quotes/latest", errors.Adapt(h.getLatest))
	router.Get("/healthz", errors.Adapt(h.health))
	router.Head("/healthz", errors.Adapt(h.health))
	router.Get("/readyz", errors.Adapt(h.readiness))
	router.Head("/readyz", errors.Adapt(h.readiness))
	router.NotFound(func(w http.ResponseWriter, r *http.Request) {
		errors.Handle(w, r, errRouteNotFound)
	})
	router.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		// A custom chi 405 handler must supply Allow itself.
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
