//go:build integration

package repository

import (
	"context"
	"database/sql/driver"
	"strings"
	"sync"
	"time"
)

// Test-only transparent lib/pq instrumentation. It records no SQL arguments,
// request IDs or secrets. No query, isolation level, pool size or lock changes.
// SQL wall time includes network, execution, row decoding and any lock wait;
// pg_stat_activity sampling below separately identifies database wait events.
type admissionTraceKey struct{}
type admissionTrace struct {
	mu                         sync.Mutex
	started, txStarted, locked time.Time
	values                     map[string]time.Duration
	counts                     map[string]int
}

func newAdmissionTrace(ctx context.Context) (context.Context, *admissionTrace) {
	t := &admissionTrace{started: time.Now(), values: map[string]time.Duration{}, counts: map[string]int{}}
	return context.WithValue(ctx, admissionTraceKey{}, t), t
}
func (t *admissionTrace) add(phase string, elapsed time.Duration) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.values[phase] += elapsed
	t.counts[phase]++
	if phase == "sql.user_lock" && t.locked.IsZero() {
		t.locked = time.Now()
	}
}
func (t *admissionTrace) end() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.values["transaction"] = time.Since(t.txStarted)
	if !t.locked.IsZero() {
		t.values["lock_held_lower_bound"] = time.Since(t.locked)
	}
}
func admissionSQLPhase(query string) string {
	q := strings.Join(strings.Fields(query), " ")
	switch {
	case strings.HasPrefix(q, "WITH user_capacity AS"):
		return "sql.capacity_batch"
	case strings.HasPrefix(q, "INSERT INTO principal_user_capacity"):
		return "sql.user_ensure"
	case strings.HasPrefix(q, "SELECT occupied FROM principal_user_capacity"):
		return "sql.user_lock"
	case strings.Contains(q, "FROM upstream_principals WHERE") && strings.Contains(q, "FOR NO KEY UPDATE"):
		return "sql.principal_lock"
	case strings.Contains(q, "SELECT (SELECT count(*) FROM request_leases"):
		return "sql.ledger_check"
	case strings.Contains(q, "upstream_principal_quota_domains"):
		return "sql.quota"
	case strings.HasPrefix(q, "SELECT k.group_id"):
		return "sql.authorization"
	case strings.Contains(q, "SELECT id,payload_digest"):
		return "sql.idempotency"
	case strings.Contains(q, "FOR UPDATE OF i"):
		return "sql.candidate_lock"
	case strings.HasPrefix(q, "SELECT i.id,i.weight"):
		return "sql.fairness_demand"
	case strings.HasPrefix(q, "SELECT last_admitted_at"):
		return "sql.fairness_arrival"
	case strings.HasPrefix(q, "SELECT t.user_id"):
		return "sql.fairness_queue"
	case strings.HasPrefix(q, "UPDATE admission_tickets SET state='EXPIRED'"):
		return "sql.ticket_cleanup"
	case strings.Contains(q, "FROM admission_tickets"):
		return "sql.ticket_read"
	case strings.Contains(q, "FROM credential_identity_profiles"):
		return "sql.snapshot"
	case strings.Contains(q, "FROM credential_instances") && strings.Contains(q, "FOR UPDATE"):
		return "sql.instance_lock"
	case strings.Contains(q, "FROM request_leases") && strings.Contains(q, "FOR UPDATE"):
		return "sql.lease_lock"
	case strings.HasPrefix(q, "SELECT p.admission_epoch"):
		return "sql.dispatch_check"
	case strings.HasPrefix(q, "SELECT clock_timestamp()"):
		return "sql.wall_clock"
	case strings.HasPrefix(q, "INSERT"):
		return "sql.insert"
	case strings.HasPrefix(q, "UPDATE"):
		return "sql.update"
	default:
		return "sql.other"
	}
}

type admissionTraceConnector struct{ driver.Connector }

func (c admissionTraceConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &admissionTraceConn{Conn: conn}, nil
}

type admissionTraceConn struct{ driver.Conn }

func (c *admissionTraceConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	trace, _ := ctx.Value(admissionTraceKey{}).(*admissionTrace)
	started := time.Now()
	if trace != nil {
		trace.mu.Lock()
		trace.txStarted = started
		trace.values["connection_acquire"] = started.Sub(trace.started)
		trace.mu.Unlock()
	}
	tx, err := c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
	trace.add("begin", time.Since(started))
	if err != nil {
		return nil, err
	}
	return &admissionTraceTx{Tx: tx, trace: trace}, nil
}
func (c *admissionTraceConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	trace, _ := ctx.Value(admissionTraceKey{}).(*admissionTrace)
	started := time.Now()
	r, err := c.Conn.(driver.ExecerContext).ExecContext(ctx, q, args)
	trace.add(admissionSQLPhase(q), time.Since(started))
	return r, err
}
func (c *admissionTraceConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	trace, _ := ctx.Value(admissionTraceKey{}).(*admissionTrace)
	started := time.Now()
	r, err := c.Conn.(driver.QueryerContext).QueryContext(ctx, q, args)
	if err != nil {
		trace.add(admissionSQLPhase(q), time.Since(started))
		return nil, err
	}
	return &admissionTraceRows{Rows: r, trace: trace, phase: admissionSQLPhase(q), started: started}, nil
}

// Preserve the driver's optional connection validation/session reset behavior.
func (c *admissionTraceConn) Ping(ctx context.Context) error { return c.Conn.(driver.Pinger).Ping(ctx) }
func (c *admissionTraceConn) ResetSession(ctx context.Context) error {
	if r, ok := c.Conn.(driver.SessionResetter); ok {
		return r.ResetSession(ctx)
	}
	return nil
}
func (c *admissionTraceConn) IsValid() bool {
	if r, ok := c.Conn.(driver.Validator); ok {
		return r.IsValid()
	}
	return true
}

type admissionTraceRows struct {
	driver.Rows
	trace   *admissionTrace
	phase   string
	started time.Time
	once    sync.Once
}

func (r *admissionTraceRows) Close() error {
	err := r.Rows.Close()
	r.once.Do(func() { r.trace.add(r.phase, time.Since(r.started)) })
	return err
}

type admissionTraceTx struct {
	driver.Tx
	trace *admissionTrace
}

func (t *admissionTraceTx) Commit() error {
	started := time.Now()
	err := t.Tx.Commit()
	t.trace.add("commit", time.Since(started))
	t.trace.end()
	return err
}
func (t *admissionTraceTx) Rollback() error {
	started := time.Now()
	err := t.Tx.Rollback()
	t.trace.add("rollback", time.Since(started))
	t.trace.end()
	return err
}
