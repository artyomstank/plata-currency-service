package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestNewQuote(t *testing.T) {
	jobID := NewJobID()
	price := decimal.RequireFromString("19.123456789012345678")
	sourceTime := time.Date(2026, 9, 30, 3, 0, 0, 0, time.FixedZone("MSK", 3*60*60))
	quote, err := NewQuote(jobID, " eur/mxn ", price, sourceTime, testCurrencies)
	if err != nil {
		t.Fatal(err)
	}
	if quote.ID == (QuoteID{}) || quote.JobID != jobID || quote.Pair != "EUR/MXN" || !quote.Price.Equal(price) {
		t.Fatalf("unexpected quote: %+v", quote)
	}
	if !quote.SourceTime.Equal(sourceTime) || quote.SourceTime.Location() != time.UTC || quote.CreatedAt.IsZero() || quote.CreatedAt.Location() != time.UTC {
		t.Fatalf("unexpected timestamps: source=%s created=%s", quote.SourceTime, quote.CreatedAt)
	}
}

func TestNewQuoteRejectsInvalidInput(t *testing.T) {
	for _, tc := range []struct {
		name, pair string
		jobID      JobID
		price      decimal.Decimal
		sourceTime time.Time
		want       error
	}{
		{"empty job ID", "EUR/MXN", JobID{}, decimal.NewFromInt(1), time.Now(), ErrInvalidQuote},
		{"zero price", "EUR/MXN", NewJobID(), decimal.Zero, time.Now(), ErrInvalidQuote},
		{"negative price", "EUR/MXN", NewJobID(), decimal.NewFromInt(-1), time.Now(), ErrInvalidQuote},
		{"missing date", "EUR/MXN", NewJobID(), decimal.NewFromInt(1), time.Time{}, ErrInvalidQuote},
		{"invalid pair", "EUR/GBP", NewJobID(), decimal.NewFromInt(1), time.Now(), ErrInvalidPair},
	} {
		t.Run(tc.name, func(t *testing.T) {
			quote, err := NewQuote(tc.jobID, tc.pair, tc.price, tc.sourceTime, testCurrencies)
			if quote != nil || !errors.Is(err, tc.want) {
				t.Fatalf("NewQuote = %v, error = %v, want %v", quote, err, tc.want)
			}
		})
	}
}

func TestNormalizePair(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{" eur/mxn ", "EUR/MXN"}, {"USD/EUR", "USD/EUR"}, {"MXN/USD", "MXN/USD"},
	} {
		got, err := NormalizePair(tc.raw, testCurrencies)
		if err != nil || got != tc.want {
			t.Fatalf("NormalizePair(%q) = %q, %v", tc.raw, got, err)
		}
	}
	for _, raw := range []string{"", "EURMXN", "EUR/MXN/USD", "EUR/GBP", "USD/USD", "EUR /MXN"} {
		if _, err := NormalizePair(raw, testCurrencies); !errors.Is(err, ErrInvalidPair) {
			t.Fatalf("NormalizePair(%q) error = %v", raw, err)
		}
	}
}

func TestQuoteIDRoundTrip(t *testing.T) {
	id := NewQuoteID()
	parsed, err := ParseQuoteID(id.String())
	if err != nil || parsed != id || id == (QuoteID{}) {
		t.Fatalf("quote ID round trip: id=%s parsed=%s error=%v", id, parsed, err)
	}
}

func TestParseQuoteIDRejectsInvalidValues(t *testing.T) {
	for _, raw := range []string{"", "not-a-uuid", "00000000-0000-0000-0000-000000000000"} {
		id, err := ParseQuoteID(raw)
		if !errors.Is(err, ErrInvalidQuoteID) || id != (QuoteID{}) {
			t.Fatalf("ParseQuoteID(%q) = %s, %v", raw, id, err)
		}
	}
}

func TestConfiguredCurrenciesApplyToJobsAndQuotes(t *testing.T) {
	currencies, err := NormalizeCurrencies([]string{"chf", " jpy "})
	if err != nil {
		t.Fatal(err)
	}
	job, err := NewJob(" chf/jpy ", currencies)
	if err != nil || job.Pair != "CHF/JPY" {
		t.Fatalf("configured job = %v, error = %v", job, err)
	}
	quote, err := NewQuote(job.ID, job.Pair, decimal.NewFromInt(100), time.Now(), currencies)
	if err != nil {
		t.Fatal(err)
	}
	if err := job.Start(testLeaseUntil); err != nil {
		t.Fatal(err)
	}
	if err := job.Complete(quote); err != nil {
		t.Fatalf("complete configured pair: %v", err)
	}
	if _, err := NewJob("EUR/USD", currencies); !errors.Is(err, ErrInvalidPair) {
		t.Fatalf("unconfigured job pair error = %v", err)
	}
	if _, err := NewQuote(job.ID, "EUR/USD", decimal.NewFromInt(1), time.Now(), currencies); !errors.Is(err, ErrInvalidPair) {
		t.Fatalf("unconfigured quote pair error = %v", err)
	}
	if _, err := NormalizePair("CHF/JPY", []string{"EUR", "USD"}); !errors.Is(err, ErrInvalidPair) {
		t.Fatalf("changed admission list error = %v", err)
	}
	if err := quote.Validate(); err != nil {
		t.Fatalf("existing quote invalidated by admission policy: %v", err)
	}
}

func TestNormalizeCurrenciesRejectsInvalidList(t *testing.T) {
	for _, codes := range [][]string{nil, {}, {"USD"}, {"USD", "usd"}, {"USD", ""}, {"USD", "US1"}, {"USD", "USDT"}, {"USD", "руб"}} {
		if _, err := NormalizeCurrencies(codes); !errors.Is(err, ErrInvalidCurrencies) {
			t.Fatalf("NormalizeCurrencies(%v) error = %v", codes, err)
		}
	}
}
