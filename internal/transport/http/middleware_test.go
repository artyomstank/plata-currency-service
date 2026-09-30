package http

import (
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestIDMiddlewarePropagatesGeneratedID(t *testing.T) {
	var receivedHeader string
	var receivedContext string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeader = r.Header.Get("X-Request-ID")
		receivedContext = requestIDFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	requestIDMiddleware(next).ServeHTTP(recorder, request)

	responseID := recorder.Header().Get("X-Request-ID")
	if responseID == "" {
		t.Fatal("response X-Request-ID is empty")
	}
	if receivedHeader != responseID {
		t.Fatalf("forwarded X-Request-ID = %q, want %q", receivedHeader, responseID)
	}
	if receivedContext != responseID {
		t.Fatalf("context request ID = %q, want %q", receivedContext, responseID)
	}
}

func TestRecovererIsOutermostAndCatchesMiddlewarePanic(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	errors := &ErrorHandler{log: log}
	router := chi.NewRouter()
	router.Use(recovererMiddleware(log, errors))
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
	handler := recovererMiddleware(log, &ErrorHandler{log: log})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func TestRecovererPreservesHTTPAbortHandler(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := recovererMiddleware(log, &ErrorHandler{log: log})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))
	defer func() {
		if value := recover(); value != http.ErrAbortHandler {
			t.Fatalf("abort panic = %v", value)
		}
	}()
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	t.Fatal("abort handler panic was swallowed")
}
