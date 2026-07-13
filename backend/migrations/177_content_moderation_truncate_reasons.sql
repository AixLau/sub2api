-- Persist extraction truncation reasons separately from the human-readable error.
ALTER TABLE content_moderation_logs
    ADD COLUMN IF NOT EXISTS truncate_reasons JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE content_moderation_logs
    DROP CONSTRAINT IF EXISTS content_moderation_logs_truncate_reasons_array_check;

ALTER TABLE content_moderation_logs
    ADD CONSTRAINT content_moderation_logs_truncate_reasons_array_check
    CHECK (jsonb_typeof(truncate_reasons) = 'array') NOT VALID;

ALTER TABLE content_moderation_logs
    VALIDATE CONSTRAINT content_moderation_logs_truncate_reasons_array_check;

CREATE INDEX IF NOT EXISTS idx_content_moderation_logs_truncate_reasons
    ON content_moderation_logs USING GIN (truncate_reasons);
