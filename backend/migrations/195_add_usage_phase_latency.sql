-- Persist request-phase timings for TTFT diagnosis and admin drilldown.
ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS user_queue_wait_ms INT,
    ADD COLUMN IF NOT EXISTS account_queue_wait_ms INT,
    ADD COLUMN IF NOT EXISTS upstream_request_write_ms INT,
    ADD COLUMN IF NOT EXISTS upstream_response_headers_ms INT,
    ADD COLUMN IF NOT EXISTS upstream_first_event_ms INT;
