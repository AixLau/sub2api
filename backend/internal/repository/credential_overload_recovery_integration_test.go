//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type overloadFinishStore struct {
	*principalAdmissionStore
	first atomic.Bool
}

func (s *overloadFinishStore) Finish(ctx context.Context, in service.FinishAdmissionInput) error {
	if !s.first.CompareAndSwap(false, true) {
		return s.principalAdmissionStore.Finish(ctx, in)
	}
	tx, err := integrationDB.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err = tx.QueryRow(`SELECT occupied FROM upstream_principals WHERE id=$1 FOR NO KEY UPDATE`, in.Lease.PrincipalID).Scan(&n); err != nil {
		return err
	}
	// Keep the original executor's 5s context. This simulates an external long
	// transaction; the completion evidence has already been committed.
	return s.principalAdmissionStore.Finish(ctx, in)
}

func TestCredentialOverloadRecoveryTerminalAndUnknown(t *testing.T) {
	ctx := context.Background()
	f := newAdmissionFixture(t, 10)
	prepareAcceptanceIdentity(t, f, f.user)
	real := reservedAdmissionStore(t)
	store := &overloadFinishStore{principalAdmissionStore: real}
	var starts atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { starts.Add(1); acceptanceTerminal(w) }))
	defer upstream.Close()
	gateway := acceptanceGateway(t, &acceptanceUpstream{url: upstream.URL}, store)
	status, _, err := acceptanceRequest(ctx, gateway.URL+"/v1/responses", acceptanceKey(t, f.key), "overload-terminal")
	require.NoError(t, err)
	require.Equal(t, 200, status)
	// The foreground billing path may acknowledge the receipt even when Finish
	// timed out. Completion recovery must therefore not rely on PENDING alone.
	var state, receiptState string
	require.NoError(t, integrationDB.QueryRow(`SELECT l.state,c.state FROM request_leases l JOIN credential_usage_receipts c ON c.lease_id=l.id WHERE l.principal_id=$1`, f.principal).Scan(&state, &receiptState))
	require.Equal(t, "DISPATCHING", state)
	t.Logf("after_timeout lease=%s receipt=%s", state, receiptState)
	unknown, err := real.TryAdmit(ctx, f.input())
	require.NoError(t, err)
	require.Equal(t, service.AdmissionAdmitted, unknown.Code)
	require.NoError(t, real.BeginDispatch(ctx, unknown.Snapshot.Lease))
	require.NoError(t, real.Finish(ctx, service.FinishAdmissionInput{Lease: unknown.Snapshot.Lease}))
	// Existing new-request pressure occupies ordinary connections, while terminal
	// recovery reads its evidence and releases through the reserved pool.
	var held []*sql.Conn
	for range 6 {
		c, err := real.db.Conn(ctx)
		require.NoError(t, err)
		held = append(held, c)
	}
	var wg sync.WaitGroup
	pressure, endPressure := context.WithCancel(ctx)
	for range 12 {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = real.TryAdmit(pressure, f.input()) }()
	}
	endPressure()
	for _, c := range held {
		_ = c.Close()
	}
	wg.Wait()
	started := time.Now()
	cache := NewConcurrencyCache(integrationRedis, 15, 15)
	global, err := newCredentialGlobalUserSlots(real.criticalDB(), integrationRedis, cache)
	require.NoError(t, err)
	// Real periodic reconciler wiring: receipt completion after the first 10s
	// tick, Redis released marker on the next. Recovery budget is 25s; individual
	// heartbeat/Finish budgets are unchanged (3s/5s).
	reconciler := service.ProvideCredentialReconciler(NewCredentialOperations(real.criticalDB()), nil, real, gateway.gateway, gateway.keys, gateway.keyService, global)
	defer reconciler.Stop()
	require.Eventually(t, func() bool {
		var occupied int
		err := integrationDB.QueryRow(`SELECT occupied FROM upstream_principals WHERE id=$1`, f.principal).Scan(&occupied)
		return err == nil && occupied == 1 && integrationRedis.ZCard(ctx, fmt.Sprintf("concurrency:user:%d", f.user)).Val() == 0
	}, 25*time.Second, 20*time.Millisecond)
	recovery, end := context.WithTimeout(ctx, 5*time.Second)
	defer end()
	require.NoError(t, gateway.gateway.RecoverCredentialUsage(recovery, real, gateway.keys, gateway.keyService))
	require.NoError(t, gateway.gateway.RecoverCredentialUsage(recovery, real, gateway.keys, gateway.keyService))
	t.Logf("recovery_after_pressure=%s", time.Since(started))
	assertAdmissionLedger(t, f, 1)
	require.NoError(t, integrationDB.QueryRow(`SELECT state FROM request_leases WHERE id=$1`, unknown.Snapshot.Lease.ID).Scan(&state))
	require.Equal(t, "ORPHANED", state)
	require.Equal(t, int64(1), starts.Load(), "local recovery must never call upstream again")
	var bills, usages int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM credential_billing_outbox b JOIN request_leases l ON b.lease_id=l.id WHERE l.principal_id=$1 AND b.settled_at IS NOT NULL`, f.principal).Scan(&bills))
	require.Equal(t, 1, bills)
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM usage_logs WHERE user_id=$1`, f.user).Scan(&usages))
	require.Equal(t, 1, usages)
}
