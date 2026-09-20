-- Route the OpenAI OAuth transport plugin through an explicit account allowlist.
CREATE TABLE IF NOT EXISTS sub2api_plugin_binding_accounts (
    binding_id BIGINT NOT NULL REFERENCES sub2api_plugin_bindings(id) ON DELETE CASCADE,
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (binding_id, account_id)
);

CREATE INDEX IF NOT EXISTS idx_sub2api_plugin_binding_accounts_account_id
    ON sub2api_plugin_binding_accounts(account_id);

-- Existing percentage bindings have no exact account selection. Disable them once
-- during the schema transition so traffic cannot silently enter a broader scope.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND table_name = 'sub2api_plugin_bindings'
          AND column_name = 'rollout_percent'
    ) THEN
        UPDATE sub2api_plugin_installations AS plugin
        SET state = 'disabled', enabled_at = NULL, updated_at = NOW()
        WHERE EXISTS (
            SELECT 1
            FROM sub2api_plugin_bindings AS binding
            WHERE binding.plugin_id = plugin.id AND binding.enabled = TRUE
        );

        UPDATE sub2api_plugin_bindings
        SET enabled = FALSE, updated_at = NOW()
        WHERE enabled = TRUE;

        ALTER TABLE sub2api_plugin_bindings
            DROP CONSTRAINT IF EXISTS sub2api_plugin_bindings_rollout_check;
        ALTER TABLE sub2api_plugin_bindings
            DROP COLUMN IF EXISTS rollout_percent;
    END IF;
END
$$;
