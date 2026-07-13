ALTER TABLE usage_logs
    DROP CONSTRAINT IF EXISTS usage_logs_source_check;

ALTER TABLE usage_logs
    ADD CONSTRAINT usage_logs_source_check
    CHECK (source IN ('gateway', 'account_test', 'content_moderation')) NOT VALID;

ALTER TABLE usage_logs
    VALIDATE CONSTRAINT usage_logs_source_check;
