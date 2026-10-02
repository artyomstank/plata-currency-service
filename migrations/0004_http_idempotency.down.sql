ALTER TABLE quote_jobs ADD COLUMN idempotency_key TEXT,
    ADD CONSTRAINT quote_jobs_idempotency_key_not_empty CHECK (idempotency_key IS NULL OR idempotency_key <> '');

UPDATE quote_jobs AS job
SET idempotency_key = response.key
FROM http_idempotency AS response
WHERE job.id::text = (convert_from(response.response_body, 'UTF8')::jsonb ->> 'jobId');

CREATE UNIQUE INDEX idx_quote_jobs_idempotency_key
    ON quote_jobs (idempotency_key)
    WHERE idempotency_key IS NOT NULL;

DROP TABLE http_idempotency;
