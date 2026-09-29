DROP INDEX IF EXISTS idx_quote_jobs_expired_lease;
DROP INDEX IF EXISTS idx_quote_jobs_available;
DROP INDEX IF EXISTS idx_quote_jobs_idempotency_key;

ALTER TABLE quote_values
    ALTER COLUMN price TYPE NUMERIC(20, 8);

ALTER TABLE quote_jobs
    DROP CONSTRAINT IF EXISTS quote_jobs_idempotency_key_not_empty,
    DROP COLUMN IF EXISTS lease_token,
    DROP COLUMN IF EXISTS lease_until,
    DROP COLUMN IF EXISTS next_attempt_at,
    DROP COLUMN IF EXISTS idempotency_key;

CREATE INDEX idx_quote_jobs_pending ON quote_jobs (created_at)
    WHERE status = 'pending';
