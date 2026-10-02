package idempotency

import (
	"encoding/json"
	"fmt"
	"net/http"

	"currency-quotes/pkg/httpserver/middleware"
)

func (m responseModel) toResponse() (*middleware.StoredResponse, error) {
	if !m.StatusCode.Valid {
		return nil, nil
	}
	var header http.Header
	if err := json.Unmarshal(m.Header, &header); err != nil {
		return nil, fmt.Errorf("decode idempotent response headers: %w", err)
	}
	return &middleware.StoredResponse{StatusCode: int(m.StatusCode.Int64), Header: header, Body: m.Body}, nil
}
