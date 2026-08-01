-- Persist request-phase timings for error-only requests and Ops drilldown.
ALTER TABLE ops_error_logs
    ADD COLUMN IF NOT EXISTS user_queue_wait_ms BIGINT,
    ADD COLUMN IF NOT EXISTS account_queue_wait_ms BIGINT,
    ADD COLUMN IF NOT EXISTS upstream_request_write_ms BIGINT,
    ADD COLUMN IF NOT EXISTS upstream_response_headers_ms BIGINT,
    ADD COLUMN IF NOT EXISTS upstream_first_event_ms BIGINT;
