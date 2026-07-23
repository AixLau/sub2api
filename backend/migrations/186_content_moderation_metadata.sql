-- Keep moderation evidence separate from operational failures.
ALTER TABLE content_moderation_logs
    ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE content_moderation_logs
    DROP CONSTRAINT IF EXISTS content_moderation_logs_metadata_object_check;

ALTER TABLE content_moderation_logs
    ADD CONSTRAINT content_moderation_logs_metadata_object_check
    CHECK (jsonb_typeof(metadata) = 'object');
