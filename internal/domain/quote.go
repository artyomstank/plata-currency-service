package domain

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var (
	ErrInvalidCurrencies = errors.New("invalid allowed currencies")
	ErrInvalidPair       = errors.New("invalid currency pair")
	ErrInvalidQuote      = errors.New("invalid quote")
	ErrInvalidQuoteID    = errors.New("invalid quote ID")
)

// QuoteID identifies a quote independently of job identities.
type QuoteID uuid.UUID

func NewQuoteID() QuoteID {
	return QuoteID(uuid.New())
}

func ParseQuoteID(raw string) (QuoteID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return QuoteID{}, fmt.Errorf("%w: %w", ErrInvalidQuoteID, err)
	}
	if id == uuid.Nil {
		return QuoteID{}, ErrInvalidQuoteID
	}
	return QuoteID(id), nil
}

func (id QuoteID) String() string {
	return uuid.UUID(id).String()
}

// Quote is the result of one job. A price is stored as decimal, not float.
// SourceTime is the provider's rate date; CreatedAt is the result creation time.
// Its fields remain public; construction and validation enforce domain rules.
type Quote struct {
	ID         QuoteID
	JobID      JobID
	Pair       string
	Price      decimal.Decimal
	SourceTime time.Time
	CreatedAt  time.Time
}

// QuoteValue keeps existing repository, worker and transport contracts working.
// New domain code uses the shorter Quote name.
type QuoteValue = Quote

func NewQuote(jobID JobID, rawPair string, price decimal.Decimal, sourceTime time.Time, allowedCurrencies []string) (*Quote, error) {
	pair, err := NormalizePair(rawPair, allowedCurrencies)
	if err != nil {
		return nil, err
	}
	quote := &Quote{
		ID: NewQuoteID(), JobID: jobID, Pair: pair, Price: price,
		SourceTime: sourceTime.UTC(), CreatedAt: time.Now().UTC(),
	}
	if err := quote.Validate(); err != nil {
		return nil, err
	}
	return quote, nil
}

// Validate also guards completion against a quote assembled by legacy code
// without using NewQuote. It checks intrinsic rules, not the current admission
// list: changing configuration must not invalidate an existing result.
func (q *Quote) Validate() error {
	if q == nil {
		return fmt.Errorf("%w: quote must not be nil", ErrInvalidQuote)
	}
	if q.ID == (QuoteID{}) || q.JobID == (JobID{}) {
		return fmt.Errorf("%w: quote ID and job ID must not be empty", ErrInvalidQuote)
	}
	pair, err := normalizePair(q.Pair)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidQuote, err)
	}
	if pair != q.Pair {
		return fmt.Errorf("%w: pair must be normalized", ErrInvalidQuote)
	}
	if !q.Price.IsPositive() {
		return fmt.Errorf("%w: price must be positive", ErrInvalidQuote)
	}
	if q.SourceTime.IsZero() || q.CreatedAt.IsZero() {
		return fmt.Errorf("%w: source and creation time must not be empty", ErrInvalidQuote)
	}
	return nil
}

// NormalizeCurrencies validates and normalizes the configured admission list.
// It returns its own slice; callers cannot change it by mutating the input.
func NormalizeCurrencies(codes []string) ([]string, error) {
	currencies := make([]string, 0, len(codes))
	for _, raw := range codes {
		code := strings.ToUpper(strings.TrimSpace(raw))
		if !validCurrencyCode(code) {
			return nil, fmt.Errorf("%w: %q must contain three ASCII letters", ErrInvalidCurrencies, raw)
		}
		if !slices.Contains(currencies, code) {
			currencies = append(currencies, code)
		}
	}
	if len(currencies) < 2 {
		return nil, fmt.Errorf("%w: at least two distinct currencies are required", ErrInvalidCurrencies)
	}
	return currencies, nil
}

// NormalizePair applies the configured admission list to a currency pair.
// The domain receives this list explicitly and never reads environment variables.
func NormalizePair(raw string, allowedCurrencies []string) (string, error) {
	pair, err := normalizePair(raw)
	if err != nil {
		return "", err
	}
	base, quote, _ := strings.Cut(pair, "/")
	if !slices.Contains(allowedCurrencies, base) || !slices.Contains(allowedCurrencies, quote) {
		return "", fmt.Errorf("%w: supported currencies are %s", ErrInvalidPair, strings.Join(allowedCurrencies, ", "))
	}
	return pair, nil
}

// Stored results remain valid if the configured admission list later changes.
func normalizePair(raw string) (string, error) {
	parts := strings.Split(strings.ToUpper(strings.TrimSpace(raw)), "/")
	if len(parts) != 2 {
		return "", fmt.Errorf("%w: expected BASE/QUOTE", ErrInvalidPair)
	}
	base, quote := parts[0], parts[1]
	if !validCurrencyCode(base) || !validCurrencyCode(quote) {
		return "", fmt.Errorf("%w: currency codes must contain three ASCII letters", ErrInvalidPair)
	}
	if base == quote {
		return "", fmt.Errorf("%w: base and quote currency must differ", ErrInvalidPair)
	}
	return base + "/" + quote, nil
}

func validCurrencyCode(code string) bool {
	if len(code) != 3 {
		return false
	}
	for i := range code {
		if code[i] < 'A' || code[i] > 'Z' {
			return false
		}
	}
	return true
}
