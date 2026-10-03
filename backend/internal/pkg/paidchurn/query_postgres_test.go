//go:build integration

package paidchurn

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// Exercise the production dialect and verify that adding historical usage does
// not turn a latest-usage lookup back into a scan of every request. Check rows
// visited rather than wall-clock thresholds, which vary across CI machines.
func TestGetStatsPostgresBoundsUsageReads(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, err := tcpostgres.Run(ctx, "postgres:18.1-alpine3.23",
		tcpostgres.WithDatabase("paidchurn"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
		tcpostgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, container.Terminate(context.Background())) })
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.ExecContext(ctx, `
		CREATE TABLE users (id bigint PRIMARY KEY, role text, deleted_at timestamptz, balance numeric);
		CREATE TABLE payment_orders (user_id bigint, order_type text, paid_at timestamptz, refund_amount numeric, amount numeric);
		CREATE TABLE usage_logs (user_id bigint, subscription_id bigint, created_at timestamptz NOT NULL);
		CREATE TABLE user_subscriptions (id bigint PRIMARY KEY, user_id bigint, group_id bigint, expires_at timestamptz, deleted_at timestamptz, monthly_window_start timestamptz, monthly_usage_usd numeric);
		CREATE TABLE groups (id bigint PRIMARY KEY, monthly_limit_usd numeric);
		INSERT INTO users SELECT i, 'user', NULL, 0 FROM generate_series(1, 100) i;
		INSERT INTO groups VALUES (1, 100);
		INSERT INTO payment_orders
			SELECT id, CASE WHEN id <= 50 THEN 'balance' ELSE 'subscription' END,
				CURRENT_TIMESTAMP - interval '90 days', 0, 100 FROM users;
		INSERT INTO user_subscriptions
			SELECT id, id, 1, CURRENT_TIMESTAMP + interval '30 days', NULL,
				CURRENT_TIMESTAMP - interval '30 days', 100 FROM users WHERE id > 50;
		INSERT INTO usage_logs
			SELECT u.id, CASE WHEN u.id > 50 THEN u.id END,
				CURRENT_TIMESTAMP - interval '10 days' - i * interval '1 minute'
			FROM users u CROSS JOIN generate_series(1, 2000) i;
		CREATE INDEX idx_usage_logs_user_created ON usage_logs(user_id, created_at);
		CREATE INDEX idx_usage_logs_sub_created ON usage_logs(subscription_id, created_at);
		ANALYZE;
	`)
	require.NoError(t, err)

	q := &churnRecordingQuerier{DB: db}
	stats, err := GetStats(ctx, q, dialect.Postgres, time.Now())
	require.NoError(t, err)
	require.Equal(t, Stats{TotalPaidUsers: 100, SevenToFourteen: 100}, stats)

	var raw []byte
	err = db.QueryRowContext(ctx, "EXPLAIN (ANALYZE, FORMAT JSON) "+q.query, q.args...).Scan(&raw)
	require.NoError(t, err)
	var plans []struct{ Plan churnQueryPlan }
	require.NoError(t, json.Unmarshal(raw, &plans))
	require.Len(t, plans, 1)
	visited := map[string]bool{}
	var check func(churnQueryPlan)
	check = func(plan churnQueryPlan) {
		if plan.Relation == "usage_logs" && plan.Loops > 0 {
			require.LessOrEqual(t, plan.Rows, float64(1), "usage lookup read historical rows: %+v", plan)
			visited[plan.Index] = true
		}
		for _, child := range plan.Plans {
			check(child)
		}
	}
	check(plans[0].Plan)
	require.True(t, visited["idx_usage_logs_user_created"], "balance lookup must execute")
	require.True(t, visited["idx_usage_logs_sub_created"], "subscription lookup must execute")
}

type churnQueryPlan struct {
	Relation string           `json:"Relation Name"`
	Index    string           `json:"Index Name"`
	Rows     float64          `json:"Actual Rows"`
	Loops    float64          `json:"Actual Loops"`
	Plans    []churnQueryPlan `json:"Plans"`
}

type churnRecordingQuerier struct {
	*sql.DB
	query string
	args  []any
}

func (q *churnRecordingQuerier) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	q.query, q.args = query, args
	return q.DB.QueryContext(ctx, query, args...)
}
