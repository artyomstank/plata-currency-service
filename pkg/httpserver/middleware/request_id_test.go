package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestIDMiddlewarePropagatesGeneratedID(t *testing.T) {
	var receivedHeader string
	var receivedContext string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeader = r.Header.Get("X-Request-ID")
		receivedContext = RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	RequestID(next).ServeHTTP(recorder, request)

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
