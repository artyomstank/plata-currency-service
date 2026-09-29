// Package provider отвечает за получение курсов из внешнего источника.
package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/shopspring/decimal"
)

type RateProvider interface {
	// FetchRate возвращает цену пары base/quote и время котировки источника.
	FetchRate(ctx context.Context, base, quote string) (decimal.Decimal, time.Time, error)
}

// ExchangeRatesAPIProvider — пример реализации поверх exchangeratesapi.io.
// Бесплатный тариф отдаёт котировки только с базой EUR, поэтому для пар без
// EUR цена считается как кросс-курс через EUR (rates[quote] / rates[base]).
type ExchangeRatesAPIProvider struct {
	BaseURL string
	APIKey  string
	Client  *http.Client
}

func NewExchangeRatesAPIProvider(baseURL, apiKey string) *ExchangeRatesAPIProvider {
	return &ExchangeRatesAPIProvider{
		BaseURL: baseURL,
		APIKey:  apiKey,
		Client:  &http.Client{Timeout: 5 * time.Second},
	}
}

type latestResponse struct {
	Success   bool               `json:"success"`
	Timestamp int64              `json:"timestamp"`
	Base      string             `json:"base"`
	Rates     map[string]float64 `json:"rates"`
	Error     *struct {
		Info string `json:"info"`
	} `json:"error"`
}

func (p *ExchangeRatesAPIProvider) FetchRate(ctx context.Context, base, quote string) (decimal.Decimal, time.Time, error) {
	url := fmt.Sprintf("%s/latest?access_key=%s&symbols=%s,%s", p.BaseURL, p.APIKey, base, quote)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return decimal.Zero, time.Time{}, err
	}

	resp, err := p.Client.Do(req)
	if err != nil {
		return decimal.Zero, time.Time{}, err
	}
	defer resp.Body.Close()

	var data latestResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return decimal.Zero, time.Time{}, err
	}
	if !data.Success {
		msg := "unknown provider error"
		if data.Error != nil {
			msg = data.Error.Info
		}
		return decimal.Zero, time.Time{}, fmt.Errorf("provider error: %s", msg)
	}

	rateTime := time.Unix(data.Timestamp, 0).UTC()

	baseRate, ok := data.Rates[base]
	if base == data.Base {
		baseRate, ok = 1, true
	}
	quoteRate, okQ := data.Rates[quote]
	if !ok || !okQ {
		return decimal.Zero, time.Time{}, fmt.Errorf("no rate for %s/%s", base, quote)
	}

	price := decimal.NewFromFloat(quoteRate).Div(decimal.NewFromFloat(baseRate))
	return price, rateTime, nil
}
