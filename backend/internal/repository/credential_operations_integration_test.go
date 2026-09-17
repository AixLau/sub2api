//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCredentialOperationsOrphansCASDrainAT26AT35AT38(t *testing.T) {
	f := newAdmissionFixture(t, 10)
	ctx := context.Background()
	store := &principalAdmissionStore{db: integrationDB}
	ops := &credentialOperations{db: integrationDB}
	d, err := store.TryAdmit(ctx, f.input())
	require.NoError(t, err)
	require.Equal(t, service.AdmissionAdmitted, d.Code)
	require.NoError(t, store.BeginDispatch(ctx, d.Snapshot.Lease))
	_, err = integrationDB.Exec(`UPDATE request_leases SET heartbeat_at=CURRENT_TIMESTAMP-INTERVAL '40 seconds' WHERE id=$1`, d.Snapshot.Lease.ID)
	require.NoError(t, err)
	count, err := ops.ReconcileCredentialLeases(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, count, 1)
	assertAdmissionLedger(t, f, 1)
	v, err := ops.CredentialRuntime(ctx, f.principal)
	require.NoError(t, err)
	require.Equal(t, 1, v.Orphaned)
	require.False(t, v.CounterMismatch)
	zero := 0
	view, err := ops.UpdatePrincipal(ctx, f.user, f.principal, 1, service.PrincipalControlUpdate{RequestedLimit: &zero})
	require.NoError(t, err)
	view.ComputeCapacityView(true)
	require.Equal(t, "DRAINING_TO_LIMIT", view.AdmissionState)
	require.Equal(t, int64(2), view.ConfigVersion)
	_, err = ops.UpdatePrincipal(ctx, f.user, f.principal, 1, service.PrincipalControlUpdate{AdminState: "ACTIVE"})
	require.ErrorIs(t, err, errCredentialConfigConflict)
	require.Error(t, ops.ResolveCredentialLease(ctx, f.user, d.Snapshot.Lease.ID, service.CredentialResolveInput{Confirm: false}))
	require.NoError(t, ops.ResolveCredentialLease(ctx, f.user, d.Snapshot.Lease.ID, service.CredentialResolveInput{Confirm: true, Evidence: "mock upstream confirmed finished", Reason: "test resolution"}))
	assertAdmissionLedger(t, f, 0)
	deadline := time.Now().Add(time.Minute)
	_, err = ops.UpdateInstance(ctx, f.user, f.instances[0], 2, service.InstanceControlUpdate{AdminState: "DRAINING", DrainDeadline: &deadline})
	require.NoError(t, err)
}

func TestCredentialOperationsMismatchFailsClosed(t *testing.T) {
	f := newAdmissionFixture(t, 10)
	ctx := context.Background()
	_, err := integrationDB.Exec(`UPDATE upstream_principals SET occupied=1 WHERE id=$1`, f.principal)
	require.NoError(t, err)
	store := NewPrincipalAdmissionStore(integrationDB)
	d, err := store.TryAdmit(ctx, f.input())
	require.NoError(t, err)
	require.Equal(t, "LEDGER_MISMATCH_FROZEN", d.Reason)
	ops := &credentialOperations{db: integrationDB}
	_, err = ops.ReconcileCredentialLeases(ctx)
	require.NoError(t, err)
	var state string
	require.NoError(t, integrationDB.QueryRow(`SELECT admin_state FROM upstream_principals WHERE id=$1`, f.principal).Scan(&state))
	require.Equal(t, "DISABLED", state)
}

func TestCredentialMaintenanceUnknownRefreshAndImportExpiry(t *testing.T) {
	f := newAdmissionFixture(t, 10)
	ctx := context.Background()
	ops := &credentialOperations{db: integrationDB}
	refresh := &credentialRefreshStore{db: integrationDB}
	_, err := integrationDB.Exec(`UPDATE credential_secrets SET can_refresh=true,refresh_family='maintenance-family',expires_at=CURRENT_TIMESTAMP+INTERVAL '1 minute' WHERE instance_id=$1`, f.instances[0])
	require.NoError(t, err)
	ids, err := ops.DueCredentialRefreshes(ctx)
	require.NoError(t, err)
	require.Contains(t, ids, f.instances[0])
	operation, err := refresh.BeginCredentialRefresh(ctx, f.instances[0])
	require.NoError(t, err)
	_, err = integrationDB.Exec(`UPDATE credential_refresh_ops SET started_at=CURRENT_TIMESTAMP-INTERVAL '2 minutes' WHERE id=$1`, operation.ID)
	require.NoError(t, err)
	_, err = ops.ReconcileCredentialLeases(ctx)
	require.NoError(t, err)
	var state string
	require.NoError(t, integrationDB.QueryRow(`SELECT credential_state FROM credential_instances WHERE id=$1`, f.instances[0]).Scan(&state))
	require.Equal(t, "REFRESH_UNKNOWN", state)
	_, err = refresh.BeginCredentialRefresh(ctx, f.instances[0])
	require.Error(t, err)
}
