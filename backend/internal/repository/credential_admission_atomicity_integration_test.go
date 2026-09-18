//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCredentialAdmissionCompetingTicketsKeepOrder(t *testing.T) {
	f := newAdmissionFixture(t, 1)
	ctx := context.Background()
	store := NewPrincipalAdmissionStore(integrationDB)
	occupied, err := store.TryAdmit(ctx, f.input())
	require.NoError(t, err)
	require.Equal(t, service.AdmissionAdmitted, occupied.Code)
	first, second := f.input(), f.input()
	for _, in := range []service.AdmissionInput{first, second} {
		d, err := store.TryAdmit(ctx, in)
		require.NoError(t, err)
		require.Equal(t, service.AdmissionWait, d.Code)
	}
	require.NoError(t, store.Cancel(ctx, occupied.Snapshot.Lease))
	// A newcomer may not take the newly available slot ahead of a ready ticket.
	newcomer, err := store.TryAdmit(ctx, f.input())
	require.NoError(t, err)
	require.Equal(t, service.AdmissionWait, newcomer.Code)
	require.Equal(t, "FAIRNESS_WAIT", newcomer.Reason)
	d, err := store.TryAdmit(ctx, first)
	require.NoError(t, err)
	require.Equal(t, service.AdmissionAdmitted, d.Code)
	require.NoError(t, store.Cancel(ctx, d.Snapshot.Lease))
	d, err = store.TryAdmit(ctx, second)
	require.NoError(t, err)
	require.Equal(t, service.AdmissionAdmitted, d.Code)
	require.NoError(t, store.Cancel(ctx, d.Snapshot.Lease))
	assertAdmissionLedger(t, f, 0)
}

func TestCredentialAdmissionCapacityWriteFailureRollsBack(t *testing.T) {
	f := newAdmissionFixture(t, 1)
	ctx := context.Background()
	store := NewPrincipalAdmissionStore(integrationDB)
	// A failure inside the capacity update must not leave a half-approved or
	// half-released ledger. The trigger affects only this disposable fixture.
	name := fmt.Sprintf("admission_capacity_failure_%d", f.principal)
	_, err := integrationDB.Exec(fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN RAISE EXCEPTION 'injected capacity write failure'; END $$;
 CREATE TRIGGER %s BEFORE UPDATE OF occupied ON credential_instances FOR EACH ROW
 WHEN (NEW.principal_id=%d) EXECUTE FUNCTION %s()`, name, name, f.principal, name))
	require.NoError(t, err)
	defer integrationDB.Exec(fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON credential_instances; DROP FUNCTION IF EXISTS %s()`, name, name))
	in := f.input()
	d, err := store.TryAdmit(ctx, in)
	require.ErrorContains(t, err, "injected capacity write failure")
	require.NotEqual(t, service.AdmissionAdmitted, d.Code)
	assertAdmissionLedger(t, f, 0)
	var count int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM logical_requests WHERE id=$1`, in.RequestID).Scan(&count))
	require.Zero(t, count)
	_, err = integrationDB.Exec(fmt.Sprintf(`ALTER TABLE credential_instances DISABLE TRIGGER %s`, name))
	require.NoError(t, err)
	d, err = store.TryAdmit(ctx, in)
	require.NoError(t, err)
	require.Equal(t, service.AdmissionAdmitted, d.Code)
	_, err = integrationDB.Exec(fmt.Sprintf(`ALTER TABLE credential_instances ENABLE TRIGGER %s`, name))
	require.NoError(t, err)
	require.ErrorContains(t, store.Cancel(ctx, d.Snapshot.Lease), "injected capacity write failure")
	assertAdmissionLedger(t, f, 1)
	var state string
	require.NoError(t, integrationDB.QueryRow(`SELECT state FROM request_leases WHERE id=$1`, d.Snapshot.Lease.ID).Scan(&state))
	require.Equal(t, "RESERVED", state)
	_, err = integrationDB.Exec(fmt.Sprintf(`ALTER TABLE credential_instances DISABLE TRIGGER %s`, name))
	require.NoError(t, err)
	require.NoError(t, store.Cancel(ctx, d.Snapshot.Lease))
	require.NoError(t, store.Cancel(ctx, d.Snapshot.Lease))
	assertAdmissionLedger(t, f, 0)
}
