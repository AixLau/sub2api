//go:build integration

package repository

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
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
