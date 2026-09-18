//go:build integration

package repository

import (
	"context"
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
