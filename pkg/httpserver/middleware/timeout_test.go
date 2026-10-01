package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTimeoutCancelsContextAndPreservesRequestID(t *testing.T) {
	handler := RequestID(Timeout(time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if RequestIDFromContext(r.Context()) != "request-1" {
			t.Fatal("request ID lost")
		}
		<-r.Context().Done()
		if !errors.Is(r.Context().Err(), context.DeadlineExceeded) {
			t.Fatalf("err = %v", r.Context().Err())
		}
	})))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Request-ID", "request-1")
	handler.ServeHTTP(httptest.NewRecorder(), request)
}
