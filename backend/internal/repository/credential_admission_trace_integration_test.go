//go:build integration

package repository

import (
	"context"
	"database/sql"
	"testing"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestCredentialAdmissionTraceCountsRetriesAndCommit(t *testing.T) {
	ctx := context.Background()
	f := newAdmissionFixture(t, 1)
	connector, err := pq.NewConnector(integrationDSN)
	require.NoError(t, err)
	totals := &admissionSQLCounts{}
	db := sql.OpenDB(admissionTraceConnector{Connector: connector, totals: totals})
	defer db.Close()
	held, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer held.Rollback()
	_, err = held.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, -f.principal)
	require.NoError(t, err)
	traceCtx, trace := newAdmissionTrace(ctx)
	tx, err := db.BeginTx(traceCtx, nil)
	require.NoError(t, err)
	var allowed bool
	require.NoError(t, tx.QueryRowContext(traceCtx, `SELECT pg_try_advisory_xact_lock($1)`, -f.principal).Scan(&allowed))
	require.False(t, allowed)
	require.NoError(t, tx.Rollback())
	require.NoError(t, held.Rollback())
	tx, err = db.BeginTx(traceCtx, nil)
	require.NoError(t, err)
	require.NoError(t, tx.QueryRowContext(traceCtx, `SELECT pg_try_advisory_xact_lock($1)`, -f.principal).Scan(&allowed))
	require.True(t, allowed)
	var occupied int
	require.NoError(t, tx.QueryRowContext(traceCtx, `SELECT occupied FROM upstream_principals WHERE id=$1 FOR NO KEY UPDATE`, f.principal).Scan(&occupied))
	require.NoError(t, tx.Commit())
	trace.mu.Lock()
	defer trace.mu.Unlock()
	require.Equal(t, 1, trace.counts["try_lock.missed"])
	require.Equal(t, 1, trace.counts["try_lock.acquired"])
	require.Equal(t, 1, trace.counts["transaction.committed"])
	require.Equal(t, 1, trace.counts["transaction.rolled_back"])
	require.Greater(t, trace.values["all_transaction_attempts"], trace.values["transaction"])
	require.Positive(t, trace.values["principal_held_lower_bound"])
	totals.mu.Lock()
	defer totals.mu.Unlock()
	require.Equal(t, int64(2), totals.transactions)
	require.Equal(t, int64(1), totals.commits)
	require.Equal(t, int64(1), totals.rollbacks)
	require.Equal(t, int64(1), totals.tryLockMisses)
	require.Equal(t, int64(1), totals.tryLockAcquired)
}
