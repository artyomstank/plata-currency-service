package handler

import (
	"time"

	"currency-quotes/internal/domain"
	"currency-quotes/internal/usecase"
)

func toRequestUpdateInput(request updateRequest, idempotencyKey string) usecase.RequestQuoteUpdateInput {
	return usecase.RequestQuoteUpdateInput{Pair: request.Pair, IdempotencyKey: idempotencyKey}
}

func toGetUpdateInput(rawID string) (usecase.GetQuoteUpdateInput, error) {
	id, err := domain.ParseJobID(rawID)
	if err != nil {
		return usecase.GetQuoteUpdateInput{}, err
	}
	return usecase.GetQuoteUpdateInput{JobID: id}, nil
}

func toGetLatestInput(pair string) usecase.GetLatestQuoteInput {
	return usecase.GetLatestQuoteInput{Pair: pair}
}

func toUpdateResponse(result *usecase.RequestUpdateResult) updateResponse {
	return updateResponse{JobID: result.Job.ID.String(), Status: publicStatus(result.Job.Status)}
}

func toJobResponse(result *usecase.JobResult) jobResponse {
	response := jobResponse{
		updateResponse: updateResponse{JobID: result.Job.ID.String(), Status: publicStatus(result.Job.Status)},
		Pair:           result.Job.Pair,
	}
	if result.Job.Status == domain.JobStatusFailed {
		message := result.Job.ErrorMessage
		response.ErrorMessage = &message
	}
	if result.Value != nil {
		price := result.Value.Price.String()
		updatedAt := result.Value.CreatedAt.UTC().Format(time.RFC3339Nano)
		response.Price = &price
		response.UpdatedAt = &updatedAt
	}
	return response
}

func toLatestResponse(value *domain.Quote) latestResponse {
	return latestResponse{
		Pair: value.Pair, Price: value.Price.String(),
		UpdatedAt: value.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func publicStatus(status domain.JobStatus) string {
	switch status {
	case domain.JobStatusPending:
		return "JOB_STATUS_PENDING"
	case domain.JobStatusProcessing:
		return "JOB_STATUS_PROCESSING"
	case domain.JobStatusDone:
		return "JOB_STATUS_DONE"
	case domain.JobStatusFailed:
		return "JOB_STATUS_FAILED"
	default:
		return "JOB_STATUS_UNSPECIFIED"
	}
}
