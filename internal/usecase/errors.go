package usecase

import (
	"errors"

	"currency-quotes/internal/domain"
)

var (
	ErrInvalidPair   = domain.ErrInvalidPair
	ErrJobNotFound   = errors.New("quote update not found")
	ErrQuoteNotFound = errors.New("quote not found")
)
