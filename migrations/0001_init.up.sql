CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE quote_jobs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pair          TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending', 'processing', 'done', 'failed')),
    error_message TEXT,
    attempts      INT NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Частичный индекс под воркер: он всегда ищет только pending-задачи.
CREATE INDEX idx_quote_jobs_pending ON quote_jobs (created_at)
    WHERE status = 'pending';

CREATE TABLE quote_values (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id     UUID NOT NULL REFERENCES quote_jobs (id),
    pair       TEXT NOT NULL,
    price      NUMERIC(20, 8) NOT NULL,
    rate_time  TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Под "получить последнее значение по паре".
CREATE INDEX idx_quote_values_pair_latest ON quote_values (pair, created_at DESC);
-- Одна задача даёт максимум одно значение.
CREATE UNIQUE INDEX idx_quote_values_job_id ON quote_values (job_id);
