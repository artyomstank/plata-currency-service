package idempotency

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"currency-quotes/pkg/httpserver/middleware"
	"currency-quotes/pkg/postgres"
)

type Repository struct{}

func New() *Repository { return &Repository{} }

func (r *Repository) Lock(ctx context.Context, key string) (*middleware.StoredResponse, error) {
	tx, err := postgres.RequireTransaction(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO http_idempotency (key) VALUES ($1) ON CONFLICT (key) DO NOTHING`, key); err != nil {
		return nil, fmt.Errorf("reserve idempotency key: %w", err)
	}
	var model responseModel
	if err := tx.QueryRow(ctx, `SELECT status_code, response_headers, response_body FROM http_idempotency WHERE key = $1 FOR UPDATE`, key).Scan(&model.StatusCode, &model.Header, &model.Body); err != nil {
		return nil, fmt.Errorf("lock idempotency key: %w", err)
	}
	return model.toResponse()
}

func (r *Repository) Save(ctx context.Context, key string, response *middleware.StoredResponse) error {
	tx, err := postgres.RequireTransaction(ctx)
	if err != nil {
		return err
	}
	header, err := json.Marshal(response.Header)
	if err != nil {
		return fmt.Errorf("encode idempotent response headers: %w", err)
	}
	result, err := tx.Exec(ctx, `UPDATE http_idempotency SET status_code = $2, response_headers = $3, response_body = $4 WHERE key = $1 AND status_code IS NULL`, key, response.StatusCode, header, response.Body)
	if err != nil {
		return fmt.Errorf("save idempotent response: %w", err)
	}
	if result.RowsAffected() != 1 {
		return errors.New("idempotency key is not reserved for this response")
	}
	return nil
}
