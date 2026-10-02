package httpclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientHonoursCancellationAndTimeout(t *testing.T) {
	for _, cancelRequest := range []bool{false, true} {
		t.Run(map[bool]string{false: "client timeout", true: "context cancelled"}[cancelRequest], func(t *testing.T) {
			entered := make(chan struct{})
			cancelled := make(chan struct{})
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				<-r.Context().Done()
				close(cancelled)
			}))
			defer source.Close()
			timeout := time.Second
			if !cancelRequest {
				timeout = 30 * time.Millisecond
			}
			httpClient := New(Config{Timeout: timeout})
			defer httpClient.CloseIdleConnections()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				request, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL, nil)
				if err != nil {
					done <- err
					return
				}
				response, err := httpClient.Do(request)
				if response != nil {
					_ = response.Body.Close()
				}
				done <- err
			}()
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("request did not reach source")
			}
			expected := context.DeadlineExceeded
			if cancelRequest {
				cancel()
				expected = context.Canceled
			}
			select {
			case err := <-done:
				if !errors.Is(err, expected) {
					t.Fatalf("error=%v want=%v", err, expected)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("HTTP request was not interrupted")
			}
			select {
			case <-cancelled:
			case <-time.After(2 * time.Second):
				t.Fatal("source request context stayed active")
			}
		})
	}
}
