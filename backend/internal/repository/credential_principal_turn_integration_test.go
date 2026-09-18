//go:build integration

package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// Hold A's cross-node turn until AFTER B is admitted. The trace barrier proves
// A has actually retried its advisory lock; scheduling a goroutine alone would
// not reproduce the head-of-line blocking deterministically.
func TestCredentialPrincipalTurnBlockedPrincipalDoesNotBlockHealthyPrincipal(t *testing.T) {
	fA, fB := newAdmissionFixture(t, 1), newAdmissionFixture(t, 1)
	ctx := context.Background()
	connector, err := pq.NewConnector(integrationDSN)
	require.NoError(t, err)
	db := sql.OpenDB(admissionTraceConnector{Connector: connector})
	db.SetMaxOpenConns(6)
	db.SetMaxIdleConns(6)
	defer db.Close()
	s := &principalAdmissionStore{db: db}
	held, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer held.Rollback()
	_, err = held.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, -fA.principal)
	require.NoError(t, err)
	defer func() { assertAdmissionLedger(t, fA, 0); assertAdmissionLedger(t, fB, 0) }()
	aCtx, cancelA := context.WithCancel(ctx)
	defer cancelA()
	aCtx, trace := newAdmissionTrace(aCtx)
	type result struct {
		decision service.AdmissionDecision
		err      error
	}
	aDone := make(chan result, 1)
	go func() { d, err := s.TryAdmit(aCtx, fA.input()); aDone <- result{d, err} }()
	require.Eventually(t, func() bool {
		trace.mu.Lock()
		defer trace.mu.Unlock()
		return trace.counts["rollback"] >= 2
	}, time.Second, time.Millisecond, "A must have entered the cross-node lock retry loop")
	bCtx, cancelB := context.WithTimeout(ctx, 750*time.Millisecond)
	defer cancelB()
	started := time.Now()
	b, err := s.TryAdmit(bCtx, fB.input())
	require.NoError(t, err, "B must complete without releasing A's cross-node lock")
	require.Equal(t, service.AdmissionAdmitted, b.Code)
	t.Logf("healthy_principal_admission_with_other_principal_locked=%s", time.Since(started))
	assertAdmissionLedger(t, fA, 0)
	assertAdmissionLedger(t, fB, 1)
	require.NoError(t, s.Cancel(ctx, b.Snapshot.Lease))
	select {
	case result := <-aDone:
		t.Fatalf("A unexpectedly completed while its cross-node lock remained held: %s %v", result.decision.Code, result.err)
	default:
	}
	cancelA()
	a := <-aDone
	require.ErrorIs(t, a.err, context.Canceled)
}
