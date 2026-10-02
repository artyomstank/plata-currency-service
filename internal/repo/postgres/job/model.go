package job

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
)

type jobModel struct {
	ID            uuid.UUID
	Pair          string
	Status        string
	ErrorMessage  sql.NullString
	Attempts      int
	LeaseUntil    *time.Time
	NextAttemptAt time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
