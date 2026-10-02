CREATE TABLE http_idempotency (
    key              TEXT PRIMARY KEY CHECK (key <> '' AND octet_length(key) <= 128),
    status_code      INTEGER,
    response_headers JSONB,
    response_body    BYTEA,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (
        (status_code IS NULL AND response_headers IS NULL AND response_body IS NULL)
        OR (status_code IS NOT NULL AND status_code BETWEEN 200 AND 299 AND response_headers IS NOT NULL AND response_body IS NOT NULL)
    )
);

INSERT INTO http_idempotency (key, status_code, response_headers, response_body)
SELECT idempotency_key, 202, '{"Content-Type":["application/json"]}'::jsonb,
       convert_to(format('{"jobId":"%s","status":"JOB_STATUS_PENDING"}', id) || E'\n', 'UTF8')
FROM quote_jobs
WHERE idempotency_key IS NOT NULL;

ALTER TABLE quote_jobs DROP COLUMN idempotency_key;
