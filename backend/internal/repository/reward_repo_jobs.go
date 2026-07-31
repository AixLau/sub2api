package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *rewardRepository) EnqueueScheduledCampaigns(ctx context.Context, now time.Time) (int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := endExpiredRewardCampaigns(ctx, tx, now); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE reward_campaigns
SET status = 'active', updated_at = NOW()
WHERE status = 'scheduled'
  AND (starts_at IS NULL OR starts_at <= $1)
  AND (ends_at IS NULL OR ends_at > $1)
`, now); err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `
INSERT INTO reward_campaign_jobs (
    campaign_id, campaign_version_id, job_type, idempotency_key, status,
    cursor_user_id, max_user_id, total_users,
    scheduled_at, next_attempt_at, created_at, updated_at
)
SELECT c.id, c.current_version_id, 'issue_batch',
       'reward:campaign:' || c.id || ':version:' || c.current_version_id || ':issue',
       'pending', 0, bounds.max_user_id, bounds.total_users,
       COALESCE(c.starts_at, $1), $1, NOW(), NOW()
FROM reward_campaigns c
CROSS JOIN (
    SELECT COALESCE(MAX(id), 0) AS max_user_id, COUNT(*) AS total_users
    FROM users
    WHERE role = 'user' AND status = 'active' AND deleted_at IS NULL
) bounds
WHERE c.issuance_mode = 'scheduled_batch'
  AND c.status = 'active'
  AND c.current_version_id IS NOT NULL
  AND (c.starts_at IS NULL OR c.starts_at <= $1)
  AND (c.ends_at IS NULL OR c.ends_at > $1)
ON CONFLICT (idempotency_key) DO NOTHING
`, now)
	if err != nil {
		return 0, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return inserted, nil
}

func (r *rewardRepository) EndExpiredCampaigns(ctx context.Context, now time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := endExpiredRewardCampaigns(ctx, tx, now); err != nil {
		return err
	}
	return tx.Commit()
}

func endExpiredRewardCampaigns(ctx context.Context, tx *sql.Tx, now time.Time) error {
	rows, err := tx.QueryContext(ctx, `
SELECT id
FROM reward_campaigns
WHERE status IN ('scheduled', 'active', 'paused')
  AND ends_at IS NOT NULL
  AND ends_at <= $1
ORDER BY id
FOR UPDATE SKIP LOCKED
`, now)
	if err != nil {
		return err
	}
	campaignIDs := make([]int64, 0)
	for rows.Next() {
		var campaignID int64
		if err := rows.Scan(&campaignID); err != nil {
			rows.Close()
			return err
		}
		campaignIDs = append(campaignIDs, campaignID)
	}
	if err := rows.Close(); err != nil {
		return err
	}

	for _, campaignID := range campaignIDs {
		if _, _, err := expireAllRewardGrantsLocked(ctx, tx, campaignID, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE reward_campaign_jobs
SET status = 'cancelled', lease_owner = '', lease_expires_at = NULL,
    finished_at = $2, updated_at = NOW()
WHERE campaign_id = $1
  AND status NOT IN ('succeeded', 'failed', 'dead_letter', 'cancelled')
`, campaignID, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE reward_campaigns
SET status = 'ended', ended_at = COALESCE(ended_at, $2), paused_at = NULL,
    updated_at = NOW()
WHERE id = $1
`, campaignID, now); err != nil {
			return err
		}
	}
	return nil
}

func (r *rewardRepository) ClaimJobs(ctx context.Context, workerID string, now time.Time, limit int, lease time.Duration) ([]service.RewardCampaignJob, error) {
	if limit <= 0 {
		limit = 2
	}
	if lease <= 0 {
		lease = 2 * time.Minute
	}
	rows, err := r.db.QueryContext(ctx, `
WITH selected AS (
    SELECT j.id
    FROM reward_campaign_jobs j
    JOIN reward_campaigns c ON c.id = j.campaign_id
    WHERE j.status IN ('pending', 'retry', 'processing')
      AND j.job_type = 'issue_batch'
      AND j.next_attempt_at <= $1
      AND (j.lease_expires_at IS NULL OR j.lease_expires_at < $1)
      AND c.status = 'active'
      AND c.issuance_mode = 'scheduled_batch'
      AND (c.ends_at IS NULL OR c.ends_at > $1)
    ORDER BY j.next_attempt_at, j.id
    LIMIT $2
    FOR UPDATE OF j SKIP LOCKED
), claimed AS (
    UPDATE reward_campaign_jobs j
    SET status = 'processing', lease_owner = $3,
        lease_expires_at = $1 + $4::interval,
        started_at = COALESCE(started_at, $1),
        attempt_count = attempt_count + 1,
        updated_at = NOW()
    FROM selected s
    WHERE j.id = s.id
    RETURNING j.id, j.campaign_id, j.campaign_version_id, j.idempotency_key,
              j.status, j.scheduled_at, j.next_attempt_at, j.cursor_user_id, j.max_user_id,
              j.scanned_users, j.matched_users, j.granted_users,
              j.attempt_count, j.max_attempts, j.lease_owner,
              j.lease_expires_at, j.last_error, j.finished_at,
              j.created_at, j.updated_at
)
SELECT * FROM claimed ORDER BY id
`, now, limit, workerID, fmt.Sprintf("%d milliseconds", lease.Milliseconds()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := make([]service.RewardCampaignJob, 0, limit)
	for rows.Next() {
		job, err := scanRewardJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, *job)
	}
	return jobs, rows.Err()
}

func scanRewardJob(scanner rewardScanner) (*service.RewardCampaignJob, error) {
	var job service.RewardCampaignJob
	var owner sql.NullString
	var lease, finished sql.NullTime
	if err := scanner.Scan(
		&job.ID, &job.CampaignID, &job.VersionID, &job.IdempotencyKey,
		&job.Status, &job.ScheduledAt, &job.AvailableAt, &job.CursorUserID, &job.MaxUserID,
		&job.ProcessedCount, &job.EligibleCount, &job.GrantedCount,
		&job.RetryCount, &job.MaxRetries, &owner, &lease, &job.LastError,
		&finished, &job.CreatedAt, &job.UpdatedAt,
	); err != nil {
		return nil, err
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
	return &job, nil
}

func (r *rewardRepository) ListBatchCandidateUserIDs(ctx context.Context, campaignID, afterUserID, maxUserID int64, limit int) ([]int64, error) {
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT id
FROM users
WHERE id > $1 AND id <= $2 AND role = 'user' AND status = 'active' AND deleted_at IS NULL
ORDER BY id
LIMIT $3
`, afterUserID, maxUserID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]int64, 0, limit)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	_ = campaignID
	return ids, rows.Err()
}

func (r *rewardRepository) ExtendJobLease(
	ctx context.Context,
	jobID int64,
	workerID string,
	cursorUserID int64,
	scanned, matched, granted, skipped, failed int64,
	now time.Time,
	lease time.Duration,
) error {
	result, err := r.db.ExecContext(ctx, `
UPDATE reward_campaign_jobs
SET cursor_user_id = GREATEST(cursor_user_id, $3),
    scanned_users = scanned_users + $4,
    matched_users = matched_users + $5,
    granted_users = granted_users + $6,
    skipped_users = skipped_users + $7,
    failed_users = failed_users + $8,
	    lease_expires_at = $9::timestamptz + $10::interval,
    updated_at = NOW()
WHERE id = $1 AND lease_owner = $2 AND status = 'processing'
`, jobID, workerID, cursorUserID, scanned, matched, granted, skipped, failed, now,
		fmt.Sprintf("%d milliseconds", lease.Milliseconds()))
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return infraerrors.Conflict("REWARD_JOB_LEASE_LOST", "reward job lease was lost")
	}
	return nil
}

func (r *rewardRepository) CompleteJob(ctx context.Context, jobID int64, workerID string, now time.Time) error {
	result, err := r.db.ExecContext(ctx, `
UPDATE reward_campaign_jobs
SET status = 'succeeded', lease_owner = '', lease_expires_at = NULL,
    finished_at = $3, last_error = '', updated_at = NOW()
WHERE id = $1 AND lease_owner = $2 AND status = 'processing'
`, jobID, workerID, now)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return infraerrors.Conflict("REWARD_JOB_LEASE_LOST", "reward job lease was lost")
	}
	return nil
}

func (r *rewardRepository) ReleaseJob(ctx context.Context, jobID int64, workerID string, now time.Time) error {
	result, err := r.db.ExecContext(ctx, `
UPDATE reward_campaign_jobs
SET status = 'retry', lease_owner = '', lease_expires_at = NULL,
    next_attempt_at = $3, attempt_count = GREATEST(attempt_count - 1, 0),
    last_error = '', finished_at = NULL, updated_at = NOW()
WHERE id = $1 AND lease_owner = $2 AND status = 'processing'
`, jobID, workerID, now)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return infraerrors.Conflict("REWARD_JOB_LEASE_LOST", "reward job lease was lost")
	}
	return nil
}

func (r *rewardRepository) RetryJob(ctx context.Context, jobID int64, workerID string, now time.Time, cause error) error {
	message := "unknown reward job error"
	if cause != nil {
		message = cause.Error()
	}
	result, err := r.db.ExecContext(ctx, `
UPDATE reward_campaign_jobs
SET status = CASE WHEN attempt_count >= max_attempts THEN 'dead_letter' ELSE 'retry' END,
    lease_owner = '', lease_expires_at = NULL,
    next_attempt_at = CASE
      WHEN attempt_count >= max_attempts THEN next_attempt_at
      ELSE $3 + (LEAST(3600, POWER(2, LEAST(attempt_count, 11))) * INTERVAL '1 second')
    END,
    last_error = LEFT($4, 4000),
    finished_at = CASE WHEN attempt_count >= max_attempts THEN $3 ELSE NULL END,
    updated_at = NOW()
WHERE id = $1 AND lease_owner = $2 AND status = 'processing'
`, jobID, workerID, now, message)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return infraerrors.Conflict("REWARD_JOB_LEASE_LOST", "reward job lease was lost")
	}
	return nil
}

func (r *rewardRepository) getJob(ctx context.Context, jobID int64) (*service.RewardCampaignJob, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT id, campaign_id, campaign_version_id, idempotency_key, status,
       scheduled_at, next_attempt_at, cursor_user_id, max_user_id, scanned_users, matched_users,
       granted_users, attempt_count, max_attempts, lease_owner, lease_expires_at,
       last_error, finished_at, created_at, updated_at
FROM reward_campaign_jobs WHERE id = $1
`, jobID)
	job, err := scanRewardJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, infraerrors.NotFound("REWARD_JOB_NOT_FOUND", "reward job not found")
	}
	return job, err
}
