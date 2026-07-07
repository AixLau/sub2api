-- Encrypted raw request snapshots for risk-control audit records.

CREATE TABLE IF NOT EXISTS content_moderation_raw_request_snapshots (
    id              BIGSERIAL PRIMARY KEY,
    log_id          BIGINT NOT NULL REFERENCES content_moderation_logs(id) ON DELETE CASCADE,
    request_id      VARCHAR(128) NOT NULL DEFAULT '',
    body_encrypted  TEXT NOT NULL,
    body_bytes      INT NOT NULL DEFAULT 0,
    truncated       BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_content_moderation_raw_request_log_id
    ON content_moderation_raw_request_snapshots (log_id);

CREATE INDEX IF NOT EXISTS idx_content_moderation_raw_request_request_id
    ON content_moderation_raw_request_snapshots (request_id);
