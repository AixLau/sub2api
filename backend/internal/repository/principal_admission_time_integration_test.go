//go:build integration

package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// Observe the real database wait graph instead of assuming a goroutine has
// reached a row lock after an arbitrary sleep.
func admissionTimeBarrier(t *testing.T, principal int64) (*sql.Tx, int) {
	t.Helper()
	tx, err := integrationDB.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	var pid int
	require.NoError(t, tx.QueryRow(`SELECT pg_backend_pid() FROM upstream_principals WHERE id=$1 FOR NO KEY UPDATE`, principal).Scan(&pid))
	return tx, pid
}

func waitAdmissionBlocked(t *testing.T, pid int) {
	t.Helper()
	require.Eventually(t, func() bool {
		var blocked bool
		err := integrationDB.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, pid).Scan(&blocked)
		return err == nil && blocked
	}, 5*time.Second, 5*time.Millisecond, "transaction never reached the lock barrier")
}

func admissionDatabaseClock(t *testing.T) time.Time {
	t.Helper()
	var now time.Time
	require.NoError(t, integrationDB.QueryRow(`SELECT clock_timestamp()`).Scan(&now))
	return now
}

func waitAdmissionDeadline(t *testing.T, deadline time.Time) {
	t.Helper()
	require.Eventually(t, func() bool { return !admissionDatabaseClock(t).Before(deadline) }, max(5*time.Second, time.Until(deadline)+5*time.Second), 5*time.Millisecond)
}

func TestPrincipalAdmissionTimeDispatchDeadlineAfterLockWait(t *testing.T) {
	f := newAdmissionFixture(t, 1)
	store := NewPrincipalAdmissionStore(integrationDB)
	in := f.input()
	in.Deadline = admissionDatabaseClock(t).Add(time.Second)
	d, err := store.TryAdmit(context.Background(), in)
	require.NoError(t, err)
	require.Equal(t, service.AdmissionAdmitted, d.Code)
	tx, pid := admissionTimeBarrier(t, f.principal)
	done := make(chan error, 1)
	go func() { done <- store.BeginDispatch(context.Background(), d.Snapshot.Lease) }()
	waitAdmissionBlocked(t, pid)
	waitAdmissionDeadline(t, in.Deadline)
	require.NoError(t, tx.Commit())
	require.ErrorIs(t, <-done, service.ErrAdmissionOwnership)
	// No dispatch was approved: the original owner can compensate exactly once.
	require.NoError(t, store.Cancel(context.Background(), d.Snapshot.Lease))
	require.NoError(t, store.Cancel(context.Background(), d.Snapshot.Lease))
	var state, outcome string
	require.NoError(t, integrationDB.QueryRow(`SELECT state,outcome FROM request_leases WHERE id=$1`, d.Snapshot.Lease.ID).Scan(&state, &outcome))
	require.Equal(t, "RELEASED", state)
	require.Equal(t, "NOT_SENT", outcome)
	assertAdmissionLedger(t, f, 0)
}

func TestPrincipalAdmissionTimeAdmitDeadlineAfterLockWait(t *testing.T) {
	f := newAdmissionFixture(t, 1)
	store := NewPrincipalAdmissionStore(integrationDB)
	tx, pid := admissionTimeBarrier(t, f.principal)
	in := f.input()
	in.Deadline = admissionDatabaseClock(t).Add(time.Second)
	done := make(chan service.AdmissionDecision, 1)
	errs := make(chan error, 1)
	go func() { d, err := store.TryAdmit(context.Background(), in); done <- d; errs <- err }()
	waitAdmissionBlocked(t, pid)
	waitAdmissionDeadline(t, in.Deadline)
	require.NoError(t, tx.Commit())
	require.NoError(t, <-errs)
	require.Equal(t, "ADMISSION_QUEUE_TIMEOUT", (<-done).Reason)
	assertAdmissionLedger(t, f, 0)
}

func TestPrincipalAdmissionTimeHeartbeatAfterLockWait(t *testing.T) {
	f := newAdmissionFixture(t, 1)
	store := NewPrincipalAdmissionStore(integrationDB)
	d, err := store.TryAdmit(context.Background(), f.input())
	require.NoError(t, err)
	require.NoError(t, store.BeginDispatch(context.Background(), d.Snapshot.Lease))
	tx, pid := admissionTimeBarrier(t, f.principal)
	done := make(chan error, 1)
	go func() { done <- store.Heartbeat(context.Background(), d.Snapshot.Lease) }()
	waitAdmissionBlocked(t, pid)
	// Cross the real 30-second stale threshold, using a DB-clock barrier. This
	// repository test deliberately supplies a long-lived context; the executor's
	// shorter 3-second heartbeat cancellation is covered separately under load.
	waitAdmissionDeadline(t, admissionDatabaseClock(t).Add(31*time.Second))
	releasedAfter := admissionDatabaseClock(t)
	require.NoError(t, tx.Commit())
	require.NoError(t, <-done)
	var heartbeat time.Time
	require.NoError(t, integrationDB.QueryRow(`SELECT heartbeat_at FROM request_leases WHERE id=$1`, d.Snapshot.Lease.ID).Scan(&heartbeat))
	require.False(t, heartbeat.Before(releasedAfter), "heartbeat=%s lock release lower bound=%s", heartbeat, releasedAfter)
	require.NoError(t, store.Finish(context.Background(), service.FinishAdmissionInput{Lease: d.Snapshot.Lease, Complete: true, Outcome: "COMPLETED"}))
	assertAdmissionLedger(t, f, 0)
}

func TestPrincipalAdmissionTimePoolCancellation(t *testing.T) {
	f := newAdmissionFixture(t, 1)
	db, err := openSQLWithRetry(context.Background(), integrationDSN, time.Second)
	require.NoError(t, err)
	defer db.Close()
	db.SetMaxOpenConns(1)
	conn, err := db.Conn(context.Background())
	require.NoError(t, err)
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	d, err := NewPrincipalAdmissionStore(db).TryAdmit(ctx, f.input())
	require.Error(t, err)
	require.NotEqual(t, service.AdmissionAdmitted, d.Code)
	require.Positive(t, db.Stats().WaitCount)
	assertAdmissionLedger(t, f, 0)
}
