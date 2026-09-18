//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// The older ticket owner stops polling after registration. Free capacity must
// become usable by a live owner within a bounded response window, without
// moving the older owner's session or creating an execution on its behalf.
func TestCredentialQueueProgressPausedOwner(t *testing.T) {
	ctx := context.Background()
	f := newAdmissionFixture(t, 1)
	store := NewPrincipalAdmissionStore(integrationDB)
	d, err := store.TryAdmit(ctx, f.input())
	require.NoError(t, err)
	require.Equal(t, service.AdmissionAdmitted, d.Code)
	paused, live := f.input(), f.input()
	paused.OriginalSession = "paused"
	live.OriginalSession = "live"
	for _, in := range []service.AdmissionInput{paused, live} {
		q, err := store.TryAdmit(ctx, in)
		require.NoError(t, err)
		require.Equal(t, service.AdmissionWait, q.Code)
	}
	require.NoError(t, store.Cancel(ctx, d.Snapshot.Lease))
	deadline := time.Now().Add(4 * time.Second)
	var admitted *service.CredentialExecutionSnapshot
	for time.Now().Before(deadline) {
		q, err := store.TryAdmit(ctx, live)
		require.NoError(t, err)
		if q.Code == service.AdmissionAdmitted {
			admitted = q.Snapshot
			break
		}
		require.Equal(t, service.AdmissionWait, q.Code)
		time.Sleep(100 * time.Millisecond)
	}
	require.NotNil(t, admitted, "older paused owner must not leave eligible capacity idle until ticket TTL")
	var count int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM request_leases WHERE request_id=$1`, paused.RequestID).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, store.Cancel(ctx, admitted.Lease))
	assertAdmissionLedger(t, f, 0)
}

func TestCredentialQueueProgressLostDuplicateHintsAndOwnerFence(t *testing.T) {
	f := newAdmissionFixture(t, 1)
	ctx := context.Background()
	first := &principalAdmissionStore{db: integrationDB}
	second := &principalAdmissionStore{db: integrationDB}
	d, err := first.TryAdmit(ctx, f.input())
	require.NoError(t, err)
	in := f.input()
	in.OriginalSession = "stable-session"
	q, err := first.TryAdmit(ctx, in)
	require.NoError(t, err)
	require.Equal(t, service.AdmissionWait, q.Code)
	require.NoError(t, first.Cancel(ctx, d.Snapshot.Lease))
	// No subscriber exists for this first offer: the notification is lost.
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	var n int
	require.NoError(t, tx.QueryRow(`SELECT occupied FROM upstream_principals WHERE id=$1 FOR NO KEY UPDATE`, f.principal).Scan(&n))
	require.NoError(t, advanceCredentialOffers(ctx, tx, f.principal))
	require.NoError(t, tx.Commit())
	assertAdmissionLedger(t, f, 0)
	wrong := in
	wrong.Node = "different-owner"
	q, err = second.TryAdmit(ctx, wrong)
	require.NoError(t, err)
	require.Equal(t, service.AdmissionAlreadyRunning, q.Code)
	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	require.NoError(t, first.WaitAdmission(waitCtx, in), "durable offer recovers missing notification")
	// Duplicating or delaying a notification creates neither lease nor binding.
	first.deliverAdmissionHints([]string{in.RequestID, in.RequestID})
	assertAdmissionLedger(t, f, 0)
	q, err = first.TryAdmit(ctx, in)
	require.NoError(t, err)
	require.Equal(t, service.AdmissionAdmitted, q.Code)
	winner := q.Snapshot
	q, err = second.TryAdmit(ctx, in)
	require.NoError(t, err)
	require.Equal(t, service.AdmissionAlreadyRunning, q.Code)
	var bound int64
	require.NoError(t, integrationDB.QueryRow(`SELECT instance_id FROM session_bindings WHERE principal_id=$1`, f.principal).Scan(&bound))
	require.Equal(t, winner.Lease.InstanceID, bound)
	require.NoError(t, first.Cancel(ctx, winner.Lease))
	assertAdmissionLedger(t, f, 0)
}

func TestCredentialQueueProgressOffersRecheckRevocationAndCapacity(t *testing.T) {
	ctx := context.Background()
	f := newAdmissionFixture(t, 1)
	s := &principalAdmissionStore{db: integrationDB}
	first, err := s.TryAdmit(ctx, f.input())
	require.NoError(t, err)
	in := f.input()
	q, err := s.TryAdmit(ctx, in)
	require.NoError(t, err)
	require.Equal(t, service.AdmissionWait, q.Code)
	require.NoError(t, s.Cancel(ctx, first.Snapshot.Lease))
	waitCtx, end := context.WithTimeout(ctx, time.Second)
	defer end()
	require.NoError(t, s.WaitAdmission(waitCtx, in))
	_, err = integrationDB.Exec(`UPDATE upstream_principals SET requested_limit=0,config_version=config_version+1 WHERE id=$1`, f.principal)
	require.NoError(t, err)
	q, err = s.TryAdmit(ctx, in)
	require.NoError(t, err)
	require.Equal(t, service.AdmissionWait, q.Code)
	assertAdmissionLedger(t, f, 0)
	_, err = integrationDB.Exec(`UPDATE api_keys SET status='disabled' WHERE id=$1`, f.key)
	require.NoError(t, err)
	q, err = s.TryAdmit(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "AUTHORIZATION_REVOKED", q.Reason)
	assertAdmissionLedger(t, f, 0)
}

func TestCredentialQueueProgressNoTicketlessWait(t *testing.T) {
	ctx := context.Background()
	f := newAdmissionFixture(t, 1)
	s := &principalAdmissionStore{db: integrationDB}
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.Exec(`SELECT pg_advisory_xact_lock($1)`, -f.principal)
	require.NoError(t, err)
	type result struct {
		d   service.AdmissionDecision
		err error
	}
	done := make(chan result, 1)
	go func() { d, err := s.TryAdmit(ctx, f.input()); done <- result{d, err} }()
	require.Eventually(t, func() bool {
		s.principalTurns.mu.Lock()
		defer s.principalTurns.mu.Unlock()
		return s.principalTurns.active > 0
	}, time.Second, time.Millisecond)
	select {
	case r := <-done:
		t.Fatalf("contention must stay local before registration, got %s %v", r.d.Code, r.err)
	case <-time.After(50 * time.Millisecond):
	}
	assertAdmissionLedger(t, f, 0)
	require.NoError(t, tx.Commit())
	r := <-done
	require.NoError(t, r.err)
	require.Equal(t, service.AdmissionAdmitted, r.d.Code)
	require.NoError(t, s.Cancel(ctx, r.d.Snapshot.Lease))
	assertAdmissionLedger(t, f, 0)
}
