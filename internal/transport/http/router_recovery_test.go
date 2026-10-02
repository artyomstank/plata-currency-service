package http

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"currency-quotes/pkg/httpserver/middleware"
)

func TestRecovererIsOutermostAndCatchesMiddlewarePanic(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	errors := &ErrorHandler{log: log}
	router := chi.NewRouter()
	router.Use(middleware.Recoverer(log, func(w http.ResponseWriter, r *http.Request) { errors.Handle(w, r, errPanic) }))
	router.Use(func(http.Handler) http.Handler {
		return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic("private middleware panic")
		})
	})
	router.Get("/", func(http.ResponseWriter, *http.Request) { t.Fatal("handler reached after panic") })
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	var response errorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusInternalServerError || response.Code != "INTERNAL_ERROR" || response.Message != "internal error" {
		t.Fatalf("panic response = %d %v", recorder.Code, response)
	}
	if response.RequestID == "" || response.RequestID != recorder.Header().Get("X-Request-ID") {
		t.Fatalf("panic request ID missing: %v", response)
	}
}

func TestRecovererDoesNotRewriteCommittedResponse(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := middleware.Recoverer(log, func(w http.ResponseWriter, r *http.Request) { (&ErrorHandler{log: log}).Handle(w, r, errPanic) })(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("response started"))
		panic("private panic after headers")
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusAccepted || recorder.Body.String() != "response started" {
		t.Fatalf("committed response rewritten: %d %s", recorder.Code, recorder.Body.String())
	}
}
