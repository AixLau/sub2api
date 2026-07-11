-- Selected upstream account snapshots for content-moderation audit records.

ALTER TABLE content_moderation_logs
    ADD COLUMN IF NOT EXISTS account_id BIGINT REFERENCES accounts(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS account_name VARCHAR(255),
    ADD COLUMN IF NOT EXISTS account_type VARCHAR(32);
