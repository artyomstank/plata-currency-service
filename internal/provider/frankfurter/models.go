package frankfurter

import "encoding/json"

type latestRequest struct {
	Base  string
	Quote string
}

type latestResponse struct {
	Date  string                 `json:"date"`
	Base  string                 `json:"base"`
	Rates map[string]json.Number `json:"rates"`
}
