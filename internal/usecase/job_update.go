package usecase

import (
	"time"

	"github.com/google/uuid"
)

type JobUpdate struct {
	ExpectedLeaseToken uuid.UUID
	LeaseUntil         time.Time
	NextAttemptAt      time.Time
}
