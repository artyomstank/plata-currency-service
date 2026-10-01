package usecase

import (
	"errors"

	"currency-quotes/internal/domain"
)

var (
	ErrInvalidPair         = domain.ErrInvalidPair
	ErrInvalidIdempotency  = domain.ErrInvalidIdempotency
	ErrIdempotencyConflict = errors.New("idempotency key is already used for another pair")
	ErrJobNotFound         = errors.New("quote update not found")
	ErrQuoteNotFound       = errors.New("quote not found")
)
