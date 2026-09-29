package provider

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestHTTPProviderFetchRate(t *testing.T) {
	t.Parallel()

	provider := NewHTTPProvider("https://rates.test", time.Second)
	provider.Client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
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

	price, rateTime, err := provider.FetchRate(context.Background(), "EUR", "MXN")
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

func TestHTTPProviderRejectsNonSuccessStatus(t *testing.T) {
	t.Parallel()

	provider := NewHTTPProvider("https://rates.test", time.Second)
	provider.Client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Body:       io.NopCloser(strings.NewReader("temporarily unavailable")),
			Request:    r,
		}, nil
	})

	_, _, err := provider.FetchRate(context.Background(), "EUR", "MXN")
	if err == nil {
		t.Fatal("FetchRate() error = nil, want provider status error")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
