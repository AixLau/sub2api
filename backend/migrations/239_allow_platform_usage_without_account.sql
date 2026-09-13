-- Platform-owned operations such as administrator-configured content
-- moderation do not have a local upstream account. Keep account_id nullable so
-- they can still be recorded with source = 'content_moderation'.
ALTER TABLE usage_logs
    ALTER COLUMN account_id DROP NOT NULL;
