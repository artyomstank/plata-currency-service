package job

import (
	"time"

	"github.com/google/uuid"
)

type jobModel struct {
	ID             uuid.UUID
	Pair           string
	IdempotencyKey string
	Status         string
	ErrorMessage   string
	Attempts       int
	LeaseToken     uuid.UUID
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
