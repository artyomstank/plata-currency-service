// Package grpc адаптирует бизнес-логику (usecase) к сгенерированному
// gRPC-интерфейсу QuotesServiceServer. Зависимость от internal/genpb
// появится после `make generate`.
package grpc

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"currency-quotes/internal/domain"
	"currency-quotes/internal/storage"
	"currency-quotes/internal/usecase"

	quotesv1 "currency-quotes/internal/genpb/quotes/v1"
)

type Handler struct {
	quotesv1.UnimplementedQuotesServiceServer
	uc *usecase.QuotesUseCase
}

func New(uc *usecase.QuotesUseCase) *Handler {
	return &Handler{uc: uc}
}

func (h *Handler) UpdateQuote(ctx context.Context, req *quotesv1.UpdateQuoteRequest) (*quotesv1.UpdateQuoteResponse, error) {
	id, err := h.uc.RequestUpdate(ctx, req.GetPair())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return &quotesv1.UpdateQuoteResponse{
		JobId:  id.String(),
		Status: quotesv1.JobStatus_JOB_STATUS_PENDING,
	}, nil
}

func (h *Handler) GetQuoteUpdate(ctx context.Context, req *quotesv1.GetQuoteUpdateRequest) (*quotesv1.GetQuoteUpdateResponse, error) {
	id, err := parseUUID(req.GetJobId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid job_id")
	}

	result, err := h.uc.GetJobResult(ctx, id)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "job not found")
	}
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	resp := &quotesv1.GetQuoteUpdateResponse{
		JobId:        result.Job.ID.String(),
		Pair:         result.Job.Pair,
		Status:       toProtoStatus(result.Job.Status),
		ErrorMessage: result.Job.ErrorMessage,
	}
	if result.Value != nil {
		resp.Price = result.Value.Price.String()
		resp.RateTime = timestampFromTime(result.Value.RateTime)
	}
	return resp, nil
}

func (h *Handler) GetLatestQuote(ctx context.Context, req *quotesv1.GetLatestQuoteRequest) (*quotesv1.GetLatestQuoteResponse, error) {
	v, err := h.uc.GetLatest(ctx, req.GetPair())
	if errors.Is(err, storage.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "no quote for pair")
	}
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	return &quotesv1.GetLatestQuoteResponse{
		Pair:     v.Pair,
		Price:    v.Price.String(),
		RateTime: timestampFromTime(v.RateTime),
	}, nil
}

func toProtoStatus(s domain.JobStatus) quotesv1.JobStatus {
	switch s {
	case domain.JobStatusPending:
		return quotesv1.JobStatus_JOB_STATUS_PENDING
	case domain.JobStatusProcessing:
		return quotesv1.JobStatus_JOB_STATUS_PROCESSING
	case domain.JobStatusDone:
		return quotesv1.JobStatus_JOB_STATUS_DONE
	case domain.JobStatusFailed:
		return quotesv1.JobStatus_JOB_STATUS_FAILED
	default:
		return quotesv1.JobStatus_JOB_STATUS_UNSPECIFIED
	}
}
