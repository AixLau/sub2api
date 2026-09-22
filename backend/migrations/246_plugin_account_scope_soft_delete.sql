-- Account deletion is implemented as UPDATE accounts SET deleted_at = NOW(), so
-- the binding table's ON DELETE CASCADE does not run for normal admin deletes.
-- Remove existing stale selections and keep future plugin scopes synchronized.
DELETE FROM sub2api_plugin_binding_accounts AS binding_account
USING accounts AS account
WHERE binding_account.account_id = account.id
  AND account.deleted_at IS NOT NULL;

CREATE OR REPLACE FUNCTION cleanup_plugin_account_scope_on_account_soft_delete()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    DELETE FROM sub2api_plugin_binding_accounts
    WHERE account_id = NEW.id;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS accounts_cleanup_plugin_scope_on_soft_delete ON accounts;

CREATE TRIGGER accounts_cleanup_plugin_scope_on_soft_delete
AFTER UPDATE OF deleted_at ON accounts
FOR EACH ROW
WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL)
EXECUTE FUNCTION cleanup_plugin_account_scope_on_account_soft_delete();
