package frankfurter

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestAdapterFetchRate(t *testing.T) {
	t.Parallel()

	httpClient := &http.Client{Timeout: time.Second}
	httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.URL.Query().Get("from"); got != "EUR" {
			t.Errorf("from = %q, want EUR", got)
		}
		if got := r.URL.Query().Get("to"); got != "MXN" {
			t.Errorf("to = %q, want MXN", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader("{\"date\":\"2026-09-29\",\"base\":\"EUR\",\"rates\":{\"MXN\":20.123456789123}}")),
			Request:    r,
		}, nil
	})

	client, err := NewClient(ClientConfig{BaseURL: "https://rates.test"}, httpClient)
	if err != nil {
		t.Fatal(err)
	}
	adapter := NewAdapter(client)
	price, rateTime, err := adapter.FetchRate(context.Background(), "EUR", "MXN")
	if err != nil {
		t.Fatalf("FetchRate() unexpected error: %v", err)
	}
	wantPrice := decimal.RequireFromString("20.123456789123")
	if !price.Equal(wantPrice) {
		t.Fatalf("FetchRate() price = %s, want %s", price, wantPrice)
	}
	wantTime := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	if !rateTime.Equal(wantTime) {
		t.Fatalf("FetchRate() time = %s, want %s", rateTime, wantTime)
	}
}

func TestAdapterRejectsNonSuccessStatus(t *testing.T) {
	t.Parallel()

	httpClient := &http.Client{Timeout: time.Second}
	httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Body:       io.NopCloser(strings.NewReader("temporarily unavailable")),
			Request:    r,
		}, nil
	})

	client, err := NewClient(ClientConfig{BaseURL: "https://rates.test"}, httpClient)
	if err != nil {
		t.Fatal(err)
	}
	adapter := NewAdapter(client)
	_, _, err = adapter.FetchRate(context.Background(), "EUR", "MXN")
	if err == nil {
		t.Fatal("FetchRate() error = nil, want provider status error")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type rateClientFunc func(context.Context, latestRequest) (*latestResponse, error)

func (f rateClientFunc) Latest(ctx context.Context, input latestRequest) (*latestResponse, error) {
	return f(ctx, input)
}

func TestAdapterRejectsInvalidExternalModels(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response *latestResponse
	}{
		{"nil response", nil},
		{"invalid date", &latestResponse{Date: "invalid", Base: "EUR", Rates: map[string]json.Number{"MXN": "20"}}},
		{"wrong base", &latestResponse{Date: "2026-10-01", Base: "USD", Rates: map[string]json.Number{"MXN": "20"}}},
		{"missing rate", &latestResponse{Date: "2026-10-01", Base: "EUR", Rates: map[string]json.Number{}}},
		{"invalid price", &latestResponse{Date: "2026-10-01", Base: "EUR", Rates: map[string]json.Number{"MXN": "invalid"}}},
		{"zero price", &latestResponse{Date: "2026-10-01", Base: "EUR", Rates: map[string]json.Number{"MXN": "0"}}},
		{"negative price", &latestResponse{Date: "2026-10-01", Base: "EUR", Rates: map[string]json.Number{"MXN": "-1"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adapter := NewAdapter(rateClientFunc(func(context.Context, latestRequest) (*latestResponse, error) { return tc.response, nil }))
			if _, _, err := adapter.FetchRate(context.Background(), "EUR", "MXN"); err == nil {
				t.Fatal("invalid source model accepted")
			}
		})
	}
}

func TestAdapterPropagatesClientError(t *testing.T) {
	failure := errors.New("source unavailable")
	adapter := NewAdapter(rateClientFunc(func(context.Context, latestRequest) (*latestResponse, error) { return nil, failure }))
	if _, _, err := adapter.FetchRate(context.Background(), "EUR", "MXN"); !errors.Is(err, failure) {
		t.Fatalf("error=%v", err)
	}
}
