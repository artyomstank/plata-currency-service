package frankfurter

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"currency-quotes/pkg/httpclient"
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
			client, err := NewClient(ClientConfig{BaseURL: "https://rates.test"}, httpClient)
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
	httpClient := httpclient.New(httpclient.Config{Timeout: time.Second},
		httpclient.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)), "Frankfurter HTTP request"),
		httpclient.Headers(http.Header{"Accept": {"application/json"}, "User-Agent": {"currency-service/frankfurter"}}),
	)
	defer httpClient.CloseIdleConnections()
	client, err := NewClient(ClientConfig{BaseURL: source.URL + "/api"}, httpClient)
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
