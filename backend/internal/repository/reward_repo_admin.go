package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

func (r *rewardRepository) CreateCampaign(ctx context.Context, campaign service.RewardCampaign, actorID *int64) (*service.RewardCampaign, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	configRaw, configHash, err := marshalRewardConfig(campaign.Config)
	if err != nil {
		return nil, err
	}
	var campaignID int64
	err = tx.QueryRowContext(ctx, `
INSERT INTO reward_campaigns (
    system_key, name, description, status, issuance_mode, timezone, starts_at, ends_at,
    priority, total_budget, reserved_budget, spent_budget, released_budget,
    created_by, updated_by, created_at, updated_at
) VALUES ($1, $2, $3, 'draft', $4, $5, $6, $7, $8, $9, 0, 0, 0, $10, $10, NOW(), NOW())
RETURNING id
`, nullableString(campaign.CampaignKey), campaign.Name, campaign.Description, campaign.IssuanceMode,
		campaign.Timezone, nullableTime(campaign.StartsAt), nullableTime(campaign.EndsAt), campaign.Priority,
		campaign.TotalBudget, actorID).Scan(&campaignID)
	if err != nil {
		return nil, err
	}
	var versionID int64
	err = tx.QueryRowContext(ctx, `
INSERT INTO reward_campaign_versions (campaign_id, version_number, config, config_hash, created_by, created_at)
VALUES ($1, 1, $2::jsonb, $3, $4, NOW())
RETURNING id
`, campaignID, configRaw, configHash, actorID).Scan(&versionID)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE reward_campaigns SET current_version_id = $2 WHERE id = $1`, campaignID, versionID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetCampaign(ctx, campaignID)
}

func (r *rewardRepository) UpdateCampaign(ctx context.Context, campaign service.RewardCampaign, actorID *int64) (*service.RewardCampaign, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	current, err := getLockedRewardCampaign(ctx, tx, campaign.ID)
	if err != nil {
		return nil, err
	}
	if current.Status == service.RewardCampaignStatusArchived || current.Status == service.RewardCampaignStatusEnded {
		return nil, infraerrors.Conflict("REWARD_CAMPAIGN_IMMUTABLE", "ended or archived campaigns cannot be edited")
	}
	if current.Status != service.RewardCampaignStatusDraft && campaign.IssuanceMode != current.IssuanceMode {
		return nil, infraerrors.Conflict("REWARD_ISSUANCE_MODE_IMMUTABLE", "issuance mode cannot be changed after publishing")
	}
	if current.IssuanceMode == service.RewardIssuanceModeScheduledBatch &&
		(current.Status == service.RewardCampaignStatusActive || current.Status == service.RewardCampaignStatusScheduled) {
		return nil, infraerrors.Conflict("REWARD_BATCH_MUST_PAUSE", "pause the batch campaign before editing")
	}
	if campaign.TotalBudget+1e-8 < current.ReservedBudget+current.SpentBudget {
		return nil, infraerrors.Conflict("REWARD_BUDGET_BELOW_COMMITTED", "total budget cannot be lower than reserved plus spent budget")
	}
	var resumeCursor, maxUserID, totalUsers int64
	hasBatchSnapshot := false
	if current.IssuanceMode == service.RewardIssuanceModeScheduledBatch && current.Status == service.RewardCampaignStatusPaused {
		if _, err := tx.ExecContext(ctx, `
UPDATE reward_campaign_jobs
SET status = 'paused', lease_owner = '', lease_expires_at = NULL, updated_at = NOW()
WHERE campaign_id = $1
  AND status = 'processing'
  AND (lease_expires_at IS NULL OR lease_expires_at <= NOW())
`, campaign.ID); err != nil {
			return nil, err
		}
		var processing bool
		if err := tx.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1 FROM reward_campaign_jobs
    WHERE campaign_id = $1 AND status = 'processing'
)
`, campaign.ID).Scan(&processing); err != nil {
			return nil, err
		}
		if processing {
			return nil, infraerrors.Conflict("REWARD_BATCH_PAUSE_IN_PROGRESS", "wait for the current batch page to finish pausing")
		}
		err := tx.QueryRowContext(ctx, `
SELECT cursor_user_id, max_user_id, total_users
FROM reward_campaign_jobs
WHERE campaign_id = $1 AND campaign_version_id = $2
ORDER BY id DESC
LIMIT 1
`, campaign.ID, current.CurrentVersionID).Scan(&resumeCursor, &maxUserID, &totalUsers)
		if errors.Is(err, sql.ErrNoRows) {
			err = nil
		} else if err == nil {
			hasBatchSnapshot = true
		}
		if err != nil {
			return nil, err
		}
	}
	configRaw, configHash, err := marshalRewardConfig(campaign.Config)
	if err != nil {
		return nil, err
	}
	nextVersion := current.CurrentVersion + 1
	var versionID int64
	err = tx.QueryRowContext(ctx, `
INSERT INTO reward_campaign_versions (campaign_id, version_number, config, config_hash, created_by, created_at)
VALUES ($1, $2, $3::jsonb, $4, $5, NOW())
RETURNING id
`, campaign.ID, nextVersion, configRaw, configHash, actorID).Scan(&versionID)
	if err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE reward_campaigns
SET name = $2, description = $3, issuance_mode = $4, timezone = $5,
    starts_at = $6, ends_at = $7, priority = $8, total_budget = $9,
    current_version_id = $10, updated_by = $11, updated_at = NOW()
WHERE id = $1 AND $9 >= reserved_budget + spent_budget
`, campaign.ID, campaign.Name, campaign.Description, campaign.IssuanceMode, campaign.Timezone,
		nullableTime(campaign.StartsAt), nullableTime(campaign.EndsAt), campaign.Priority,
		campaign.TotalBudget, versionID, actorID)
	if err != nil {
		return nil, err
	}
	updated, _ := result.RowsAffected()
	if updated != 1 {
		return nil, infraerrors.Conflict("REWARD_CAMPAIGN_UPDATE_CONFLICT", "campaign changed while it was being updated")
	}
	if current.IssuanceMode == service.RewardIssuanceModeScheduledBatch && current.Status == service.RewardCampaignStatusPaused && hasBatchSnapshot {
		if _, err := tx.ExecContext(ctx, `
UPDATE reward_campaign_jobs
SET status = 'cancelled', lease_owner = '', lease_expires_at = NULL,
    finished_at = NOW(), updated_at = NOW()
WHERE campaign_id = $1 AND status IN ('pending', 'retry', 'paused')
`, campaign.ID); err != nil {
			return nil, err
		}
		scheduledAt := campaign.StartsAt
		if scheduledAt.IsZero() || scheduledAt.Before(time.Now().UTC()) {
			scheduledAt = time.Now().UTC()
		}
		if _, err := tx.ExecContext(ctx, `
		INSERT INTO reward_campaign_jobs (
		    campaign_id, campaign_version_id, job_type, idempotency_key, status,
		    cursor_user_id, max_user_id, total_users,
		    scheduled_at, next_attempt_at, created_at, updated_at
		) VALUES ($1, $2, 'issue_batch', $3, 'paused', $4, $5, $6, $7, $7, NOW(), NOW())
		ON CONFLICT (idempotency_key) DO NOTHING
`, campaign.ID, versionID,
			fmt.Sprintf("reward:campaign:%d:version:%d:issue", campaign.ID, versionID),
			resumeCursor, maxUserID, totalUsers, scheduledAt); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetCampaign(ctx, campaign.ID)
}

func (r *rewardRepository) CloneCampaign(ctx context.Context, campaignID int64, actorID *int64) (*service.RewardCampaign, error) {
	campaign, err := r.GetCampaign(ctx, campaignID)
	if err != nil {
		return nil, err
	}
	campaign.ID = 0
	campaign.CampaignKey = ""
	campaign.System = false
	campaign.Name += " (copy)"
	campaign.Status = service.RewardCampaignStatusDraft
	campaign.ReservedBudget = 0
	campaign.SpentBudget = 0
	campaign.ReleasedBudget = 0
	campaign.CurrentVersionID = 0
	campaign.CurrentVersion = 0
	return r.CreateCampaign(ctx, *campaign, actorID)
}

func (r *rewardRepository) GetCampaign(ctx context.Context, campaignID int64) (*service.RewardCampaign, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT c.id, c.system_key, c.name, c.description, c.status, c.issuance_mode, c.timezone,
       c.starts_at, c.ends_at, c.priority, c.total_budget, c.reserved_budget,
       c.spent_budget, c.released_budget, c.current_version_id,
       c.created_by, c.updated_by, c.created_at, c.updated_at,
       v.version_number, v.config
FROM reward_campaigns c
JOIN reward_campaign_versions v ON v.id = c.current_version_id
WHERE c.id = $1
`, campaignID)
	campaign, err := scanRewardCampaign(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, infraerrors.NotFound("REWARD_CAMPAIGN_NOT_FOUND", "reward campaign not found")
	}
	return campaign, err
}

func (r *rewardRepository) GetCampaignVersion(ctx context.Context, campaignID, versionID int64) (*service.RewardCampaign, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT c.id, c.system_key, c.name, c.description, c.status, c.issuance_mode, c.timezone,
       c.starts_at, c.ends_at, c.priority, c.total_budget, c.reserved_budget,
       c.spent_budget, c.released_budget, c.current_version_id,
       c.created_by, c.updated_by, c.created_at, c.updated_at,
       v.version_number, v.config
FROM reward_campaigns c
JOIN reward_campaign_versions v ON v.id = $2 AND v.campaign_id = c.id
WHERE c.id = $1
`, campaignID, versionID)
	campaign, err := scanRewardCampaign(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, infraerrors.NotFound("REWARD_CAMPAIGN_VERSION_NOT_FOUND", "reward campaign version not found")
	}
	if campaign != nil {
		campaign.CurrentVersionID = versionID
	}
	return campaign, err
}

func (r *rewardRepository) ListCampaigns(ctx context.Context, filter service.RewardCampaignListFilter) ([]service.RewardCampaign, int64, error) {
	conditions := []string{"1=1"}
	args := make([]any, 0, 4)
	if filter.Status != "" {
		args = append(args, filter.Status)
		conditions = append(conditions, fmt.Sprintf("c.status = $%d", len(args)))
	}
	if filter.Search != "" {
		args = append(args, "%"+strings.TrimSpace(filter.Search)+"%")
		conditions = append(conditions, fmt.Sprintf("(c.name ILIKE $%d OR c.description ILIKE $%d)", len(args), len(args)))
	}
	where := strings.Join(conditions, " AND ")
	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM reward_campaigns c WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, filter.Limit, filter.Offset)
	query := fmt.Sprintf(`
SELECT c.id, c.system_key, c.name, c.description, c.status, c.issuance_mode, c.timezone,
       c.starts_at, c.ends_at, c.priority, c.total_budget, c.reserved_budget,
       c.spent_budget, c.released_budget, c.current_version_id,
       c.created_by, c.updated_by, c.created_at, c.updated_at,
       v.version_number, v.config
FROM reward_campaigns c
JOIN reward_campaign_versions v ON v.id = c.current_version_id
WHERE %s
ORDER BY c.created_at DESC, c.id DESC
LIMIT $%d OFFSET $%d
`, where, len(args)-1, len(args))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	campaigns, err := scanRewardCampaignRows(rows)
	return campaigns, total, err
}

func (r *rewardRepository) TransitionCampaign(ctx context.Context, campaignID int64, action string, actorID *int64, now time.Time) (*service.RewardCampaign, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	campaign, err := getLockedRewardCampaign(ctx, tx, campaignID)
	if err != nil {
		return nil, err
	}
	status := campaign.Status
	switch action {
	case "publish":
		if status != service.RewardCampaignStatusDraft {
			return nil, infraerrors.Conflict("REWARD_CAMPAIGN_STATE_INVALID", "only draft campaigns can be published")
		}
		if !campaign.EndsAt.IsZero() && !now.Before(campaign.EndsAt) {
			return nil, infraerrors.Conflict("REWARD_CAMPAIGN_ALREADY_ENDED", "campaign end time has passed")
		}
		if campaign.WinProbability > 0 && campaign.ControlGroupPercent < 100 {
			minimumAmount := 0.0
			for _, tier := range campaign.Config.AmountTiers {
				if minimumAmount == 0 || tier.Amount < minimumAmount {
					minimumAmount = tier.Amount
				}
			}
			if minimumAmount <= 0 || campaign.TotalBudget+1e-8 < minimumAmount {
				return nil, infraerrors.Conflict("REWARD_BUDGET_TOO_SMALL", "budget must fund at least one configured reward")
			}
		}
		if len(campaign.Config.SkinWeights) > 0 {
			skinIDs := make([]int64, 0, len(campaign.Config.SkinWeights))
			for _, skin := range campaign.Config.SkinWeights {
				skinIDs = append(skinIDs, skin.SkinID)
			}
			var activeSkins int
			if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM reward_skins WHERE id = ANY($1::bigint[]) AND status = 'active'
`, pq.Array(skinIDs)).Scan(&activeSkins); err != nil {
				return nil, err
			}
			if activeSkins != len(skinIDs) {
				return nil, infraerrors.Conflict("REWARD_SKIN_UNAVAILABLE", "all selected skins must be active before publishing")
			}
		}
		status = service.RewardCampaignStatusActive
		if !campaign.StartsAt.IsZero() && now.Before(campaign.StartsAt) {
			status = service.RewardCampaignStatusScheduled
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE reward_campaigns
SET status = $2, published_at = $3, paused_at = NULL, updated_by = $4, updated_at = NOW()
WHERE id = $1
`, campaignID, status, now, actorID); err != nil {
			return nil, err
		}
		if campaign.IssuanceMode == service.RewardIssuanceModeScheduledBatch && status == service.RewardCampaignStatusActive {
			scheduledAt := now
			if !campaign.StartsAt.IsZero() && campaign.StartsAt.After(now) {
				scheduledAt = campaign.StartsAt
			}
			if _, err := tx.ExecContext(ctx, `
			INSERT INTO reward_campaign_jobs (
			    campaign_id, campaign_version_id, job_type, idempotency_key, status,
			    cursor_user_id, max_user_id, total_users,
			    scheduled_at, next_attempt_at, created_at, updated_at
			) VALUES (
			    $1, $2, 'issue_batch', $3, 'pending', 0,
			    (SELECT COALESCE(MAX(id), 0) FROM users WHERE role = 'user' AND status = 'active' AND deleted_at IS NULL),
			    (SELECT COUNT(*) FROM users WHERE role = 'user' AND status = 'active' AND deleted_at IS NULL),
			    $4, $4, NOW(), NOW()
			)
ON CONFLICT (idempotency_key) DO NOTHING
`, campaignID, campaign.CurrentVersionID,
				fmt.Sprintf("reward:campaign:%d:version:%d:issue", campaignID, campaign.CurrentVersionID), scheduledAt); err != nil {
				return nil, err
			}
		}
	case "pause":
		if status != service.RewardCampaignStatusActive && status != service.RewardCampaignStatusScheduled {
			return nil, infraerrors.Conflict("REWARD_CAMPAIGN_STATE_INVALID", "only scheduled or active campaigns can be paused")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE reward_campaigns SET status = 'paused', paused_at = $2, updated_by = $3, updated_at = NOW() WHERE id = $1`, campaignID, now, actorID); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE reward_campaign_jobs SET status = 'paused', lease_owner = '', lease_expires_at = NULL, updated_at = NOW() WHERE campaign_id = $1 AND status IN ('pending','retry')`, campaignID); err != nil {
			return nil, err
		}
	case "resume":
		if status != service.RewardCampaignStatusPaused {
			return nil, infraerrors.Conflict("REWARD_CAMPAIGN_STATE_INVALID", "only paused campaigns can be resumed")
		}
		status = service.RewardCampaignStatusActive
		if !campaign.StartsAt.IsZero() && campaign.StartsAt.After(now) {
			status = service.RewardCampaignStatusScheduled
		}
		if _, err := tx.ExecContext(ctx, `UPDATE reward_campaigns SET status = $2, paused_at = NULL, updated_by = $3, updated_at = NOW() WHERE id = $1`, campaignID, status, actorID); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE reward_campaign_jobs SET status = 'pending', next_attempt_at = $2, updated_at = NOW() WHERE campaign_id = $1 AND status = 'paused'`, campaignID, now); err != nil {
			return nil, err
		}
	case "end":
		if status == service.RewardCampaignStatusEnded || status == service.RewardCampaignStatusArchived {
			return nil, infraerrors.Conflict("REWARD_CAMPAIGN_STATE_INVALID", "campaign is already ended")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE reward_campaigns SET status = 'ended', ended_at = $2, updated_by = $3, updated_at = NOW() WHERE id = $1`, campaignID, now, actorID); err != nil {
			return nil, err
		}
		if _, _, err := expireAllRewardGrantsLocked(ctx, tx, campaignID, now); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE reward_campaign_jobs SET status = 'cancelled', lease_owner = '', lease_expires_at = NULL, finished_at = $2, updated_at = NOW() WHERE campaign_id = $1 AND status NOT IN ('succeeded','failed','dead_letter','cancelled')`, campaignID, now); err != nil {
			return nil, err
		}
	case "archive":
		if status != service.RewardCampaignStatusEnded {
			return nil, infraerrors.Conflict("REWARD_CAMPAIGN_STATE_INVALID", "only ended campaigns can be archived")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE reward_campaigns SET status = 'archived', archived_at = $2, updated_by = $3, updated_at = NOW() WHERE id = $1`, campaignID, now, actorID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetCampaign(ctx, campaignID)
}

func expireAllRewardGrantsLocked(ctx context.Context, tx *sql.Tx, campaignID int64, now time.Time) (int64, float64, error) {
	var count int64
	var released float64
	if err := tx.QueryRowContext(ctx, `
WITH expired AS (
    UPDATE user_reward_grants
    SET status = 'expired', expired_at = $2, updated_at = NOW()
    WHERE campaign_id = $1 AND status = 'pending'
    RETURNING amount
)
SELECT COUNT(*), COALESCE(SUM(amount), 0) FROM expired
`, campaignID, now).Scan(&count, &released); err != nil {
		return 0, 0, err
	}
	if released > 0 {
		result, err := tx.ExecContext(ctx, `UPDATE reward_campaigns SET reserved_budget = reserved_budget - $2, released_budget = released_budget + $2, updated_at = NOW() WHERE id = $1 AND reserved_budget >= $2`, campaignID, released)
		if err != nil {
			return 0, 0, err
		}
		if updated, _ := result.RowsAffected(); updated != 1 {
			return 0, 0, fmt.Errorf("reward campaign %d reservation release mismatch", campaignID)
		}
	}
	return count, released, nil
}

func (r *rewardRepository) EstimateAudience(ctx context.Context, audience service.RewardAudience, now time.Time) (int64, time.Time, error) {
	audience = service.ResolveRewardAudienceRelativeTimes(audience, now)
	where, args, err := compileRewardAudienceSQL(audience, now, 2)
	if err != nil {
		return 0, time.Time{}, infraerrors.BadRequest("REWARD_AUDIENCE_INVALID", err.Error())
	}
	query := `
WITH recent_behavior AS (
    SELECT user_id,
           SUM(request_count) FILTER (WHERE bucket_start >= $1::timestamptz - INTERVAL '7 days') AS requests_7d,
           SUM(request_count) AS requests_30d,
           SUM(actual_cost) FILTER (WHERE bucket_start >= $1::timestamptz - INTERVAL '7 days') AS actual_cost_7d,
           SUM(actual_cost) AS actual_cost_30d,
           SUM(recharge_amount) AS recharge_30d,
           MAX(updated_at) AS data_updated_at
    FROM user_behavior_daily
    WHERE bucket_start >= $1::timestamptz - INTERVAL '30 days'
    GROUP BY user_id
), last_api AS (
    SELECT DISTINCT ON (user_id)
           user_id, last_api_use_at AS last_api_used_at, updated_at AS data_updated_at
    FROM user_behavior_daily
    WHERE last_api_use_at IS NOT NULL
    ORDER BY user_id, last_api_use_at DESC, updated_at DESC, id DESC
), behavior AS (
    SELECT COALESCE(r.user_id, l.user_id) AS user_id,
           r.requests_7d, r.requests_30d, r.actual_cost_7d, r.actual_cost_30d,
           l.last_api_used_at, r.recharge_30d,
           GREATEST(r.data_updated_at, l.data_updated_at) AS data_updated_at
    FROM recent_behavior r
    FULL OUTER JOIN last_api l ON l.user_id = r.user_id
)
SELECT COUNT(*), COALESCE(MAX(b.data_updated_at), $1)
FROM users u
LEFT JOIN behavior b ON b.user_id = u.id
WHERE u.role = 'user' AND u.status = 'active' AND u.deleted_at IS NULL
  AND POSITION('+' IN SPLIT_PART(u.email, '@', 1)) = 0
  AND (` + where + `)
`
	queryArgs := append([]any{now}, args...)
	var count int64
	var updatedAt time.Time
	if err := r.db.QueryRowContext(ctx, query, queryArgs...).Scan(&count, &updatedAt); err != nil {
		return 0, time.Time{}, err
	}
	return count, updatedAt, nil
}

func compileRewardAudienceSQL(audience service.RewardAudience, now time.Time, firstPlaceholder int) (string, []any, error) {
	if len(audience.AnyOf) == 0 {
		return "TRUE", nil, nil
	}
	args := make([]any, 0)
	groups := make([]string, 0, len(audience.AnyOf))
	for _, group := range audience.AnyOf {
		rules := make([]string, 0, len(group.AllOf))
		for _, rule := range group.AllOf {
			sqlRule, ruleArgs, err := compileRewardAudienceRule(rule, firstPlaceholder+len(args))
			if err != nil {
				return "", nil, err
			}
			rules = append(rules, sqlRule)
			args = append(args, ruleArgs...)
		}
		if len(rules) > 0 {
			groups = append(groups, "("+strings.Join(rules, " AND ")+")")
		}
	}
	if len(groups) == 0 {
		return "TRUE", args, nil
	}
	_ = now
	return strings.Join(groups, " OR "), args, nil
}

func compileRewardAudienceRule(rule service.RewardAudienceRule, placeholder int) (string, []any, error) {
	fields := map[string]string{
		"registered_at": "u.created_at", "signup_source": "u.signup_source",
		"last_active_at": "u.last_active_at", "balance": "u.balance", "user_id": "u.id",
		"requests_7d": "COALESCE(b.requests_7d,0)", "requests_30d": "COALESCE(b.requests_30d,0)",
		"actual_cost_7d": "COALESCE(b.actual_cost_7d,0)", "actual_cost_30d": "COALESCE(b.actual_cost_30d,0)",
		"last_api_used_at": "b.last_api_used_at", "recharge_30d": "COALESCE(b.recharge_30d,0)",
		"total_recharged": "u.total_recharged",
	}
	if rule.Field == "subscription_group_id" {
		values, err := rewardRuleInt64Values(rule.Value)
		if err != nil {
			return "", nil, err
		}
		match := fmt.Sprintf(`EXISTS (SELECT 1 FROM user_subscriptions us WHERE us.user_id = u.id AND us.deleted_at IS NULL AND us.status = 'active' AND us.starts_at <= $1 AND us.expires_at > $1 AND us.group_id = ANY($%d::bigint[]))`, placeholder)
		if rule.Operator == "neq" || rule.Operator == "not_in" {
			match = "NOT (" + match + ")"
		} else if rule.Operator != "eq" && rule.Operator != "in" {
			return "", nil, fmt.Errorf("invalid subscription group operator")
		}
		return match, []any{pq.Array(values)}, nil
	}
	expr, ok := fields[rule.Field]
	if !ok {
		return "", nil, fmt.Errorf("unsupported audience field %q", rule.Field)
	}
	switch rule.Operator {
	case "eq", "neq", "gt", "gte", "lt", "lte", "before", "after":
		op := map[string]string{"eq": "=", "neq": "<>", "gt": ">", "gte": ">=", "lt": "<", "lte": "<=", "before": "<", "after": ">"}[rule.Operator]
		value, err := normalizeRewardRuleScalar(rule.Field, rule.Value)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("%s %s $%d", expr, op, placeholder), []any{value}, nil
	case "between":
		values := rewardRuleValues(rule.Value)
		if len(values) != 2 {
			return "", nil, fmt.Errorf("between requires two values")
		}
		low, err := normalizeRewardRuleScalar(rule.Field, values[0])
		if err != nil {
			return "", nil, err
		}
		high, err := normalizeRewardRuleScalar(rule.Field, values[1])
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("%s BETWEEN $%d AND $%d", expr, placeholder, placeholder+1), []any{low, high}, nil
	case "in", "not_in":
		values := rewardRuleValues(rule.Value)
		if len(values) == 0 {
			return "", nil, fmt.Errorf("in requires values")
		}
		negate := ""
		if rule.Operator == "not_in" {
			negate = "NOT "
		}
		if rule.Field == "signup_source" {
			stringsArray := make([]string, 0, len(values))
			for _, value := range values {
				stringsArray = append(stringsArray, fmt.Sprint(value))
			}
			return fmt.Sprintf("%s(%s = ANY($%d::text[]))", negate, expr, placeholder), []any{pq.Array(stringsArray)}, nil
		}
		if rule.Field == "user_id" {
			ids, err := rewardRuleInt64Values(rule.Value)
			if err != nil {
				return "", nil, err
			}
			return fmt.Sprintf("%s(%s = ANY($%d::bigint[]))", negate, expr, placeholder), []any{pq.Array(ids)}, nil
		}
		return "", nil, fmt.Errorf("in is unsupported for field %q", rule.Field)
	default:
		return "", nil, fmt.Errorf("unsupported audience operator %q", rule.Operator)
	}
}

func normalizeRewardRuleScalar(field string, value any) (any, error) {
	switch field {
	case "registered_at", "last_active_at", "last_api_used_at":
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("%s requires an RFC3339 time", field)
		}
		parsed, err := time.Parse(time.RFC3339, text)
		if err != nil {
			return nil, fmt.Errorf("%s requires an RFC3339 time", field)
		}
		return parsed, nil
	case "signup_source":
		return fmt.Sprint(value), nil
	case "user_id", "requests_7d", "requests_30d":
		parsed, err := strconv.ParseInt(fmt.Sprint(value), 10, 64)
		if err != nil {
			if number, ok := value.(float64); ok {
				return int64(number), nil
			}
			return nil, fmt.Errorf("%s requires an integer", field)
		}
		return parsed, nil
	default:
		parsed, err := strconv.ParseFloat(fmt.Sprint(value), 64)
		if err != nil {
			return nil, fmt.Errorf("%s requires a number", field)
		}
		return parsed, nil
	}
}

func rewardRuleValues(value any) []any {
	if values, ok := value.([]any); ok {
		return values
	}
	raw, _ := json.Marshal(value)
	var values []any
	_ = json.Unmarshal(raw, &values)
	return values
}

func rewardRuleInt64Values(value any) ([]int64, error) {
	values := rewardRuleValues(value)
	if len(values) == 0 {
		values = []any{value}
	}
	out := make([]int64, 0, len(values))
	for _, value := range values {
		normalized, err := normalizeRewardRuleScalar("user_id", value)
		if err != nil {
			return nil, err
		}
		out = append(out, normalized.(int64))
	}
	return out, nil
}

func (r *rewardRepository) CampaignStats(ctx context.Context, campaignID int64) (*service.RewardCampaignStats, error) {
	campaign, err := r.GetCampaign(ctx, campaignID)
	if err != nil {
		return nil, err
	}
	stats := &service.RewardCampaignStats{Amounts: map[string]int64{}}
	if err := r.db.QueryRowContext(ctx, `
SELECT COALESCE((SELECT SUM(evaluation_count) FROM reward_campaign_user_states WHERE campaign_id = $1), 0),
       COALESCE((SELECT SUM(win_count) FROM reward_campaign_user_states WHERE campaign_id = $1), 0),
       COALESCE((SELECT COUNT(*) FROM reward_campaign_user_states WHERE campaign_id = $1 AND control_group), 0),
       COUNT(*) FILTER (WHERE viewed_at IS NOT NULL),
       COUNT(*) FILTER (WHERE status = 'claimed'),
       COUNT(*) FILTER (WHERE status = 'expired'),
       COUNT(*) FILTER (WHERE status = 'pending')
FROM user_reward_grants
WHERE campaign_id = $1
`, campaignID).Scan(&stats.Evaluated, &stats.Won, &stats.ControlGroup, &stats.Viewed, &stats.Claimed, &stats.Expired, &stats.Pending); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT amount::text, COUNT(*) FROM user_reward_grants WHERE campaign_id = $1 GROUP BY amount ORDER BY amount`, campaignID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var amount string
		var count int64
		if err := rows.Scan(&amount, &count); err != nil {
			rows.Close()
			return nil, err
		}
		stats.Amounts[amount] = count
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	stats.Budget = service.RewardBudgetStats{
		Total: campaign.TotalBudget, Reserved: campaign.ReservedBudget, Spent: campaign.SpentBudget,
		Released: campaign.ReleasedBudget, Available: campaign.AvailableBudget(),
	}
	return stats, nil
}

func (r *rewardRepository) ListCampaignGrants(ctx context.Context, campaignID int64, filter service.RewardGrantListFilter) ([]service.RewardGrant, int64, error) {
	conditions := []string{"g.campaign_id = $1"}
	args := []any{campaignID}
	if filter.Status != "" {
		args = append(args, filter.Status)
		conditions = append(conditions, fmt.Sprintf("g.status = $%d", len(args)))
	}
	if filter.Search != "" {
		args = append(args, "%"+filter.Search+"%")
		placeholder := len(args)
		conditions = append(conditions, fmt.Sprintf(
			"(CAST(g.id AS TEXT) ILIKE $%d OR CAST(g.user_id AS TEXT) ILIKE $%d OR u.email ILIKE $%d)",
			placeholder, placeholder, placeholder,
		))
	}
	where := strings.Join(conditions, " AND ")

	var total int64
	countQuery := `
SELECT COUNT(*)
FROM user_reward_grants g
JOIN users u ON u.id = g.user_id
WHERE ` + where
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	listArgs := append([]any(nil), args...)
	listArgs = append(listArgs, filter.Limit, filter.Offset)
	limitPlaceholder := len(listArgs) - 1
	offsetPlaceholder := len(listArgs)
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
SELECT g.id, g.campaign_id, c.system_key, c.name, g.campaign_version_id, v.version_number, g.user_id,
       u.email, g.cycle_key, g.source, g.status, g.amount, g.priority,
       g.copy_snapshot, g.skin_snapshot, g.expires_at, g.viewed_at,
       g.claimed_at, g.balance_after, g.created_at, g.updated_at
FROM user_reward_grants g
JOIN reward_campaigns c ON c.id = g.campaign_id
JOIN reward_campaign_versions v ON v.id = g.campaign_version_id
JOIN users u ON u.id = g.user_id
WHERE %s
ORDER BY g.created_at DESC, g.id DESC
LIMIT $%d OFFSET $%d
`, where, limitPlaceholder, offsetPlaceholder), listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	grants, err := scanRewardGrantRowsWithUserEmail(rows)
	return grants, total, err
}

func (r *rewardRepository) ListCampaignJobs(ctx context.Context, campaignID int64, limit, offset int) ([]service.RewardCampaignJob, int64, error) {
	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM reward_campaign_jobs WHERE campaign_id = $1`, campaignID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT id, campaign_id, campaign_version_id, idempotency_key, status,
       scheduled_at, next_attempt_at, cursor_user_id, max_user_id, scanned_users, matched_users,
       granted_users, attempt_count, max_attempts, lease_owner, lease_expires_at,
       last_error, finished_at, created_at, updated_at
FROM reward_campaign_jobs
WHERE campaign_id = $1
ORDER BY created_at DESC, id DESC
LIMIT $2 OFFSET $3
`, campaignID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	jobs := make([]service.RewardCampaignJob, 0)
	for rows.Next() {
		var job service.RewardCampaignJob
		var owner sql.NullString
		var lease, finished sql.NullTime
		if err := rows.Scan(
			&job.ID, &job.CampaignID, &job.VersionID, &job.IdempotencyKey, &job.Status,
			&job.ScheduledAt, &job.AvailableAt, &job.CursorUserID, &job.MaxUserID, &job.ProcessedCount,
			&job.EligibleCount, &job.GrantedCount, &job.RetryCount, &job.MaxRetries,
			&owner, &lease, &job.LastError, &finished, &job.CreatedAt, &job.UpdatedAt,
		); err != nil {
			return nil, 0, err
		}
		if owner.Valid {
			job.LockedBy = &owner.String
		}
		if lease.Valid {
			job.LockedUntil = &lease.Time
		}
		if finished.Valid {
			job.FinishedAt = &finished.Time
		}
		jobs = append(jobs, job)
	}
	return jobs, total, rows.Err()
}

func (r *rewardRepository) CreateSkin(ctx context.Context, skin service.RewardSkin, content []byte, actorID *int64) (*service.RewardSkin, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
INSERT INTO reward_skins (
    name, description, alt_text, status, mime_type, width, height, byte_size,
    sha256, content, created_by, updated_by, created_at, updated_at
) VALUES ($1, $2, $3, 'active', $4, $5, $6, $7, $8, $9, $10, $10, NOW(), NOW())
RETURNING id
`, skin.Name, skin.Description, skin.AltText, skin.MIMEType, skin.Width, skin.Height,
		len(content), skin.SHA256, content, actorID).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "reward_skins_sha256") {
			return nil, infraerrors.Conflict("REWARD_SKIN_DUPLICATE", "this skin image already exists")
		}
		return nil, err
	}
	skins, err := r.listSkinsByCondition(ctx, "id = $1", id)
	if err != nil || len(skins) == 0 {
		return nil, err
	}
	return &skins[0], nil
}

func (r *rewardRepository) ListSkins(ctx context.Context, includeArchived bool) ([]service.RewardSkin, error) {
	if includeArchived {
		return r.listSkinsByCondition(ctx, "TRUE")
	}
	return r.listSkinsByCondition(ctx, "status <> 'archived'")
}

func (r *rewardRepository) listSkinsByCondition(ctx context.Context, condition string, args ...any) ([]service.RewardSkin, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, name, description, alt_text, status, mime_type, width, height, byte_size, sha256,
       created_by, updated_by, archived_at, created_at, updated_at
FROM reward_skins
WHERE `+condition+`
ORDER BY created_at DESC, id DESC
`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	skins := make([]service.RewardSkin, 0)
	for rows.Next() {
		var skin service.RewardSkin
		var createdBy, updatedBy sql.NullInt64
		var archivedAt sql.NullTime
		if err := rows.Scan(
			&skin.ID, &skin.Name, &skin.Description, &skin.AltText, &skin.Status, &skin.MIMEType,
			&skin.Width, &skin.Height, &skin.SizeBytes, &skin.SHA256,
			&createdBy, &updatedBy, &archivedAt, &skin.CreatedAt, &skin.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if createdBy.Valid {
			skin.CreatedBy = &createdBy.Int64
		}
		if updatedBy.Valid {
			skin.UpdatedBy = &updatedBy.Int64
		}
		if archivedAt.Valid {
			skin.ArchivedAt = &archivedAt.Time
		}
		skin.ImageURL = fmt.Sprintf("/api/v1/reward-skins/%d/content?v=%s", skin.ID, skin.SHA256)
		skins = append(skins, skin)
	}
	return skins, rows.Err()
}

func (r *rewardRepository) UpdateSkin(ctx context.Context, skinID int64, name, description, altText, status *string, actorID *int64) (*service.RewardSkin, error) {
	result, err := r.db.ExecContext(ctx, `
UPDATE reward_skins
SET name = COALESCE($2, name),
    description = COALESCE($3, description),
    alt_text = COALESCE($4, alt_text),
    status = COALESCE($5, status),
    archived_at = CASE WHEN $5 = 'archived' THEN NOW() WHEN $5 IS NOT NULL THEN NULL ELSE archived_at END,
    updated_by = $6,
    updated_at = NOW()
WHERE id = $1
`, skinID, name, description, altText, status, actorID)
	if err != nil {
		return nil, err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return nil, infraerrors.NotFound("REWARD_SKIN_NOT_FOUND", "reward skin not found")
	}
	skins, err := r.listSkinsByCondition(ctx, "id = $1", skinID)
	if err != nil || len(skins) == 0 {
		return nil, err
	}
	return &skins[0], nil
}

func (r *rewardRepository) GetSkinContent(ctx context.Context, skinID int64) (string, string, []byte, error) {
	var mimeType, hash string
	var content []byte
	err := r.db.QueryRowContext(ctx, `SELECT mime_type, sha256, content FROM reward_skins WHERE id = $1`, skinID).Scan(&mimeType, &hash, &content)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil, infraerrors.NotFound("REWARD_SKIN_NOT_FOUND", "reward skin not found")
	}
	return mimeType, hash, content, err
}

func marshalRewardConfig(config service.RewardCampaignConfig) ([]byte, string, error) {
	raw, err := json.Marshal(config)
	if err != nil {
		return nil, "", err
	}
	hash := sha256.Sum256(raw)
	return raw, hex.EncodeToString(hash[:]), nil
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
}
