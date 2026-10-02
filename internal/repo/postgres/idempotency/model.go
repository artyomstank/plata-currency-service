package idempotency

import "database/sql"

type responseModel struct {
	StatusCode sql.NullInt64
	Header     []byte
	Body       []byte
}
