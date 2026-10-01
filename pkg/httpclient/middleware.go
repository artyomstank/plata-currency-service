package httpclient

import (
	"log/slog"
	"net/http"
	"time"
)

type Middleware func(http.RoundTripper) http.RoundTripper

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

func Headers(headers http.Header) Middleware {
	headers = headers.Clone()
	return func(next http.RoundTripper) http.RoundTripper {
		return &middlewareTransport{next: next, request: func(r *http.Request) (*http.Response, error) {
			clone := r.Clone(r.Context())
			for key, values := range headers {
				clone.Header[http.CanonicalHeaderKey(key)] = append([]string(nil), values...)
			}
			return next.RoundTrip(clone)
		}}
	}
}

func Logging(log *slog.Logger, message string) Middleware {
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
			log.Log(r.Context(), level, message, "method", r.Method, "status", status, "duration", time.Since(started), "err", err)
			return response, err
		}}
	}
}
