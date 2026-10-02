package job

import (
	"database/sql"
	"time"

	"github.com/google/uuid"

	"currency-quotes/internal/domain"
)

func jobToModel(job *domain.Job) jobModel {
	return jobModel{
		ID: uuid.UUID(job.ID), Pair: job.Pair,
		Status: string(job.Status), ErrorMessage: nullableString(job.ErrorMessage), Attempts: job.Attempts,
		LeaseUntil: nullableTime(job.LeaseUntil), NextAttemptAt: job.NextAttemptAt,
		CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt,
	}
}

func (m jobModel) toDomain() *domain.Job {
	job := &domain.Job{
		ID: domain.JobID(m.ID), Pair: m.Pair,
		Status: domain.JobStatus(m.Status), ErrorMessage: m.ErrorMessage.String, Attempts: m.Attempts,
		NextAttemptAt: m.NextAttemptAt, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
	if m.LeaseUntil != nil {
		job.LeaseUntil = *m.LeaseUntil
	}
	return job
}

func nullableString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}

func nullableTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}
