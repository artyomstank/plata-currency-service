package frankfurter

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientRejectsMalformedAndOversizedResponses(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"malformed", "{"},
		{"multiple JSON objects", `{"base":"EUR"} {}`},
		{"oversized trailing body", `{"base":"EUR"}` + strings.Repeat(" ", maxResponseBody)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.body)), Request: r}, nil
			})}
			client, err := NewClient("https://rates.test", httpClient)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Latest(context.Background(), latestRequest{"EUR", "MXN"}); err == nil {
				t.Fatal("invalid response accepted")
			}
		})
	}
}

func TestClientRequestUsesMiddlewareAndPreservesWirePrecision(t *testing.T) {
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/latest" || r.URL.Query().Get("from") != "EUR" || r.URL.Query().Get("to") != "MXN" {
			t.Errorf("unexpected request: %s", r.URL)
		}
		if r.Header.Get("Accept") != "application/json" || r.Header.Get("User-Agent") != "currency-service/frankfurter" {
			t.Errorf("missing HTTP middleware headers: %v", r.Header)
		}
		_, _ = w.Write([]byte(`{"date":"2026-10-01","base":"EUR","rates":{"MXN":20.123456789012345678}}`))
	}))
	defer source.Close()
	httpClient := NewHTTPClient(time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer httpClient.CloseIdleConnections()
	client, err := NewClient(source.URL+"/api", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Latest(context.Background(), latestRequest{"EUR", "MXN"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Rates["MXN"].String() != "20.123456789012345678" {
		t.Fatalf("precision lost: %+v", response)
	}
}

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
			httpClient := NewHTTPClient(timeout, slog.New(slog.NewTextHandler(io.Discard, nil)))
			defer httpClient.CloseIdleConnections()
			client, err := NewClient(source.URL, httpClient)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := client.Latest(ctx, latestRequest{"EUR", "MXN"}); done <- err }()
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

func TestMiddlewareLogsStatusWithoutResponseBody(t *testing.T) {
	var output bytes.Buffer
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		_, _ = w.Write([]byte("private source details"))
	}))
	defer source.Close()
	httpClient := NewHTTPClient(time.Second, slog.New(slog.NewJSONHandler(&output, nil)))
	defer httpClient.CloseIdleConnections()
	client, err := NewClient(source.URL, httpClient)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Latest(context.Background(), latestRequest{"EUR", "MXN"}); err == nil {
		t.Fatal("503 was accepted")
	}
	if !strings.Contains(output.String(), `"status":503`) || strings.Contains(output.String(), "private source details") {
		t.Fatalf("unexpected log: %s", output.String())
	}
}

func TestHeaderMiddlewareDoesNotMutateRequest(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "https://rates.test/latest", nil)
	if err != nil {
		t.Fatal(err)
	}
	transport := requestHeaders(roundTripFunc(func(got *http.Request) (*http.Response, error) {
		if got == request || got.Header.Get("Accept") != "application/json" {
			t.Fatal("middleware did not clone the request")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))}, nil
	}))
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if request.Header.Get("Accept") != "" {
		t.Fatal("caller request mutated")
	}
}
