package middleware

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRecovererPreservesHTTPAbortHandler(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := Recoverer(log, func(http.ResponseWriter, *http.Request) { t.Fatal("abort handler triggered panic callback") })(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
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
