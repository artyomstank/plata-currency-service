package job

import (
	"time"

	"github.com/google/uuid"

	"currency-quotes/internal/domain"
)

func jobToModel(job *domain.Job) jobModel {
	return jobModel{
		ID: uuid.UUID(job.ID), Pair: job.Pair, IdempotencyKey: job.IdempotencyKey,
		Status: string(job.Status), ErrorMessage: job.ErrorMessage, Attempts: job.Attempts,
		LeaseToken: job.LeaseToken, CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt,
	}
}

func (m jobModel) toDomain() *domain.Job {
	return &domain.Job{
		ID: domain.JobID(m.ID), Pair: m.Pair, IdempotencyKey: m.IdempotencyKey,
		Status: domain.JobStatus(m.Status), ErrorMessage: m.ErrorMessage, Attempts: m.Attempts,
		LeaseToken: m.LeaseToken, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

func nullableUUID(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}
