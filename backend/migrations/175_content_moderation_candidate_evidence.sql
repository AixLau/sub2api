-- Source-aware candidate moderation metadata and encrypted provider payloads.
-- The payload is capped by the service at the configured candidate fragment
-- limit, so this table never stores the original gateway request body.

ALTER TABLE content_moderation_logs
    ADD COLUMN IF NOT EXISTS decision_source VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS moderation_provider VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS moderation_model VARCHAR(255) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS source_origin VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS selected_source VARCHAR(255) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS selected_source_role VARCHAR(32) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS selected_fragment_runes INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS decision_cache_hit BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS duplicate_retry_count INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS user_violation_eligible BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_content_moderation_logs_decision_source_created_at
    ON content_moderation_logs (decision_source, created_at DESC);

CREATE TABLE IF NOT EXISTS content_moderation_evidence_snapshots (
    id                BIGSERIAL PRIMARY KEY,
    log_id            BIGINT NOT NULL REFERENCES content_moderation_logs(id) ON DELETE CASCADE,
    request_id        VARCHAR(128) NOT NULL DEFAULT '',
    selection         JSONB NOT NULL DEFAULT '{}'::jsonb,
    payload_encrypted TEXT NOT NULL,
    payload_hmac      VARCHAR(128) NOT NULL DEFAULT '',
    payload_runes     INT NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_content_moderation_evidence_log_id
    ON content_moderation_evidence_snapshots (log_id);

CREATE INDEX IF NOT EXISTS idx_content_moderation_evidence_request_id
    ON content_moderation_evidence_snapshots (request_id);
