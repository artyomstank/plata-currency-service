package frankfurter

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

type rateClient interface {
	Latest(context.Context, latestRequest) (*latestResponse, error)
}

type Adapter struct{ client rateClient }

func NewAdapter(client rateClient) *Adapter { return &Adapter{client: client} }

func (a *Adapter) FetchRate(ctx context.Context, base, quote string) (decimal.Decimal, time.Time, error) {
	response, err := a.client.Latest(ctx, latestRequest{Base: base, Quote: quote})
	if err != nil {
		return decimal.Zero, time.Time{}, err
	}
	if response == nil {
		return decimal.Zero, time.Time{}, fmt.Errorf("Frankfurter returned an empty response")
	}
	sourceTime, err := time.Parse("2006-01-02", response.Date)
	if err != nil {
		return decimal.Zero, time.Time{}, fmt.Errorf("parse Frankfurter rate date: %w", err)
	}
	if response.Base != base {
		return decimal.Zero, time.Time{}, fmt.Errorf("Frankfurter returned base %q, expected %q", response.Base, base)
	}
	rate, exists := response.Rates[quote]
	if !exists {
		return decimal.Zero, time.Time{}, fmt.Errorf("Frankfurter response has no rate for %s", quote)
	}
	price, err := decimal.NewFromString(rate.String())
	if err != nil {
		return decimal.Zero, time.Time{}, fmt.Errorf("parse Frankfurter rate for %s: %w", quote, err)
	}
	if !price.IsPositive() {
		return decimal.Zero, time.Time{}, fmt.Errorf("Frankfurter returned invalid rate for %s", quote)
	}
	return price, sourceTime.UTC(), nil
}
