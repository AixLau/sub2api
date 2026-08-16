package repository

import (
	"context"
	cryptorand "crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type rewardRepository struct {
	db *sql.DB
}

func NewRewardRepository(db *sql.DB) service.RewardRepository {
	return &rewardRepository{db: db}
}

func (r *rewardRepository) ListRuntimeCampaigns(ctx context.Context, issuanceMode string, now time.Time) ([]service.RewardCampaign, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT c.id, c.system_key, c.name, c.description, c.status, c.issuance_mode, c.timezone,
       c.starts_at, c.ends_at, c.priority, c.total_budget, c.reserved_budget,
       c.spent_budget, c.released_budget, c.current_version_id,
       c.created_by, c.updated_by, c.created_at, c.updated_at,
       v.version_number, v.config
FROM reward_campaigns c
JOIN reward_campaign_versions v ON v.id = c.current_version_id
WHERE c.issuance_mode = $1
  AND c.status IN ('active', 'scheduled')
  AND (c.starts_at IS NULL OR c.starts_at <= $2)
  AND (c.ends_at IS NULL OR c.ends_at > $2)
ORDER BY c.priority DESC, c.ends_at ASC NULLS LAST, c.id ASC
`, issuanceMode, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRewardCampaignRows(rows)
}

func (r *rewardRepository) GetAudienceProfile(ctx context.Context, userID int64, now time.Time) (*service.RewardAudienceProfile, error) {
	profile := &service.RewardAudienceProfile{}
	err := r.db.QueryRowContext(ctx, `
SELECT id, email, role, status, created_at, signup_source, last_active_at, balance, total_recharged
FROM users
WHERE id = $1 AND deleted_at IS NULL
`, userID).Scan(
		&profile.UserID,
		&profile.Email,
		&profile.Role,
		&profile.Status,
		&profile.RegisteredAt,
		&profile.SignupSource,
		&profile.LastActiveAt,
		&profile.Balance,
		&profile.TotalRecharged,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, infraerrors.NotFound("USER_NOT_FOUND", "user not found")
	}
	if err != nil {
		return nil, err
	}

	if err := r.db.QueryRowContext(ctx, `
SELECT COALESCE(SUM(request_count) FILTER (WHERE bucket_start >= $2::timestamptz - INTERVAL '7 days'), 0),
       COALESCE(SUM(request_count) FILTER (WHERE bucket_start >= $2::timestamptz - INTERVAL '30 days'), 0),
       COALESCE(SUM(actual_cost) FILTER (WHERE bucket_start >= $2::timestamptz - INTERVAL '7 days'), 0),
       COALESCE(SUM(actual_cost) FILTER (WHERE bucket_start >= $2::timestamptz - INTERVAL '30 days'), 0),
       MAX(last_api_use_at),
       COALESCE(SUM(recharge_amount) FILTER (WHERE bucket_start >= $2::timestamptz - INTERVAL '30 days'), 0)
FROM user_behavior_daily
WHERE user_id = $1
`, userID, now).Scan(
		&profile.Requests7D,
		&profile.Requests30D,
		&profile.ActualCost7D,
		&profile.ActualCost30D,
		&profile.LastAPIUsedAt,
		&profile.Recharge30D,
	); err != nil {
		return nil, err
	}

	rows, err := r.db.QueryContext(ctx, `
SELECT group_id
FROM user_subscriptions
WHERE user_id = $1
  AND status = 'active'
  AND deleted_at IS NULL
  AND starts_at <= $2
  AND expires_at > $2
ORDER BY group_id
`, userID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var groupID int64
		if err := rows.Scan(&groupID); err != nil {
			return nil, err
		}
		profile.SubscriptionGroupIDs = append(profile.SubscriptionGroupIDs, groupID)
	}
	return profile, rows.Err()
}

func (r *rewardRepository) GetAudienceProfiles(ctx context.Context, userIDs []int64, now time.Time) (map[int64]service.RewardAudienceProfile, error) {
	profiles := make(map[int64]service.RewardAudienceProfile, len(userIDs))
	if len(userIDs) == 0 {
		return profiles, nil
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT id, email, role, status, created_at, signup_source, last_active_at, balance, total_recharged
FROM users
WHERE id = ANY($1::bigint[]) AND role = 'user' AND status = 'active' AND deleted_at IS NULL
`, pq.Array(userIDs))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var profile service.RewardAudienceProfile
		var lastActive sql.NullTime
		if err := rows.Scan(&profile.UserID, &profile.Email, &profile.Role, &profile.Status, &profile.RegisteredAt, &profile.SignupSource, &lastActive, &profile.Balance, &profile.TotalRecharged); err != nil {
			rows.Close()
			return nil, err
		}
		if lastActive.Valid {
			value := lastActive.Time
			profile.LastActiveAt = &value
		}
		profiles[profile.UserID] = profile
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	rows, err = r.db.QueryContext(ctx, `
SELECT user_id,
       COALESCE(SUM(request_count) FILTER (WHERE bucket_start >= $2::timestamptz - INTERVAL '7 days'), 0),
       COALESCE(SUM(request_count) FILTER (WHERE bucket_start >= $2::timestamptz - INTERVAL '30 days'), 0),
       COALESCE(SUM(actual_cost) FILTER (WHERE bucket_start >= $2::timestamptz - INTERVAL '7 days'), 0),
       COALESCE(SUM(actual_cost) FILTER (WHERE bucket_start >= $2::timestamptz - INTERVAL '30 days'), 0),
       MAX(last_api_use_at),
       COALESCE(SUM(recharge_amount) FILTER (WHERE bucket_start >= $2::timestamptz - INTERVAL '30 days'), 0)
FROM user_behavior_daily
WHERE user_id = ANY($1::bigint[])
GROUP BY user_id
`, pq.Array(userIDs), now)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var userID int64
		var lastAPI sql.NullTime
		var requests7D, requests30D int64
		var cost7D, cost30D, recharge30D float64
		if err := rows.Scan(&userID, &requests7D, &requests30D, &cost7D, &cost30D, &lastAPI, &recharge30D); err != nil {
			rows.Close()
			return nil, err
		}
		profile, ok := profiles[userID]
		if !ok {
			continue
		}
		profile.Requests7D = requests7D
		profile.Requests30D = requests30D
		profile.ActualCost7D = cost7D
		profile.ActualCost30D = cost30D
		profile.Recharge30D = recharge30D
		if lastAPI.Valid {
			value := lastAPI.Time
			profile.LastAPIUsedAt = &value
		}
		profiles[userID] = profile
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	rows, err = r.db.QueryContext(ctx, `
SELECT user_id, group_id
FROM user_subscriptions
WHERE user_id = ANY($1::bigint[]) AND status = 'active' AND deleted_at IS NULL
  AND starts_at <= $2 AND expires_at > $2
ORDER BY user_id, group_id
`, pq.Array(userIDs), now)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var userID, groupID int64
		if err := rows.Scan(&userID, &groupID); err != nil {
			rows.Close()
			return nil, err
		}
		profile, ok := profiles[userID]
		if !ok {
			continue
		}
		profile.SubscriptionGroupIDs = append(profile.SubscriptionGroupIDs, groupID)
		profiles[userID] = profile
	}
	return profiles, rows.Close()
}

func (r *rewardRepository) EvaluateAndMaybeGrant(
	ctx context.Context,
	campaign service.RewardCampaign,
	userID int64,
	now time.Time,
	shouldAward, controlGroup bool,
	source string,
	jobID *int64,
) (*service.RewardGrant, bool, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()

	locked, err := getLockedRewardCampaignVersion(ctx, tx, campaign.ID, campaign.CurrentVersionID)
	if err != nil {
		return nil, false, err
	}
	if locked.Status != service.RewardCampaignStatusActive && locked.Status != service.RewardCampaignStatusScheduled {
		return nil, false, tx.Commit()
	}
	if (!locked.StartsAt.IsZero() && now.Before(locked.StartsAt)) || (!locked.EndsAt.IsZero() && !now.Before(locked.EndsAt)) {
		return nil, false, tx.Commit()
	}

	if _, err := tx.ExecContext(ctx, `
INSERT INTO reward_campaign_user_states (campaign_id, user_id, created_at, updated_at)
VALUES ($1, $2, NOW(), NOW())
ON CONFLICT (campaign_id, user_id) DO NOTHING
`, locked.ID, userID); err != nil {
		return nil, false, err
	}

	var lastEvaluated, lastWon sql.NullTime
	var evaluationCount, grantCount int64
	var currentCycleKey string
	if err := tx.QueryRowContext(ctx, `
SELECT last_evaluated_at, last_won_at, evaluation_count, grant_count, current_cycle_key
FROM reward_campaign_user_states
WHERE campaign_id = $1 AND user_id = $2
FOR UPDATE
`, locked.ID, userID).Scan(&lastEvaluated, &lastWon, &evaluationCount, &grantCount, &currentCycleKey); err != nil {
		return nil, false, err
	}
	cycleKey := ""
	if jobID != nil {
		cycleKey = fmt.Sprintf("job:%d", *jobID)
		if currentCycleKey == cycleKey {
			var existingGrantID int64
			err := tx.QueryRowContext(ctx, `
SELECT id
FROM user_reward_grants
WHERE campaign_id = $1 AND user_id = $2 AND cycle_key = $3
`, locked.ID, userID, cycleKey).Scan(&existingGrantID)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return nil, false, err
			}
			if err := tx.Commit(); err != nil {
				return nil, false, err
			}
			if existingGrantID > 0 {
				return &service.RewardGrant{ID: existingGrantID, CampaignID: locked.ID, UserID: userID}, false, nil
			}
			return nil, false, nil
		}
	}

	if grantCount >= int64(locked.PerUserLimit) {
		return nil, false, tx.Commit()
	}
	if lastEvaluated.Valid && locked.EvaluationIntervalMinutes > 0 && now.Before(lastEvaluated.Time.Add(time.Duration(locked.EvaluationIntervalMinutes)*time.Minute)) {
		return nil, false, tx.Commit()
	}
	if lastWon.Valid && locked.ClaimCooldownMinutes > 0 && now.Before(lastWon.Time.Add(time.Duration(locked.ClaimCooldownMinutes)*time.Minute)) {
		shouldAward = false
	}

	evaluationCount++
	if jobID == nil {
		cycleKey = fmt.Sprintf("eval:%d", evaluationCount)
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE reward_campaign_user_states
SET last_evaluated_at = $3,
    evaluation_count = $4,
    control_group = $5,
    current_cycle_key = $6,
    updated_at = NOW()
WHERE campaign_id = $1 AND user_id = $2
`, locked.ID, userID, now, evaluationCount, controlGroup, cycleKey); err != nil {
		return nil, false, err
	}
	if !shouldAward || controlGroup {
		return nil, false, tx.Commit()
	}

	available := locked.TotalBudget - locked.ReservedBudget - locked.SpentBudget
	tier, ok := chooseAffordableRewardTier(locked.Config.AmountTiers, available)
	if !ok {
		return nil, false, tx.Commit()
	}
	skin, err := chooseRewardSkinSnapshot(ctx, tx, locked.Config.SkinWeights)
	if err != nil {
		return nil, false, err
	}
	copyRaw, err := marshalRewardCopySnapshot(locked.Config)
	if err != nil {
		return nil, false, err
	}
	skinRaw, err := json.Marshal(skin)
	if err != nil {
		return nil, false, err
	}

	result, err := tx.ExecContext(ctx, `
UPDATE reward_campaigns
SET reserved_budget = reserved_budget + $2, updated_at = NOW()
WHERE id = $1
  AND total_budget - reserved_budget - spent_budget >= $2
`, locked.ID, tier.Amount)
	if err != nil {
		return nil, false, err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	if updated == 0 {
		return nil, false, tx.Commit()
	}

	var grant service.RewardGrant
	var copySnapshot, skinSnapshot []byte
	var expiresAt sql.NullTime
	err = tx.QueryRowContext(ctx, `
INSERT INTO user_reward_grants (
    campaign_id, campaign_version_id, user_id, skin_id, job_id, cycle_key,
    source, status, amount, priority, copy_snapshot, skin_snapshot, metadata,
    expires_at, created_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, 'pending', $8, $9,
    $10::jsonb, $11::jsonb, '{}'::jsonb, $12, NOW(), NOW()
)
ON CONFLICT (campaign_id, user_id, cycle_key) DO NOTHING
RETURNING id, campaign_id, campaign_version_id, user_id, cycle_key, source, status,
          amount, priority, copy_snapshot, skin_snapshot, expires_at, created_at, updated_at
`, locked.ID, locked.CurrentVersionID, userID, nullablePositiveInt64(skin.ID), jobID, cycleKey,
		source, tier.Amount, locked.Priority, copyRaw, skinRaw, nullableTime(locked.EndsAt)).Scan(
		&grant.ID, &grant.CampaignID, &grant.VersionID, &grant.UserID, &grant.CycleKey,
		&grant.Source, &grant.Status, &grant.Amount, &grant.Priority, &copySnapshot,
		&skinSnapshot, &expiresAt, &grant.CreatedAt, &grant.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		releaseResult, releaseErr := tx.ExecContext(ctx, `
UPDATE reward_campaigns
SET reserved_budget = reserved_budget - $2, updated_at = NOW()
WHERE id = $1 AND reserved_budget >= $2
`, locked.ID, tier.Amount)
		if releaseErr != nil {
			return nil, false, releaseErr
		}
		released, releaseErr := releaseResult.RowsAffected()
		if releaseErr != nil || released != 1 {
			return nil, false, fmt.Errorf("reward campaign %d reservation release mismatch", locked.ID)
		}
		return nil, false, tx.Commit()
	}
	if err != nil {
		return nil, false, err
	}
	grant.CampaignTitle = locked.Title
	grant.CampaignKey = locked.CampaignKey
	grant.Version = locked.CurrentVersion
	if expiresAt.Valid {
		grant.ExpiresAt = expiresAt.Time
	}
	if err := unmarshalRewardCopySnapshot(copySnapshot, &grant); err != nil {
		return nil, false, err
	}
	if err := json.Unmarshal(skinSnapshot, &grant.Skin); err != nil {
		return nil, false, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE reward_campaign_user_states
SET last_won_at = $3::timestamptz,
    last_granted_at = $3::timestamptz,
    next_eligible_at = CASE
        WHEN $4::double precision > 0
        THEN $3::timestamptz + ($4::double precision * INTERVAL '1 minute')
        ELSE NULL
    END,
    win_count = win_count + 1,
    grant_count = grant_count + 1,
    updated_at = NOW()
WHERE campaign_id = $1 AND user_id = $2
`, locked.ID, userID, now, locked.ClaimCooldownMinutes); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return &grant, true, nil
}

func (r *rewardRepository) ListPending(ctx context.Context, userID int64, now time.Time) ([]service.RewardGrant, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT g.id, g.campaign_id, c.system_key, c.name, g.campaign_version_id, v.version_number, g.user_id,
       g.cycle_key, g.source, g.status, g.amount, g.priority,
       g.copy_snapshot, g.skin_snapshot, g.expires_at, g.viewed_at,
       g.claimed_at, g.balance_after, g.created_at, g.updated_at
FROM user_reward_grants g
JOIN reward_campaigns c ON c.id = g.campaign_id
JOIN reward_campaign_versions v ON v.id = g.campaign_version_id
WHERE g.user_id = $1
  AND g.status = 'pending'
  AND (g.expires_at IS NULL OR g.expires_at > $2)
ORDER BY g.priority DESC, g.expires_at ASC NULLS LAST, g.created_at ASC, g.id ASC
`, userID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRewardGrantRows(rows)
}

func (r *rewardRepository) MarkViewed(ctx context.Context, userID, grantID int64, now time.Time) (*service.RewardGrant, error) {
	result, err := r.db.ExecContext(ctx, `
UPDATE user_reward_grants
SET viewed_at = COALESCE(viewed_at, $3), updated_at = NOW()
WHERE id = $1 AND user_id = $2
  AND (
    status = 'claimed'
    OR (status = 'pending' AND (expires_at IS NULL OR expires_at > $3))
  )
`, grantID, userID, now)
	if err != nil {
		return nil, err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return nil, infraerrors.Conflict("REWARD_UNAVAILABLE", "reward is unavailable")
	}
	return r.getGrant(ctx, userID, grantID)
}

func (r *rewardRepository) FindPendingBySystemKey(ctx context.Context, userID int64, systemKey string, now time.Time) (*service.RewardGrant, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT g.id, g.campaign_id, c.system_key, c.name, g.campaign_version_id, v.version_number, g.user_id,
       g.cycle_key, g.source, g.status, g.amount, g.priority,
       g.copy_snapshot, g.skin_snapshot, g.expires_at, g.viewed_at,
       g.claimed_at, g.balance_after, g.created_at, g.updated_at
FROM user_reward_grants g
JOIN reward_campaigns c ON c.id = g.campaign_id
JOIN reward_campaign_versions v ON v.id = g.campaign_version_id
WHERE g.user_id = $1 AND c.system_key = $2 AND g.status = 'pending'
  AND (g.expires_at IS NULL OR g.expires_at > $3)
ORDER BY g.priority DESC, g.created_at ASC
LIMIT 1
`, userID, systemKey, now)
	grant, err := scanRewardGrant(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, infraerrors.NotFound("REWARD_NOT_FOUND", "reward not found")
	}
	return grant, err
}

func (r *rewardRepository) FindLatestBySystemKey(ctx context.Context, userID int64, systemKey string) (*service.RewardGrant, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT g.id, g.campaign_id, c.system_key, c.name, g.campaign_version_id, v.version_number, g.user_id,
       g.cycle_key, g.source, g.status, g.amount, g.priority,
       g.copy_snapshot, g.skin_snapshot, g.expires_at, g.viewed_at,
       g.claimed_at, g.balance_after, g.created_at, g.updated_at
FROM user_reward_grants g
JOIN reward_campaigns c ON c.id = g.campaign_id
JOIN reward_campaign_versions v ON v.id = g.campaign_version_id
WHERE g.user_id = $1 AND c.system_key = $2 AND g.status IN ('pending', 'claimed')
ORDER BY (g.status = 'pending') DESC, g.created_at DESC, g.id DESC
LIMIT 1
`, userID, systemKey)
	grant, err := scanRewardGrant(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, infraerrors.NotFound("REWARD_NOT_FOUND", "reward not found")
	}
	return grant, err
}

func (r *rewardRepository) getGrant(ctx context.Context, userID, grantID int64) (*service.RewardGrant, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT g.id, g.campaign_id, c.system_key, c.name, g.campaign_version_id, v.version_number, g.user_id,
       g.cycle_key, g.source, g.status, g.amount, g.priority,
       g.copy_snapshot, g.skin_snapshot, g.expires_at, g.viewed_at,
       g.claimed_at, g.balance_after, g.created_at, g.updated_at
FROM user_reward_grants g
JOIN reward_campaigns c ON c.id = g.campaign_id
JOIN reward_campaign_versions v ON v.id = g.campaign_version_id
WHERE g.id = $1 AND g.user_id = $2
`, grantID, userID)
	grant, err := scanRewardGrant(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, infraerrors.NotFound("REWARD_NOT_FOUND", "reward not found")
	}
	return grant, err
}

type rewardScanner interface {
	Scan(dest ...any) error
}

func scanRewardGrant(scanner rewardScanner) (*service.RewardGrant, error) {
	return scanRewardGrantRecord(scanner, false)
}

func scanRewardGrantRecord(scanner rewardScanner, includeUserEmail bool) (*service.RewardGrant, error) {
	var grant service.RewardGrant
	var systemKey sql.NullString
	var copyRaw, skinRaw []byte
	var expiresAt, viewedAt, claimedAt sql.NullTime
	var balanceAfter sql.NullFloat64
	destinations := []any{
		&grant.ID, &grant.CampaignID, &systemKey, &grant.CampaignTitle, &grant.VersionID, &grant.Version,
		&grant.UserID,
	}
	if includeUserEmail {
		destinations = append(destinations, &grant.UserEmail)
	}
	destinations = append(destinations,
		&grant.CycleKey, &grant.Source, &grant.Status, &grant.Amount,
		&grant.Priority, &copyRaw, &skinRaw, &expiresAt, &viewedAt, &claimedAt,
		&balanceAfter, &grant.CreatedAt, &grant.UpdatedAt,
	)
	err := scanner.Scan(destinations...)
	if err != nil {
		return nil, err
	}
	grant.CampaignKey = systemKey.String
	if expiresAt.Valid {
		grant.ExpiresAt = expiresAt.Time
	}
	if viewedAt.Valid {
		grant.ViewedAt = &viewedAt.Time
	}
	if claimedAt.Valid {
		grant.ClaimedAt = &claimedAt.Time
	}
	if balanceAfter.Valid {
		grant.BalanceAfter = &balanceAfter.Float64
	}
	if err := unmarshalRewardCopySnapshot(copyRaw, &grant); err != nil {
		return nil, err
	}
	if len(skinRaw) > 0 {
		if err := json.Unmarshal(skinRaw, &grant.Skin); err != nil {
			return nil, err
		}
	}
	return &grant, nil
}

func scanRewardGrantRows(rows *sql.Rows) ([]service.RewardGrant, error) {
	return scanRewardGrantRowsRecord(rows, false)
}

func scanRewardGrantRowsWithUserEmail(rows *sql.Rows) ([]service.RewardGrant, error) {
	return scanRewardGrantRowsRecord(rows, true)
}

func scanRewardGrantRowsRecord(rows *sql.Rows, includeUserEmail bool) ([]service.RewardGrant, error) {
	grants := make([]service.RewardGrant, 0)
	for rows.Next() {
		grant, err := scanRewardGrantRecord(rows, includeUserEmail)
		if err != nil {
			return nil, err
		}
		grants = append(grants, *grant)
	}
	return grants, rows.Err()
}

type storedRewardCopySnapshot struct {
	Default service.RewardCopy            `json:"default"`
	I18n    map[string]service.RewardCopy `json:"i18n,omitempty"`
}

func marshalRewardCopySnapshot(config service.RewardCampaignConfig) ([]byte, error) {
	return json.Marshal(storedRewardCopySnapshot{Default: config.Copy, I18n: config.CopyI18n})
}

func unmarshalRewardCopySnapshot(raw []byte, grant *service.RewardGrant) error {
	var snapshot storedRewardCopySnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return err
	}
	if snapshot.Default.Title != "" || snapshot.I18n != nil {
		grant.Copy = snapshot.Default
		grant.CopyI18n = snapshot.I18n
		return nil
	}
	// Migration 193 stored the legacy system campaign copy directly.
	return json.Unmarshal(raw, &grant.Copy)
}

func getLockedRewardCampaign(ctx context.Context, tx *sql.Tx, id int64) (*service.RewardCampaign, error) {
	return getLockedRewardCampaignVersion(ctx, tx, id, 0)
}

func getLockedRewardCampaignVersion(ctx context.Context, tx *sql.Tx, id, versionID int64) (*service.RewardCampaign, error) {
	row := tx.QueryRowContext(ctx, `
SELECT c.id, c.system_key, c.name, c.description, c.status, c.issuance_mode, c.timezone,
       c.starts_at, c.ends_at, c.priority, c.total_budget, c.reserved_budget,
       c.spent_budget, c.released_budget, c.current_version_id,
       c.created_by, c.updated_by, c.created_at, c.updated_at,
       v.version_number, v.config
FROM reward_campaigns c
JOIN reward_campaign_versions v ON v.id = CASE WHEN $2 > 0 THEN $2 ELSE c.current_version_id END
WHERE c.id = $1 AND v.campaign_id = c.id
FOR UPDATE OF c
`, id, versionID)
	campaign, err := scanRewardCampaign(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, infraerrors.NotFound("REWARD_CAMPAIGN_NOT_FOUND", "reward campaign not found")
	}
	if campaign != nil && versionID > 0 {
		campaign.CurrentVersionID = versionID
	}
	return campaign, err
}

func scanRewardCampaign(scanner rewardScanner) (*service.RewardCampaign, error) {
	var campaign service.RewardCampaign
	var systemKey sql.NullString
	var startsAt, endsAt sql.NullTime
	var currentVersionID sql.NullInt64
	var createdBy, updatedBy sql.NullInt64
	var configRaw []byte
	err := scanner.Scan(
		&campaign.ID, &systemKey, &campaign.Name, &campaign.Description,
		&campaign.Status, &campaign.IssuanceMode, &campaign.Timezone,
		&startsAt, &endsAt, &campaign.Priority, &campaign.TotalBudget,
		&campaign.ReservedBudget, &campaign.SpentBudget, &campaign.ReleasedBudget,
		&currentVersionID, &createdBy, &updatedBy, &campaign.CreatedAt,
		&campaign.UpdatedAt, &campaign.CurrentVersion, &configRaw,
	)
	if err != nil {
		return nil, err
	}
	campaign.CampaignKey = systemKey.String
	campaign.System = systemKey.Valid
	campaign.CurrentVersionID = currentVersionID.Int64
	if startsAt.Valid {
		campaign.StartsAt = startsAt.Time
	}
	if endsAt.Valid {
		campaign.EndsAt = endsAt.Time
	}
	if createdBy.Valid {
		campaign.CreatedBy = &createdBy.Int64
	}
	if updatedBy.Valid {
		campaign.UpdatedBy = &updatedBy.Int64
	}
	if err := json.Unmarshal(configRaw, &campaign.Config); err != nil {
		return nil, err
	}
	applyRewardConfig(&campaign)
	return &campaign, nil
}

func scanRewardCampaignRows(rows *sql.Rows) ([]service.RewardCampaign, error) {
	campaigns := make([]service.RewardCampaign, 0)
	for rows.Next() {
		campaign, err := scanRewardCampaign(rows)
		if err != nil {
			return nil, err
		}
		campaigns = append(campaigns, *campaign)
	}
	return campaigns, rows.Err()
}

func applyRewardConfig(campaign *service.RewardCampaign) {
	campaign.Title = campaign.Config.Title
	if campaign.Title == "" {
		campaign.Title = campaign.Config.Copy.Title
	}
	if campaign.Config.Priority != 0 || campaign.Priority == 0 {
		campaign.Priority = campaign.Config.Priority
	}
	campaign.WinProbability = campaign.Config.WinProbability
	campaign.PerUserLimit = campaign.Config.PerUserLimit
	campaign.EvaluationIntervalMinutes = campaign.Config.EvaluationIntervalMinutes
	campaign.ClaimCooldownMinutes = campaign.Config.ClaimCooldownMinutes
	campaign.ControlGroupPercent = campaign.Config.ControlGroupPercent
	if campaign.PerUserLimit <= 0 {
		campaign.PerUserLimit = 1
	}
}

func chooseAffordableRewardTier(tiers []service.RewardAmountTier, available float64) (service.RewardAmountTier, bool) {
	affordable := make([]service.RewardAmountTier, 0, len(tiers))
	for _, tier := range tiers {
		if tier.Amount > 0 && tier.Weight > 0 && tier.Amount <= available+1e-8 {
			affordable = append(affordable, tier)
		}
	}
	if len(affordable) == 0 {
		return service.RewardAmountTier{}, false
	}
	index, ok := secureWeightedIndex(affordable, func(tier service.RewardAmountTier) int { return tier.Weight })
	if !ok {
		return service.RewardAmountTier{}, false
	}
	return affordable[index], true
}

func chooseRewardSkinSnapshot(ctx context.Context, tx *sql.Tx, options []service.RewardSkinWeight) (service.RewardSkinSnapshot, error) {
	if len(options) == 0 {
		return service.RewardSkinSnapshot{}, nil
	}
	available := make([]service.RewardSkinWeight, 0, len(options))
	for _, option := range options {
		if option.SkinID > 0 && option.Weight > 0 {
			available = append(available, option)
		}
	}
	for len(available) > 0 {
		index, ok := secureWeightedIndex(available, func(option service.RewardSkinWeight) int { return option.Weight })
		if !ok {
			break
		}
		option := available[index]
		var snapshot service.RewardSkinSnapshot
		var altText string
		err := tx.QueryRowContext(ctx, `
SELECT id, name, description, alt_text, sha256
FROM reward_skins
WHERE id = $1 AND status = 'active'
`, option.SkinID).Scan(&snapshot.ID, &snapshot.Name, &snapshot.Description, &altText, &snapshot.SHA256)
		if err == nil {
			if altText != "" {
				snapshot.Description = altText
			}
			snapshot.ImageURL = fmt.Sprintf("/api/v1/reward-skins/%d/content?v=%s", snapshot.ID, snapshot.SHA256)
			return snapshot, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return service.RewardSkinSnapshot{}, err
		}
		available = append(available[:index], available[index+1:]...)
	}
	return service.RewardSkinSnapshot{}, nil
}

func secureWeightedIndex[T any](items []T, weight func(T) int) (int, bool) {
	var total uint64
	for _, item := range items {
		w := weight(item)
		if w <= 0 {
			continue
		}
		if math.MaxUint64-total < uint64(w) {
			return 0, false
		}
		total += uint64(w)
	}
	if total == 0 {
		return 0, false
	}
	var raw [8]byte
	if _, err := cryptorand.Read(raw[:]); err != nil {
		return 0, false
	}
	draw := binary.BigEndian.Uint64(raw[:])
	limit := math.MaxUint64 - (math.MaxUint64 % total)
	for draw >= limit {
		if _, err := cryptorand.Read(raw[:]); err != nil {
			return 0, false
		}
		draw = binary.BigEndian.Uint64(raw[:])
	}
	draw %= total
	var cursor uint64
	for index, item := range items {
		w := weight(item)
		if w <= 0 {
			continue
		}
		cursor += uint64(w)
		if draw < cursor {
			return index, true
		}
	}
	return 0, false
}

func nullablePositiveInt64(value int64) any {
	if value <= 0 {
		return nil
	}
	return value
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func sortedUniqueInt64(values []int64) []int64 {
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	out := values[:0]
	for _, value := range values {
		if len(out) == 0 || out[len(out)-1] != value {
			out = append(out, value)
		}
	}
	return out
}
