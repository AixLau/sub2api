package paidchurn

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestGetStatsUsesMutuallyExclusiveBucketsAndExcludesRepayment(t *testing.T) {
	db, err := sql.Open("sqlite", "file:paid_churn_stats?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	for _, statement := range []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY, role TEXT, deleted_at DATETIME, balance NUMERIC)`,
		`CREATE TABLE payment_orders (user_id INTEGER, order_type TEXT, paid_at DATETIME, refund_amount NUMERIC, amount NUMERIC)`,
		`CREATE TABLE usage_logs (user_id INTEGER, subscription_id INTEGER, created_at DATETIME)`,
		`CREATE TABLE user_subscriptions (id INTEGER PRIMARY KEY, user_id INTEGER, group_id INTEGER, expires_at DATETIME, deleted_at DATETIME, monthly_window_start DATETIME, monthly_usage_usd NUMERIC)`,
		`CREATE TABLE groups (id INTEGER PRIMARY KEY, monthly_limit_usd NUMERIC)`,
	} {
		_, err = db.Exec(statement)
		require.NoError(t, err)
	}

	now := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	for id := 1; id <= 6; id++ {
		balance := 10.0
		if id >= 2 && id <= 5 {
			balance = -0.1
		}
		_, err = db.Exec(`INSERT INTO users (id, role, balance) VALUES (?, 'user', ?)`, id, balance)
		require.NoError(t, err)
	}

	paidAt := now.AddDate(0, 0, -45)
	for id := 1; id <= 5; id++ {
		_, err = db.Exec(`INSERT INTO payment_orders (user_id, order_type, paid_at, refund_amount, amount) VALUES (?, 'balance', ?, 0, 100)`, id, paidAt)
		require.NoError(t, err)
	}
	_, err = db.Exec(`INSERT INTO payment_orders (user_id, order_type, paid_at, refund_amount, amount) VALUES (6, 'balance', ?, 100, 100)`, paidAt)
	require.NoError(t, err)

	usageDays := map[int]int{2: 10, 3: 20, 4: 40, 5: 20}
	for id, days := range usageDays {
		_, err = db.Exec(`INSERT INTO usage_logs (user_id, created_at) VALUES (?, ?)`, id, now.AddDate(0, 0, -days))
		require.NoError(t, err)
	}
	_, err = db.Exec(`INSERT INTO payment_orders (user_id, order_type, paid_at, refund_amount, amount) VALUES (5, 'subscription', ?, 0, 50)`, now.AddDate(0, 0, -1))
	require.NoError(t, err)

	stats, err := GetStats(context.Background(), db, dialect.SQLite, now)
	require.NoError(t, err)
	require.Equal(t, Stats{
		TotalPaidUsers:      5,
		SevenToFourteen:     1,
		FifteenToTwentyNine: 1,
		ThirtyPlus:          1,
	}, stats)

	ids, err := ListUserIDs(context.Background(), db, dialect.SQLite, BucketFifteenToTwentyNine, now)
	require.NoError(t, err)
	require.Equal(t, []int64{3}, ids)
}
