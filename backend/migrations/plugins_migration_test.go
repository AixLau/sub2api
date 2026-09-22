package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPluginsMigrationKeepsAccountSchemaUnchanged(t *testing.T) {
	content, err := FS.ReadFile("229_plugins.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS sub2api_plugin_installations")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS sub2api_plugin_bindings")
	require.Contains(t, sql, "config_encrypted TEXT NOT NULL DEFAULT ''")
	require.Contains(t, sql, "REFERENCES sub2api_plugin_installations(id)")
	require.Contains(t, sql, "CREATE UNIQUE INDEX IF NOT EXISTS idx_sub2api_plugin_bindings_one_enabled_scope")
	require.Contains(t, sql, "WHERE enabled = TRUE")
	require.NotContains(t, sql, "CREATE TABLE IF NOT EXISTS plugin_installations")
	require.NotContains(t, sql, "CREATE TABLE IF NOT EXISTS plugin_bindings")
	require.NotContains(t, strings.ToUpper(sql), "ALTER TABLE ACCOUNTS")
	require.NotContains(t, sql, "account_id")
}

func TestPluginArtifactMigrationSupportsExistingInstallations(t *testing.T) {
	content, err := FS.ReadFile("230_plugin_artifacts.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "ALTER TABLE sub2api_plugin_installations")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS artifact_data BYTEA")
	require.NotContains(t, strings.ToUpper(sql), "ALTER TABLE ACCOUNTS")
}

func TestPluginAccountScopeMigrationReplacesPercentageRollout(t *testing.T) {
	content, err := FS.ReadFile("231_plugin_account_scopes.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS sub2api_plugin_binding_accounts")
	require.Contains(t, sql, "binding_id BIGINT NOT NULL REFERENCES sub2api_plugin_bindings(id) ON DELETE CASCADE")
	require.Contains(t, sql, "account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE")
	require.Contains(t, sql, "PRIMARY KEY (binding_id, account_id)")
	require.Contains(t, sql, "SET state = 'disabled'")
	require.Contains(t, sql, "DROP COLUMN IF EXISTS rollout_percent")
	require.NotContains(t, strings.ToUpper(sql), "ALTER TABLE ACCOUNTS")
}

func TestPluginAccountScopeSoftDeleteMigrationCleansBindings(t *testing.T) {
	content, err := FS.ReadFile("246_plugin_account_scope_soft_delete.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "DELETE FROM sub2api_plugin_binding_accounts AS binding_account USING accounts AS account")
	require.Contains(t, sql, "account.deleted_at IS NOT NULL")
	require.Contains(t, sql, "CREATE OR REPLACE FUNCTION cleanup_plugin_account_scope_on_account_soft_delete()")
	require.Contains(t, sql, "AFTER UPDATE OF deleted_at ON accounts")
	require.Contains(t, sql, "WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL)")
	require.Contains(t, sql, "DELETE FROM sub2api_plugin_binding_accounts WHERE account_id = NEW.id")
}
