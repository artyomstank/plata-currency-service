// Package grpc адаптирует бизнес-логику (usecase) к сгенерированному
// gRPC-интерфейсу QuotesServiceServer. Зависимость от internal/genpb
// появится после `make generate`.
package grpc

import (
	"context"
	"errors"
	"log/slog"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"currency-quotes/internal/domain"
	"currency-quotes/internal/usecase"

	quotesv1 "currency-quotes/internal/genpb/quotes/v1"
)

type Handler struct {
	quotesv1.UnimplementedQuotesServiceServer
	uc  *usecase.QuotesUseCase
	log *slog.Logger
}

func New(uc *usecase.QuotesUseCase, log *slog.Logger) *Handler {
	return &Handler{uc: uc, log: log}
}

func (h *Handler) UpdateQuote(ctx context.Context, req *quotesv1.UpdateQuoteRequest) (*quotesv1.UpdateQuoteResponse, error) {
	result, err := h.uc.RequestUpdate(ctx, req.GetPair(), idempotencyKeyFromContext(ctx))
	if errors.Is(err, usecase.ErrInvalidPair) || errors.Is(err, usecase.ErrInvalidIdempotency) {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if errors.Is(err, usecase.ErrIdempotencyConflict) {
		return nil, status.Error(codes.AlreadyExists, err.Error())
	}
	if err != nil {
		h.log.ErrorContext(ctx, "request quote update failed", "err", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	if result.Created {
		if err := grpc.SetHeader(ctx, metadata.Pairs("x-http-code", "202")); err != nil {
			h.log.ErrorContext(ctx, "set HTTP response status metadata", "err", err)
			return nil, status.Error(codes.Internal, "internal error")
		}
	}
	return &quotesv1.UpdateQuoteResponse{
		JobId:  result.Job.ID.String(),
		Status: toProtoStatus(result.Job.Status),
	}, nil
}

func (h *Handler) GetQuoteUpdate(ctx context.Context, req *quotesv1.GetQuoteUpdateRequest) (*quotesv1.GetQuoteUpdateResponse, error) {
	id, err := parseUUID(req.GetJobId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid job_id")
	}

	result, err := h.uc.GetJobResult(ctx, id)
	if errors.Is(err, usecase.ErrJobNotFound) {
		return nil, status.Error(codes.NotFound, "job not found")
	}
	if err != nil {
		h.log.ErrorContext(ctx, "get quote update failed", "job_id", id, "err", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	resp := &quotesv1.GetQuoteUpdateResponse{
		JobId:  result.Job.ID.String(),
		Pair:   result.Job.Pair,
		Status: toProtoStatus(result.Job.Status),
	}
	if result.Job.Status == domain.JobStatusFailed {
		errorMessage := result.Job.ErrorMessage
		resp.ErrorMessage = &errorMessage
	}
	if result.Value != nil {
		price := result.Value.Price.String()
		resp.Price = &price
		resp.UpdatedAt = timestampFromTime(result.Value.CreatedAt)
	}
	return resp, nil
}

func (h *Handler) GetLatestQuote(ctx context.Context, req *quotesv1.GetLatestQuoteRequest) (*quotesv1.GetLatestQuoteResponse, error) {
	v, err := h.uc.GetLatest(ctx, req.GetPair())
	if errors.Is(err, usecase.ErrInvalidPair) {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if errors.Is(err, usecase.ErrQuoteNotFound) {
		return nil, status.Error(codes.NotFound, "no quote for pair")
	}
	if err != nil {
		h.log.ErrorContext(ctx, "get latest quote failed", "pair", req.GetPair(), "err", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	return &quotesv1.GetLatestQuoteResponse{
		Pair:      v.Pair,
		Price:     v.Price.String(),
		UpdatedAt: timestampFromTime(v.CreatedAt),
	}, nil
}

func idempotencyKeyFromContext(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	values := md.Get("idempotency-key")
	if len(values) == 0 {
		return ""
	}
	return values[0]
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
