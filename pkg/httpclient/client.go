package httpclient

import (
	"net/http"
	"time"
)

type Config struct {
	Timeout time.Duration
}

func New(cfg Config, chain ...Middleware) *http.Client {
	var transport http.RoundTripper = http.DefaultTransport.(*http.Transport).Clone()
	for i := len(chain) - 1; i >= 0; i-- {
		transport = chain[i](transport)
	}
	return &http.Client{Timeout: cfg.Timeout, Transport: transport}
}
