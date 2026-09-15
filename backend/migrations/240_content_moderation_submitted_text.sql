-- Persist the exact semantic-review text actually sent to the model.
--
-- input_excerpt remains a bounded 240-rune display summary. For semantic audit
-- records the gateway now also stores the full submitted text plus how it was
-- bounded, so the audit record is 1:1 with the upstream request instead of a
-- re-derived summary.
ALTER TABLE content_moderation_logs
    ADD COLUMN IF NOT EXISTS submitted_text TEXT NOT NULL DEFAULT '';

ALTER TABLE content_moderation_logs
    ADD COLUMN IF NOT EXISTS submitted_runes INT NOT NULL DEFAULT 0;

ALTER TABLE content_moderation_logs
    ADD COLUMN IF NOT EXISTS submitted_max_runes INT NOT NULL DEFAULT 0;

ALTER TABLE content_moderation_logs
    ADD COLUMN IF NOT EXISTS submitted_truncated BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE content_moderation_logs
    ADD COLUMN IF NOT EXISTS submitted_truncate_reasons JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE content_moderation_logs
    DROP CONSTRAINT IF EXISTS content_moderation_logs_submitted_truncate_reasons_array_check;

ALTER TABLE content_moderation_logs
    ADD CONSTRAINT content_moderation_logs_submitted_truncate_reasons_array_check
    CHECK (jsonb_typeof(submitted_truncate_reasons) = 'array') NOT VALID;

ALTER TABLE content_moderation_logs
    VALIDATE CONSTRAINT content_moderation_logs_submitted_truncate_reasons_array_check;