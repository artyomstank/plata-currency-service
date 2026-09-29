// Package provider отвечает за получение курсов из внешнего источника.
package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

type RateProvider interface {
	// FetchRate возвращает цену пары base/quote и время котировки источника.
	FetchRate(ctx context.Context, base, quote string) (decimal.Decimal, time.Time, error)
}

const maxResponseBody = 1 << 20

// HTTPProvider fetches rates from a Frankfurter-compatible HTTP API. The
// default endpoint does not require an API key and supports the limited
// currency set required by the task.
type HTTPProvider struct {
	BaseURL string
	Client  *http.Client
}

func NewHTTPProvider(baseURL string, timeout time.Duration) *HTTPProvider {
	return &HTTPProvider{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Client:  &http.Client{Timeout: timeout},
	}
}

type latestResponse struct {
	Date  string                 `json:"date"`
	Base  string                 `json:"base"`
	Rates map[string]json.Number `json:"rates"`
}

func (p *HTTPProvider) FetchRate(ctx context.Context, base, quote string) (decimal.Decimal, time.Time, error) {
	endpoint, err := url.Parse(p.BaseURL + "/latest")
	if err != nil {
		return decimal.Zero, time.Time{}, fmt.Errorf("parse provider URL: %w", err)
	}
	query := endpoint.Query()
	query.Set("from", base)
	query.Set("to", quote)
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return decimal.Zero, time.Time{}, fmt.Errorf("create provider request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := p.Client.Do(req)
	if err != nil {
		return decimal.Zero, time.Time{}, fmt.Errorf("request provider: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBody))
		return decimal.Zero, time.Time{}, fmt.Errorf("provider returned HTTP %d", resp.StatusCode)
	}

	var data latestResponse
	decoder := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBody))
	decoder.UseNumber()
	if err := decoder.Decode(&data); err != nil {
		return decimal.Zero, time.Time{}, fmt.Errorf("decode provider response: %w", err)
	}

	rateTime, err := time.Parse("2006-01-02", data.Date)
	if err != nil {
		return decimal.Zero, time.Time{}, fmt.Errorf("parse provider rate date: %w", err)
	}
	if data.Base != base {
		return decimal.Zero, time.Time{}, fmt.Errorf("provider returned base %q, expected %q", data.Base, base)
	}

	rate, ok := data.Rates[quote]
	if !ok {
		return decimal.Zero, time.Time{}, fmt.Errorf("provider response has no rate for %s", quote)
	}
	price, err := decimal.NewFromString(rate.String())
	if err != nil || !price.IsPositive() {
		return decimal.Zero, time.Time{}, fmt.Errorf("provider returned invalid rate for %s", quote)
	}
	return price, rateTime.UTC(), nil
}
