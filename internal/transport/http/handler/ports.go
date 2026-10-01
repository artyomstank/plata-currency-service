package handler

import (
	"context"

	"currency-quotes/internal/domain"
	"currency-quotes/internal/usecase"
)

type RequestUpdateUseCase interface {
	Execute(context.Context, usecase.RequestQuoteUpdateInput) (*usecase.RequestUpdateResult, error)
}

type GetJobResultUseCase interface {
	Execute(context.Context, usecase.GetQuoteUpdateInput) (*usecase.JobResult, error)
}

type GetLatestUseCase interface {
	Execute(context.Context, usecase.GetLatestQuoteInput) (*domain.Quote, error)
}

type UseCases struct {
	RequestUpdate RequestUpdateUseCase
	GetJobResult  GetJobResultUseCase
	GetLatest     GetLatestUseCase
}
