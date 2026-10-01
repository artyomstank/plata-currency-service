package frankfurter

import (
	"log/slog"
	"net/http"
	"time"
)

type middleware func(http.RoundTripper) http.RoundTripper

func NewHTTPClient(timeout time.Duration, log *slog.Logger) *http.Client {
	var transport http.RoundTripper = http.DefaultTransport.(*http.Transport).Clone()
	chain := []middleware{requestLogging(log), requestHeaders}
	for i := len(chain) - 1; i >= 0; i-- {
		transport = chain[i](transport)
	}
	return &http.Client{Timeout: timeout, Transport: transport}
}

type middlewareTransport struct {
	next    http.RoundTripper
	request func(*http.Request) (*http.Response, error)
}

func (t *middlewareTransport) RoundTrip(r *http.Request) (*http.Response, error) { return t.request(r) }
func (t *middlewareTransport) CloseIdleConnections() {
	if closer, ok := t.next.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func requestHeaders(next http.RoundTripper) http.RoundTripper {
	return &middlewareTransport{next: next, request: func(r *http.Request) (*http.Response, error) {
		clone := r.Clone(r.Context())
		clone.Header.Set("Accept", "application/json")
		clone.Header.Set("User-Agent", "currency-service/frankfurter")
		return next.RoundTrip(clone)
	}}
}

func requestLogging(log *slog.Logger) middleware {
	return func(next http.RoundTripper) http.RoundTripper {
		return &middlewareTransport{next: next, request: func(r *http.Request) (*http.Response, error) {
			started := time.Now()
			response, err := next.RoundTrip(r)
			status := 0
			if response != nil {
				status = response.StatusCode
			}
			level := slog.LevelDebug
			if (err != nil && r.Context().Err() == nil) || status >= http.StatusBadRequest {
				level = slog.LevelWarn
			}
			log.Log(r.Context(), level, "Frankfurter HTTP request", "method", r.Method, "status", status, "duration", time.Since(started), "err", err)
			return response, err
		}}
	}
}
