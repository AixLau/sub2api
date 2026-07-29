package paidchurn

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"entgo.io/ent/dialect"
)

const (
	BucketSevenToFourteen     = "7_14"
	BucketFifteenToTwentyNine = "15_29"
	BucketThirtyPlus          = "30_plus"
)

type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

type Stats struct {
	TotalPaidUsers      int `json:"total_paid_users"`
	SevenToFourteen     int `json:"days_7_to_14"`
	FifteenToTwentyNine int `json:"days_15_to_29"`
	ThirtyPlus          int `json:"days_30_plus"`
}

func ValidBucket(bucket string) bool {
	switch bucket {
	case BucketSevenToFourteen, BucketFifteenToTwentyNine, BucketThirtyPlus:
		return true
	default:
		return false
	}
}

func GetStats(ctx context.Context, q Querier, driverDialect string, now time.Time) (Stats, error) {
	placeholders := bindVars(driverDialect, 3)
	query := churnCandidatesSQL + fmt.Sprintf(`
		SELECT
			(SELECT COUNT(DISTINCT user_id) FROM paid_users) AS total_paid_users,
			COALESCE(SUM(CASE WHEN exhausted_at > %s AND exhausted_at <= %s THEN 1 ELSE 0 END), 0) AS days_7_to_14,
			COALESCE(SUM(CASE WHEN exhausted_at > %s AND exhausted_at <= %s THEN 1 ELSE 0 END), 0) AS days_15_to_29,
			COALESCE(SUM(CASE WHEN exhausted_at <= %s THEN 1 ELSE 0 END), 0) AS days_30_plus
		FROM dedup
	`, placeholders[1], placeholders[0], placeholders[2], placeholders[1], placeholders[2])

	rows, err := q.QueryContext(ctx, query, now.AddDate(0, 0, -7), now.AddDate(0, 0, -15), now.AddDate(0, 0, -30))
	if err != nil {
		return Stats{}, err
	}
	defer func() { _ = rows.Close() }()

	var stats Stats
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return Stats{}, err
		}
		return stats, nil
	}
	if err := rows.Scan(&stats.TotalPaidUsers, &stats.SevenToFourteen, &stats.FifteenToTwentyNine, &stats.ThirtyPlus); err != nil {
		return Stats{}, err
	}
	return stats, rows.Err()
}

func ListUserIDs(ctx context.Context, q Querier, driverDialect, bucket string, now time.Time) ([]int64, error) {
	if !ValidBucket(bucket) {
		return nil, nil
	}
	placeholders := bindVars(driverDialect, 3)
	var where string
	switch bucket {
	case BucketSevenToFourteen:
		where = fmt.Sprintf("exhausted_at > %s AND exhausted_at <= %s", placeholders[1], placeholders[0])
	case BucketFifteenToTwentyNine:
		where = fmt.Sprintf("exhausted_at > %s AND exhausted_at <= %s", placeholders[2], placeholders[1])
	case BucketThirtyPlus:
		where = fmt.Sprintf("exhausted_at <= %s", placeholders[2])
	}

	rows, err := q.QueryContext(
		ctx,
		churnCandidatesSQL+" SELECT user_id FROM dedup WHERE "+where+" ORDER BY exhausted_at ASC, user_id ASC",
		now.AddDate(0, 0, -7),
		now.AddDate(0, 0, -15),
		now.AddDate(0, 0, -30),
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func bindVars(driverDialect string, count int) []string {
	vars := make([]string, count)
	for i := range vars {
		if driverDialect == dialect.Postgres {
			vars[i] = fmt.Sprintf("$%d", i+1)
		} else {
			vars[i] = fmt.Sprintf("?%d", i+1)
		}
	}
	return vars
}

const churnCandidatesSQL = `
	WITH valid_orders AS (
		SELECT user_id, order_type, paid_at
		FROM payment_orders
		WHERE paid_at IS NOT NULL
		  AND COALESCE(refund_amount, 0) < amount
	),
	paid_users AS (
		SELECT DISTINCT vo.user_id
		FROM valid_orders vo
		JOIN users u ON u.id = vo.user_id
		WHERE u.role = 'user' AND u.deleted_at IS NULL
	),
	balance_candidates AS (
		SELECT u.id AS user_id, MAX(ul.created_at) AS exhausted_at
		FROM users u
		LEFT JOIN usage_logs ul ON ul.user_id = u.id AND ul.subscription_id IS NULL
		WHERE u.role = 'user'
		  AND u.deleted_at IS NULL
		  AND u.balance <= 0
		  AND EXISTS (
			SELECT 1 FROM valid_orders vo
			WHERE vo.user_id = u.id AND vo.order_type = 'balance'
		  )
		GROUP BY u.id
		HAVING MAX(ul.created_at) IS NOT NULL
	),
	subscription_expired AS (
		SELECT us.user_id, us.expires_at AS exhausted_at
		FROM user_subscriptions us
		JOIN users u ON u.id = us.user_id
		WHERE u.role = 'user'
		  AND u.deleted_at IS NULL
		  AND us.deleted_at IS NULL
		  AND us.expires_at < CURRENT_TIMESTAMP
		  AND EXISTS (
			SELECT 1 FROM valid_orders vo
			WHERE vo.user_id = us.user_id AND vo.order_type = 'subscription'
		  )
	),
	subscription_exhausted AS (
		SELECT us.user_id, MAX(ul.created_at) AS exhausted_at
		FROM user_subscriptions us
		JOIN users u ON u.id = us.user_id
		JOIN groups g ON g.id = us.group_id
		LEFT JOIN usage_logs ul
		  ON ul.subscription_id = us.id
		 AND ul.created_at >= us.monthly_window_start
		WHERE u.role = 'user'
		  AND u.deleted_at IS NULL
		  AND us.deleted_at IS NULL
		  AND g.monthly_limit_usd > 0
		  AND us.monthly_usage_usd >= g.monthly_limit_usd
		  AND EXISTS (
			SELECT 1 FROM valid_orders vo
			WHERE vo.user_id = us.user_id AND vo.order_type = 'subscription'
		  )
		GROUP BY us.user_id, us.id
		HAVING MAX(ul.created_at) IS NOT NULL
	),
	candidates AS (
		SELECT * FROM balance_candidates
		UNION ALL
		SELECT * FROM subscription_expired
		UNION ALL
		SELECT * FROM subscription_exhausted
	),
	no_repayment AS (
		SELECT c.*
		FROM candidates c
		WHERE NOT EXISTS (
			SELECT 1 FROM valid_orders vo
			WHERE vo.user_id = c.user_id AND vo.paid_at > c.exhausted_at
		)
	),
	dedup AS (
		SELECT user_id, MAX(exhausted_at) AS exhausted_at
		FROM no_repayment
		GROUP BY user_id
	)
`
