-- Store the request identifier returned by the upstream provider.
-- NULL is expected for historical rows and paths without an upstream ID.
ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS upstream_request_id VARCHAR(128);

CREATE INDEX IF NOT EXISTS idx_usage_logs_upstream_request_id
    ON usage_logs (upstream_request_id)
    WHERE upstream_request_id IS NOT NULL;
