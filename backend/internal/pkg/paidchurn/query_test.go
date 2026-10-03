package paidchurn

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"regexp"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestGetStatsUsesMutuallyExclusiveBucketsAndExcludesRepayment(t *testing.T) {
	db := newChurnTestDB(t)
	now := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	for id := 1; id <= 6; id++ {
		balance := 10.0
		if id >= 2 && id <= 5 {
			balance = -0.1
		}
		_, err := db.Exec(`INSERT INTO users (id, role, balance) VALUES (?, 'user', ?)`, id, balance)
		require.NoError(t, err)
	}

	paidAt := now.AddDate(0, 0, -45)
	for id := 1; id <= 5; id++ {
		_, err := db.Exec(`INSERT INTO payment_orders (user_id, order_type, paid_at, refund_amount, amount) VALUES (?, 'balance', ?, 0, 100)`, id, paidAt)
		require.NoError(t, err)
	}
	_, err := db.Exec(`INSERT INTO payment_orders (user_id, order_type, paid_at, refund_amount, amount) VALUES (6, 'balance', ?, 100, 100)`, paidAt)
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

func newChurnTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
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

	return db
}

func TestLatestUsagePreservesChurnCandidates(t *testing.T) {
	db := newChurnTestDB(t)
	now := time.Now().UTC().Truncate(time.Second)
	ago := func(days int) time.Time { return now.AddDate(0, 0, -days) }
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := db.Exec(query, args...)
		require.NoError(t, err)
	}
	order := func(id int, kind string, days int, refund int) {
		t.Helper()
		exec(`INSERT INTO payment_orders VALUES (?, ?, ?, ?, 100)`, id, kind, ago(days), refund)
	}
	usage := func(id int, subscription any, days int) {
		t.Helper()
		exec(`INSERT INTO usage_logs VALUES (?, ?, ?)`, id, subscription, ago(days))
	}
	for id := 1; id <= 8; id++ {
		exec(`INSERT INTO users (id, role, balance) VALUES (?, 'user', 0)`, id)
		kind := "balance"
		if id >= 3 && id <= 5 {
			kind = "subscription"
		}
		order(id, kind, 90, 0)
	}

	// The last balance usage, not a newer subscription usage, sets the bucket.
	usage(1, nil, 20)
	usage(1, nil, 10)
	usage(1, 100, 1)
	// User 2 has paid but has no usage and must not become a churn candidate.

	exec(`INSERT INTO groups VALUES (1, 100)`)
	for id := 3; id <= 5; id++ {
		exec(`INSERT INTO user_subscriptions
			(id, user_id, group_id, expires_at, monthly_window_start, monthly_usage_usd)
			VALUES (?, ?, 1, ?, ?, 100)`, id, id, now.AddDate(0, 0, 30), ago(30))
		usage(id, id, 40) // Outside the current monthly window.
	}
	usage(3, 3, 20)
	usage(3, 3, 10)
	// User 4 has no usage in the current window and must not be counted.
	usage(5, 5, 10)
	exec(`UPDATE user_subscriptions SET expires_at = ? WHERE id = 5`, ago(20))
	// User 5 qualifies twice; retain only the newer exhaustion time.

	usage(6, nil, 10)
	order(6, "balance", 5, 100) // A fully refunded repayment does not reactivate.
	usage(7, nil, 10)
	order(7, "balance", 5, 50) // A partially refunded repayment does reactivate.

	// Repayment excludes only the earlier subscription candidate, not the
	// later balance candidate for the same user.
	order(8, "subscription", 90, 0)
	exec(`INSERT INTO user_subscriptions
		(id, user_id, group_id, expires_at, monthly_window_start, monthly_usage_usd)
		VALUES (8, 8, 1, ?, ?, 0)`, ago(40), ago(60))
	order(8, "balance", 30, 0)
	usage(8, nil, 20)

	stats, err := GetStats(context.Background(), db, dialect.SQLite, now)
	require.NoError(t, err)
	require.Equal(t, Stats{TotalPaidUsers: 8, SevenToFourteen: 4, FifteenToTwentyNine: 1}, stats)
	for bucket, expected := range map[string][]int64{
		BucketSevenToFourteen:     {1, 3, 5, 6},
		BucketFifteenToTwentyNine: {8},
		BucketThirtyPlus:          nil,
	} {
		ids, err := ListUserIDs(context.Background(), db, dialect.SQLite, bucket, now)
		require.NoError(t, err)
		require.Equal(t, expected, ids, bucket)
	}
}

func TestListUserIDsUsesContiguousPostgresParameters(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	now := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		bucket  string
		where   string
		args    []any
		userIDs []int64
	}{
		{
			name:    "seven to fourteen",
			bucket:  BucketSevenToFourteen,
			where:   "exhausted_at > $1 AND exhausted_at <= $2",
			args:    []any{now.AddDate(0, 0, -15), now.AddDate(0, 0, -7)},
			userIDs: []int64{7},
		},
		{
			name:    "fifteen to twenty nine",
			bucket:  BucketFifteenToTwentyNine,
			where:   "exhausted_at > $1 AND exhausted_at <= $2",
			args:    []any{now.AddDate(0, 0, -30), now.AddDate(0, 0, -15)},
			userIDs: []int64{15},
		},
		{
			name:    "thirty plus",
			bucket:  BucketThirtyPlus,
			where:   "exhausted_at <= $1",
			args:    []any{now.AddDate(0, 0, -30)},
			userIDs: []int64{30},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queryPattern := `(?s).*SELECT user_id FROM dedup WHERE ` + regexp.QuoteMeta(tt.where) + ` ORDER BY exhausted_at ASC, user_id ASC`
			expectedArgs := make([]driver.Value, len(tt.args))
			for i := range tt.args {
				expectedArgs[i] = tt.args[i]
			}
			rows := sqlmock.NewRows([]string{"user_id"})
			for _, userID := range tt.userIDs {
				rows.AddRow(userID)
			}
			mock.ExpectQuery(queryPattern).WithArgs(expectedArgs...).WillReturnRows(rows)

			ids, queryErr := ListUserIDs(context.Background(), db, dialect.Postgres, tt.bucket, now)
			require.NoError(t, queryErr)
			require.Equal(t, tt.userIDs, ids)
		})
	}

	require.NoError(t, mock.ExpectationsWereMet())
}
