ALTER TABLE quote_jobs
    ADD COLUMN idempotency_key TEXT,
    ADD COLUMN next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN lease_until TIMESTAMPTZ,
    ADD COLUMN lease_token UUID,
    ADD CONSTRAINT quote_jobs_idempotency_key_not_empty
        CHECK (idempotency_key IS NULL OR idempotency_key <> '');

CREATE UNIQUE INDEX idx_quote_jobs_idempotency_key
    ON quote_jobs (idempotency_key)
    WHERE idempotency_key IS NOT NULL;

DROP INDEX idx_quote_jobs_pending;
CREATE INDEX idx_quote_jobs_available
    ON quote_jobs (next_attempt_at, created_at)
    WHERE status = 'pending';

CREATE INDEX idx_quote_jobs_expired_lease
    ON quote_jobs (lease_until)
    WHERE status = 'processing';

ALTER TABLE quote_values
    ALTER COLUMN price TYPE NUMERIC(38, 18);
