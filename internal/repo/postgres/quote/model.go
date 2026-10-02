package quote

import (
	"time"

	"github.com/google/uuid"
)

type quoteModel struct {
	ID         uuid.UUID
	JobID      uuid.UUID
	Pair       string
	Price      string
	SourceTime time.Time
	CreatedAt  time.Time
}
