//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestMultiCredentialControlMigrationRollbackAndReplay(t *testing.T) {
	ctx := context.Background()
	migration, err := migrations.FS.ReadFile("246_multi_credential_control.sql")
	require.NoError(t, err)
	// Isolated transactional schema: rollback must remove only test DDL and data.
	tx := testTx(t)
	_, err = tx.ExecContext(ctx, `CREATE SCHEMA multi_credential_rollback; SET LOCAL search_path=multi_credential_rollback;
        CREATE TABLE accounts(id BIGINT PRIMARY KEY, credentials JSONB, extra JSONB);
        INSERT INTO accounts VALUES(1,'{"access_token":"fixture-only"}','{"codex_fingerprint_seed":"existing"}')`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migration))
	require.NoError(t, err, "migration must be replayable")
	var mode, state, credentials, extra string
	_, err = tx.ExecContext(ctx, `INSERT INTO upstream_principals(name,provider,requested_limit) VALUES('test','openai_oauth',10)`)
	require.NoError(t, err)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT routing_mode,verification_state FROM upstream_principals`).Scan(&mode, &state))
	require.Equal(t, "OFF", mode)
	require.Equal(t, "UNVERIFIED", state)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT credentials::text,extra::text FROM accounts WHERE id=1`).Scan(&credentials, &extra))
	require.JSONEq(t, `{"access_token":"fixture-only"}`, credentials)
	require.JSONEq(t, `{"codex_fingerprint_seed":"existing"}`, extra)
	require.NoError(t, tx.Rollback())
	var exists bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname='multi_credential_rollback')`).Scan(&exists))
	require.False(t, exists)
}

func TestMultiCredentialControlConstraints(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	var principal, account, instance int64
	require.NoError(t, tx.QueryRowContext(ctx, `INSERT INTO upstream_principals(name,provider,requested_limit)
        VALUES('test','openai_oauth',10) RETURNING id`).Scan(&principal))
	require.NoError(t, tx.QueryRowContext(ctx, `INSERT INTO accounts(name,platform,type) VALUES('fixture','openai','oauth') RETURNING id`).Scan(&account))
	require.NoError(t, tx.QueryRowContext(ctx, `INSERT INTO credential_instances(principal_id,account_id,name,identity_generation,hard_max)
        VALUES($1,$2,'fixture','00000000-0000-4000-8000-000000000001',0) RETURNING id`, principal, account).Scan(&instance))
	_, err := tx.ExecContext(ctx, `INSERT INTO credential_identity_profiles(instance_id,principal_id,generation,installation_id,source)
        VALUES($1,$2,'00000000-0000-4000-8000-000000000001','old-installation','LEGACY_SEED')`, instance, principal)
	require.NoError(t, err)
	invalid := []string{
		`UPDATE upstream_principals SET requested_limit=-1`,
		`UPDATE upstream_principals SET tenant_id=2`,
		`UPDATE upstream_principals SET routing_mode='GROUPED'`,
		`UPDATE credential_instances SET weight=0`,
		`UPDATE credential_instances SET weight='NaN'::float8`,
		`UPDATE credential_instances SET hard_max=-1`,
		`UPDATE credential_instances SET identity_generation='00000000-0000-4000-8000-000000000002'`,
		`UPDATE credential_identity_profiles SET installation_id='changed'`,
	}
	for _, query := range invalid {
		_, err = tx.ExecContext(ctx, `SAVEPOINT invalid_change`)
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, query)
		require.Error(t, err, query)
		_, err = tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT invalid_change`)
		require.NoError(t, err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE credential_instances SET credential_version=credential_version+1 WHERE id=$1`, instance)
	require.NoError(t, err)
	var installation string
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT installation_id FROM credential_identity_profiles WHERE instance_id=$1`, instance).Scan(&installation))
	require.Equal(t, "old-installation", installation)
}
