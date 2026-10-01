package httpclient

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMiddlewareLogsStatusWithoutResponseBody(t *testing.T) {
	var output bytes.Buffer
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		_, _ = w.Write([]byte("private source details"))
	}))
	defer source.Close()
	httpClient := New(Config{Timeout: time.Second}, Logging(slog.New(slog.NewJSONHandler(&output, nil)), "source request"))
	defer httpClient.CloseIdleConnections()
	response, err := httpClient.Get(source.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if !strings.Contains(output.String(), `"status":503`) || strings.Contains(output.String(), "private source details") {
		t.Fatalf("unexpected log: %s", output.String())
	}
}

func TestHeaderMiddlewareDoesNotMutateRequest(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "https://rates.test/latest", nil)
	if err != nil {
		t.Fatal(err)
	}
	transport := Headers(http.Header{"Accept": {"application/json"}})(roundTripFunc(func(got *http.Request) (*http.Response, error) {
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

type closingTransport struct {
	closed bool
}

func (t *closingTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, nil }
func (t *closingTransport) CloseIdleConnections()                           { t.closed = true }

func TestMiddlewarePreservesCloseIdleConnections(t *testing.T) {
	transport := &closingTransport{}
	client := &http.Client{Transport: Logging(slog.New(slog.NewTextHandler(io.Discard, nil)), "source request")(
		Headers(http.Header{"Accept": {"application/json"}})(transport),
	)}
	client.CloseIdleConnections()
	if !transport.closed {
		t.Fatal("idle connections were not closed through middleware chain")
	}
}

func TestHeadersCopiesConfigurationAndPreservesMultipleValues(t *testing.T) {
	headers := http.Header{"x-resource": {"first", "second"}}
	wrap := Headers(headers)
	headers["x-resource"][0] = "changed"
	request, err := http.NewRequest(http.MethodGet, "https://rates.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	transport := wrap(roundTripFunc(func(got *http.Request) (*http.Response, error) {
		values := got.Header.Values("X-Resource")
		if len(values) != 2 || values[0] != "first" || values[1] != "second" {
			t.Fatalf("headers = %v", got.Header)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))}, nil
	}))
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
}
