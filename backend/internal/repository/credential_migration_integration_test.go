//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCredentialMigrationPreviewAndRollbackAT37AT38(t *testing.T) {
	f := newAdmissionFixture(t, 10)
	ctx := context.Background()
	migration := &credentialMigrationStore{db: integrationDB}
	var account int64
	require.NoError(t, integrationDB.QueryRow(`SELECT account_id FROM credential_instances WHERE id=$1`, f.instances[0]).Scan(&account))
	var before, after string
	require.NoError(t, integrationDB.QueryRow(`SELECT row_to_json(a)::text FROM accounts a WHERE id=$1`, account).Scan(&before))
	preview, err := migration.PreviewCredentialMigration(ctx, []int64{account})
	require.NoError(t, err)
	require.Len(t, preview, 1)
	require.Equal(t, "ALREADY_CONTROLLED", preview[0].State)
	require.NoError(t, integrationDB.QueryRow(`SELECT row_to_json(a)::text FROM accounts a WHERE id=$1`, account).Scan(&after))
	require.Equal(t, before, after)
	store := NewPrincipalAdmissionStore(integrationDB)
	d, err := store.TryAdmit(ctx, f.input())
	require.NoError(t, err)
	require.Equal(t, service.AdmissionAdmitted, d.Code)
	require.NoError(t, store.BeginDispatch(ctx, d.Snapshot.Lease))
	require.NoError(t, store.Finish(ctx, service.FinishAdmissionInput{Lease: d.Snapshot.Lease}))
	require.NoError(t, migration.PrepareCredentialRollback(ctx, f.user, f.principal, 1))
	assertAdmissionLedger(t, f, 1)
	var mode, state string
	require.NoError(t, integrationDB.QueryRow(`SELECT routing_mode,admin_state FROM upstream_principals WHERE id=$1`, f.principal).Scan(&mode, &state))
	require.Equal(t, "GROUPED", mode)
	require.Equal(t, "PAUSED", state)
}
