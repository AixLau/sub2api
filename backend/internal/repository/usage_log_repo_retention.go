package repository

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

// GetUserGrowthRetention measures API activity and payment conversion for daily registration cohorts.
func (r *usageLogRepository) GetUserGrowthRetention(ctx context.Context, startTime, endTime time.Time) (result *usagestats.UserGrowthRetention, err error) {
	query := `
		WITH cohort_dates AS (
			SELECT generate_series(
				($1 AT TIME ZONE $3)::date,
				(($2 AT TIME ZONE $3)::date - 1),
				interval '1 day'
			)::date AS cohort_date
		), cohorts AS (
			SELECT id, created_at, (created_at AT TIME ZONE $3)::date AS cohort_date
			FROM users
			WHERE deleted_at IS NULL
			  AND created_at >= $1 AND created_at < $2
		), cohort_activity AS (
			SELECT
				c.cohort_date,
				c.id,
				BOOL_OR((ul.created_at AT TIME ZONE $3)::date = c.cohort_date + 1) AS retained_d1,
				BOOL_OR((ul.created_at AT TIME ZONE $3)::date = c.cohort_date + 7) AS retained_d7,
				BOOL_OR((ul.created_at AT TIME ZONE $3)::date = c.cohort_date + 30) AS retained_d30
			FROM cohorts c
			LEFT JOIN usage_logs ul
			  ON ul.user_id = c.id
			 AND ul.source = 'gateway'
			 AND ul.api_key_id IS NOT NULL
			 AND ul.created_at >= (c.cohort_date::timestamp AT TIME ZONE $3) + interval '1 day'
			 AND ul.created_at < (c.cohort_date::timestamp AT TIME ZONE $3) + interval '31 days'
			GROUP BY c.cohort_date, c.id
		), api_activity AS (
			SELECT
				c.id,
				COUNT(ul.user_id) > 0 AS active_user
			FROM cohorts c
			LEFT JOIN usage_logs ul
			  ON ul.user_id = c.id
			 AND ul.source = 'gateway'
			 AND ul.api_key_id IS NOT NULL
			 AND ul.created_at >= c.created_at
			 AND ul.created_at < $2
			GROUP BY c.id
		), payment_by_user AS (
			SELECT
				c.id,
				COUNT(po.id) AS payment_count,
				COALESCE(SUM(po.amount), 0) AS recharge_amount
			FROM cohorts c
			LEFT JOIN payment_orders po
			  ON po.user_id = c.id
			 AND po.order_type = 'balance'
			 AND po.status IN ('PAID', 'RECHARGING', 'COMPLETED')
			 AND po.paid_at >= c.created_at
			 AND po.paid_at < c.created_at + interval '30 days'
			GROUP BY c.id
		), cohort_stats AS (
			SELECT
				ca.cohort_date,
				COUNT(*) AS registrations,
				COUNT(*) FILTER (WHERE ca.retained_d1) AS d1_retained,
				COUNT(*) FILTER (WHERE ca.retained_d7) AS d7_retained,
				COUNT(*) FILTER (WHERE ca.retained_d30) AS d30_retained,
				COUNT(*) FILTER (WHERE p.payment_count >= 1) AS paid_users,
				COUNT(*) FILTER (WHERE p.payment_count >= 2) AS repeat_buyers,
				COALESCE(SUM(p.recharge_amount), 0) AS recharge_amount,
				COUNT(*) FILTER (WHERE a.active_user) AS active_users
			FROM cohort_activity ca
			JOIN payment_by_user p ON p.id = ca.id
			JOIN api_activity a ON a.id = ca.id
			GROUP BY ca.cohort_date
		)
		SELECT
			TO_CHAR(d.cohort_date, 'YYYY-MM-DD'),
			COALESCE(s.registrations, 0),
			COALESCE(s.d1_retained, 0),
			COALESCE(s.d7_retained, 0),
			COALESCE(s.d30_retained, 0),
			COALESCE(s.paid_users, 0),
			COALESCE(s.repeat_buyers, 0),
			COALESCE(s.recharge_amount, 0),
			COALESCE(s.active_users, 0)
		FROM cohort_dates d
		LEFT JOIN cohort_stats s USING (cohort_date)
		ORDER BY d.cohort_date ASC`

	rows, err := r.sql.QueryContext(ctx, query, startTime, endTime, timezone.Name())
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
			result = nil
		}
	}()

	result = &usagestats.UserGrowthRetention{Cohorts: make([]usagestats.UserRetentionPoint, 0)}
	today := timezone.Today()
	var eligible [3]int64
	var retained [3]int64
	var paymentEligible, paidUsers, repeatBuyers int64
	for rows.Next() {
		var point usagestats.UserRetentionPoint
		if err = rows.Scan(
			&point.Date,
			&point.Registrations,
			&point.D1Retained,
			&point.D7Retained,
			&point.D30Retained,
			&point.PaidUsers,
			&point.RepeatBuyers,
			&point.RechargeAmount,
			&point.ActiveUsers,
		); err != nil {
			return nil, err
		}
		cohortDate, parseErr := time.ParseInLocation("2006-01-02", point.Date, timezone.Location())
		if parseErr != nil {
			return nil, parseErr
		}
		ages := []int{1, 7, 30}
		counts := []int64{point.D1Retained, point.D7Retained, point.D30Retained}
		rates := []**float64{&point.D1Rate, &point.D7Rate, &point.D30Rate}
		for i, age := range ages {
			if cohortDate.AddDate(0, 0, age).Before(today) && point.Registrations > 0 {
				rate := retentionRate(counts[i], point.Registrations)
				*rates[i] = &rate
				eligible[i] += point.Registrations
				retained[i] += counts[i]
			}
		}
		if cohortDate.AddDate(0, 0, 30).Before(today) && point.Registrations > 0 {
			paidRate := retentionRate(point.PaidUsers, point.Registrations)
			point.PaidRate = &paidRate
			paymentEligible += point.Registrations
			paidUsers += point.PaidUsers
			if point.PaidUsers > 0 {
				repeatRate := retentionRate(point.RepeatBuyers, point.PaidUsers)
				point.RepeatBuyRate = &repeatRate
				repeatBuyers += point.RepeatBuyers
			}
		}
		result.Cohorts = append(result.Cohorts, point)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	summaryRates := []**float64{&result.Summary.D1Rate, &result.Summary.D7Rate, &result.Summary.D30Rate}
	for i := range eligible {
		if eligible[i] > 0 {
			rate := retentionRate(retained[i], eligible[i])
			*summaryRates[i] = &rate
		}
	}
	if paymentEligible > 0 {
		rate := retentionRate(paidUsers, paymentEligible)
		result.Summary.PaidRate = &rate
	}
	if paidUsers > 0 {
		rate := retentionRate(repeatBuyers, paidUsers)
		result.Summary.RepeatBuyRate = &rate
	}
	return result, nil
}

func retentionRate(retained, cohortSize int64) float64 {
	if cohortSize == 0 {
		return 0
	}
	return float64(retained) * 100 / float64(cohortSize)
}
