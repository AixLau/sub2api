package repository

import (
	"context"
	cryptorand "crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *rewardRepository) ImportLegacyPending(ctx context.Context, userID int64, now time.Time) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var welcomeAmount, surpriseAmount float64
	if err := tx.QueryRowContext(ctx, `
SELECT welcome_reward_amount, surprise_reward_amount
FROM users
WHERE id = $1 AND deleted_at IS NULL
FOR UPDATE
`, userID).Scan(&welcomeAmount, &surpriseAmount); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return infraerrors.NotFound("USER_NOT_FOUND", "user not found")
		}
		return err
	}
	if welcomeAmount > 0 {
		if err := importLegacyReward(ctx, tx, userID, service.RewardSystemCampaignWelcome, service.RewardGrantSourceLegacyWelcome, "legacy:welcome", welcomeAmount, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE users SET welcome_reward_amount = 0, updated_at = NOW() WHERE id = $1`, userID); err != nil {
			return err
		}
	}
	if surpriseAmount > 0 {
		if err := importLegacyReward(ctx, tx, userID, service.RewardSystemCampaignSurprise, service.RewardGrantSourceLegacySurprise, "legacy:surprise", surpriseAmount, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE users SET surprise_reward_amount = 0, updated_at = NOW() WHERE id = $1`, userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func importLegacyReward(
	ctx context.Context,
	tx *sql.Tx,
	userID int64,
	systemKey, source, cycleKey string,
	amount float64,
	now time.Time,
) error {
	var campaignID, versionID int64
	var priority int
	var endsAt sql.NullTime
	var configRaw []byte
	if err := tx.QueryRowContext(ctx, `
SELECT c.id, c.current_version_id, c.priority, c.ends_at, v.config
FROM reward_campaigns c
JOIN reward_campaign_versions v ON v.id = c.current_version_id
WHERE c.system_key = $1
FOR UPDATE OF c
`, systemKey).Scan(&campaignID, &versionID, &priority, &endsAt, &configRaw); err != nil {
		return fmt.Errorf("load legacy campaign %s: %w", systemKey, err)
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1 FROM user_reward_grants
  WHERE campaign_id = $1 AND user_id = $2 AND cycle_key = $3
)
`, campaignID, userID, cycleKey).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	var config service.RewardCampaignConfig
	if err := json.Unmarshal(configRaw, &config); err != nil {
		return err
	}
	copyRaw, err := marshalRewardCopySnapshot(config)
	if err != nil {
		return err
	}
	skin, err := chooseRewardSkinSnapshot(ctx, tx, config.SkinWeights)
	if err != nil {
		return err
	}
	skinRaw, err := json.Marshal(skin)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE reward_campaigns
SET total_budget = GREATEST(total_budget, spent_budget + reserved_budget + $2),
    reserved_budget = reserved_budget + $2,
    updated_at = NOW()
WHERE id = $1
`, campaignID, amount); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO user_reward_grants (
    campaign_id, campaign_version_id, user_id, skin_id, cycle_key, source, status,
    amount, priority, copy_snapshot, skin_snapshot, metadata, expires_at, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, 'pending', $7, $8, $9::jsonb, $10::jsonb,
          '{"legacy":true}'::jsonb, $11, NOW(), NOW())
ON CONFLICT (campaign_id, user_id, cycle_key) DO NOTHING
`, campaignID, versionID, userID, nullablePositiveInt64(skin.ID), cycleKey, source,
		amount, priority, copyRaw, skinRaw, nullTimeValue(endsAt)); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO reward_campaign_user_states (
    campaign_id, user_id, last_evaluated_at, last_won_at, last_granted_at,
    evaluation_count, win_count, grant_count,
    current_cycle_key, created_at, updated_at
) VALUES ($1, $2, $3, $3, $3, 1, 1, 1, $4, NOW(), NOW())
ON CONFLICT (campaign_id, user_id) DO UPDATE
SET last_evaluated_at = COALESCE(reward_campaign_user_states.last_evaluated_at, EXCLUDED.last_evaluated_at),
    last_won_at = COALESCE(reward_campaign_user_states.last_won_at, EXCLUDED.last_won_at),
    last_granted_at = COALESCE(reward_campaign_user_states.last_granted_at, EXCLUDED.last_granted_at),
    evaluation_count = GREATEST(reward_campaign_user_states.evaluation_count, 1),
    win_count = GREATEST(reward_campaign_user_states.win_count, 1),
    grant_count = GREATEST(reward_campaign_user_states.grant_count, 1),
    current_cycle_key = EXCLUDED.current_cycle_key,
    updated_at = NOW()
`, campaignID, userID, now, cycleKey)
	return err
}

func (r *rewardRepository) Claim(ctx context.Context, userID, grantID int64, now time.Time) (*service.RewardClaimResult, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var campaignID int64
	if err := tx.QueryRowContext(ctx, `SELECT campaign_id FROM user_reward_grants WHERE id = $1 AND user_id = $2`, grantID, userID).Scan(&campaignID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, infraerrors.NotFound("REWARD_NOT_FOUND", "reward not found")
		}
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `SELECT 1 FROM reward_campaigns WHERE id = $1 FOR UPDATE`, campaignID); err != nil {
		return nil, err
	}
	var status string
	var amount float64
	var expiresAt, claimedAt sql.NullTime
	var balanceAfter sql.NullFloat64
	if err := tx.QueryRowContext(ctx, `
SELECT status, amount, expires_at, claimed_at, balance_after
FROM user_reward_grants
WHERE id = $1 AND user_id = $2
FOR UPDATE
`, grantID, userID).Scan(&status, &amount, &expiresAt, &claimedAt, &balanceAfter); err != nil {
		return nil, err
	}
	if status == service.RewardGrantStatusClaimed && claimedAt.Valid && balanceAfter.Valid {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &service.RewardClaimResult{
			GrantID:        grantID,
			Amount:         amount,
			Balance:        balanceAfter.Float64,
			ClaimedAt:      claimedAt.Time,
			AlreadyClaimed: true,
		}, nil
	}
	if status != service.RewardGrantStatusPending {
		return nil, infraerrors.Conflict("REWARD_UNAVAILABLE", "reward is unavailable")
	}
	if expiresAt.Valid && !now.Before(expiresAt.Time) {
		if _, err := tx.ExecContext(ctx, `
UPDATE user_reward_grants
SET status = 'expired', expired_at = $2, updated_at = NOW()
WHERE id = $1 AND status = 'pending'
`, grantID, now); err != nil {
			return nil, err
		}
		budgetResult, err := tx.ExecContext(ctx, `
UPDATE reward_campaigns
SET reserved_budget = reserved_budget - $2,
    released_budget = released_budget + $2,
    updated_at = NOW()
WHERE id = $1 AND reserved_budget >= $2
`, campaignID, amount)
		if err != nil {
			return nil, err
		}
		if updated, _ := budgetResult.RowsAffected(); updated != 1 {
			return nil, fmt.Errorf("reward campaign %d reservation release mismatch", campaignID)
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, infraerrors.Conflict("REWARD_EXPIRED", "reward has expired")
	}

	var balance float64
	if err := tx.QueryRowContext(ctx, `
UPDATE users
SET balance = balance + $2, updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL
RETURNING balance
`, userID, amount).Scan(&balance); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, infraerrors.NotFound("USER_NOT_FOUND", "user not found")
		}
		return nil, err
	}
	claimReference, err := newRewardClaimReference(grantID)
	if err != nil {
		return nil, err
	}
	var claimRecordID int64
	if err := tx.QueryRowContext(ctx, `
INSERT INTO redeem_codes (code, type, value, status, used_by, used_at, notes, created_at, validity_days)
VALUES ($1, $2, $3, 'used', $4, $5, $6, $5, 0)
RETURNING id
`, claimReference, service.RedeemTypeCampaignReward, amount, userID, now,
		fmt.Sprintf("reward campaign grant #%d", grantID)).Scan(&claimRecordID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE user_reward_grants
SET status = 'claimed', claimed_at = $2, balance_after = $3,
    claim_record_id = $4, claim_reference = $5, updated_at = NOW()
WHERE id = $1 AND status = 'pending'
`, grantID, now, balance, claimRecordID, claimReference); err != nil {
		return nil, err
	}
	budgetResult, err := tx.ExecContext(ctx, `
UPDATE reward_campaigns
SET reserved_budget = reserved_budget - $2,
    spent_budget = spent_budget + $2,
    updated_at = NOW()
WHERE id = $1 AND reserved_budget >= $2
`, campaignID, amount)
	if err != nil {
		return nil, err
	}
	if updated, _ := budgetResult.RowsAffected(); updated != 1 {
		return nil, fmt.Errorf("reward campaign %d reservation spend mismatch", campaignID)
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE reward_campaign_user_states
SET last_claimed_at = $3, claim_count = claim_count + 1, updated_at = NOW()
WHERE campaign_id = $1 AND user_id = $2
`, campaignID, userID, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &service.RewardClaimResult{
		GrantID:   grantID,
		Amount:    amount,
		Balance:   balance,
		ClaimedAt: now,
	}, nil
}

func (r *rewardRepository) ExpirePendingForUser(ctx context.Context, userID int64, now time.Time) error {
	rows, err := r.db.QueryContext(ctx, `
SELECT DISTINCT campaign_id
FROM user_reward_grants
WHERE user_id = $1 AND status = 'pending' AND expires_at IS NOT NULL AND expires_at <= $2
ORDER BY campaign_id
`, userID, now)
	if err != nil {
		return err
	}
	var campaignIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		campaignIDs = append(campaignIDs, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	sort.Slice(campaignIDs, func(i, j int) bool { return campaignIDs[i] < campaignIDs[j] })
	for _, campaignID := range campaignIDs {
		tx, err := r.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `SELECT 1 FROM reward_campaigns WHERE id = $1 FOR UPDATE`, campaignID); err != nil {
			tx.Rollback()
			return err
		}
		if _, _, err := expireRewardGrantsLocked(ctx, tx, campaignID, userID, now, 0); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (r *rewardRepository) ExpirePendingCampaign(ctx context.Context, campaignID int64, now time.Time, limit int) (int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT 1 FROM reward_campaigns WHERE id = $1 FOR UPDATE`, campaignID); err != nil {
		return 0, err
	}
	count, _, err := expireRewardGrantsLocked(ctx, tx, campaignID, 0, now, limit)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return count, nil
}

func expireRewardGrantsLocked(ctx context.Context, tx *sql.Tx, campaignID, userID int64, now time.Time, limit int) (int64, float64, error) {
	if limit <= 0 {
		limit = 1_000_000_000
	}
	var count int64
	var released float64
	if err := tx.QueryRowContext(ctx, `
WITH selected AS (
    SELECT id
    FROM user_reward_grants
    WHERE campaign_id = $1
      AND ($2 = 0 OR user_id = $2)
      AND status = 'pending'
      AND expires_at IS NOT NULL
      AND expires_at <= $3
    ORDER BY id
    LIMIT $4
    FOR UPDATE
), expired AS (
    UPDATE user_reward_grants g
    SET status = 'expired', expired_at = $3, updated_at = NOW()
    FROM selected s
    WHERE g.id = s.id AND g.status = 'pending'
    RETURNING g.amount
)
SELECT COUNT(*), COALESCE(SUM(amount), 0) FROM expired
`, campaignID, userID, now, limit).Scan(&count, &released); err != nil {
		return 0, 0, err
	}
	if released > 0 {
		budgetResult, err := tx.ExecContext(ctx, `
UPDATE reward_campaigns
SET reserved_budget = reserved_budget - $2,
    released_budget = released_budget + $2,
    updated_at = NOW()
WHERE id = $1 AND reserved_budget >= $2
`, campaignID, released)
		if err != nil {
			return 0, 0, err
		}
		if updated, _ := budgetResult.RowsAffected(); updated != 1 {
			return 0, 0, fmt.Errorf("reward campaign %d reservation release mismatch", campaignID)
		}
	}
	return count, released, nil
}

func nullTimeValue(value sql.NullTime) any {
	if !value.Valid {
		return nil
	}
	return value.Time
}

func newRewardClaimReference(grantID int64) (string, error) {
	var nonce [8]byte
	if _, err := cryptorand.Read(nonce[:]); err != nil {
		return "", fmt.Errorf("generate reward claim reference: %w", err)
	}
	// redeem_codes.code is limited to 32 characters. The grant ID makes codes
	// unique across grants; the nonce prevents an operator-created code from
	// predictably occupying a future reward claim reference.
	return "r" + strconv.FormatInt(grantID, 36) + "-" + hex.EncodeToString(nonce[:]), nil
}
