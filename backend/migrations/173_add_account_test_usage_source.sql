ALTER TABLE usage_logs
    ALTER COLUMN user_id DROP NOT NULL,
    ALTER COLUMN api_key_id DROP NOT NULL,
    ADD COLUMN IF NOT EXISTS source VARCHAR(32) NOT NULL DEFAULT 'gateway';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'usage_logs_source_check'
          AND conrelid = 'usage_logs'::regclass
    ) THEN
        ALTER TABLE usage_logs
            ADD CONSTRAINT usage_logs_source_check
            CHECK (source IN ('gateway', 'account_test')) NOT VALID;
    END IF;
END
$$;
