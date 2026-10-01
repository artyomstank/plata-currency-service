package handler

type updateRequest struct {
	Pair string `json:"pair"`
}

type updateResponse struct {
	JobID  string `json:"jobId"`
	Status string `json:"status"`
}

type jobResponse struct {
	updateResponse
	Pair         string  `json:"pair"`
	Price        *string `json:"price,omitempty"`
	UpdatedAt    *string `json:"updatedAt,omitempty"`
	ErrorMessage *string `json:"errorMessage,omitempty"`
}

type latestResponse struct {
	Pair      string `json:"pair"`
	Price     string `json:"price"`
	UpdatedAt string `json:"updatedAt"`
}

type healthResponse struct {
	Status string `json:"status"`
}
