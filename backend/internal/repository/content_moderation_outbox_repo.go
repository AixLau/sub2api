package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type contentModerationOutboxRepository struct {
	db *sql.DB
}

func NewContentModerationOutboxRepository(db *sql.DB) service.ContentModerationOutboxRepository {
	return &contentModerationOutboxRepository{db: db}
}

func (r *contentModerationOutboxRepository) EnqueueEvents(ctx context.Context, events []service.ContentModerationOutboxEvent) (int, error) {
	if len(events) == 0 {
		return 0, nil
	}
	if r == nil || r.db == nil {
		return 0, errors.New("content moderation outbox repository is unavailable")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = tx.Rollback()
	}()
	inserted := 0
	for _, event := range events {
		payload, err := json.Marshal(event.Payload)
		if err != nil {
			return 0, fmt.Errorf("marshal content moderation outbox payload: %w", err)
		}
		if event.MaxRetries <= 0 {
			event.MaxRetries = service.ContentModerationOutboxDefaultMaxRetries(event.Priority)
		}
		result, err := tx.ExecContext(ctx, `
INSERT INTO content_moderation_outbox (
	decision_id, event_type, event_key, priority, payload, max_retries
) VALUES (
	$1, $2, $3, $4, $5::jsonb, $6
)
ON CONFLICT (decision_id, event_type, event_key) DO NOTHING
`, event.DecisionID, event.EventType, event.EventKey, event.Priority, string(payload), event.MaxRetries)
		if err != nil {
			return 0, fmt.Errorf("insert content moderation outbox event: %w", err)
		}
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("read content moderation outbox insert result: %w", err)
		}
		inserted += int(rowsAffected)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return inserted, nil
}

func (r *contentModerationOutboxRepository) ClaimDueEvents(ctx context.Context, _ time.Time, limit int, lockFor time.Duration) ([]service.ContentModerationOutboxEvent, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("content moderation outbox repository is unavailable")
	}
	if limit <= 0 {
		limit = 20
	}
	if lockFor <= 0 {
		lockFor = 2 * time.Minute
	}
	rows, err := r.db.QueryContext(ctx, `
WITH selected AS (
	SELECT id
	FROM content_moderation_outbox
	WHERE status IN ('pending', 'retry', 'processing')
	  AND next_retry_at <= CURRENT_TIMESTAMP
	  AND (locked_until IS NULL OR locked_until < CURRENT_TIMESTAMP)
	ORDER BY CASE priority WHEN 'strong' THEN 0 ELSE 1 END, id ASC
	LIMIT $1
	FOR UPDATE SKIP LOCKED
), claimed AS (
	UPDATE content_moderation_outbox o
	SET status = 'processing',
	    locked_until = CURRENT_TIMESTAMP + $2::interval,
	    updated_at = NOW()
	FROM selected s
	WHERE o.id = s.id
	RETURNING o.id, o.decision_id, o.event_type, o.event_key, o.priority,
	          o.payload, o.retry_count, o.max_retries, o.next_retry_at, o.created_at, o.last_error,
	          o.locked_until
)
SELECT id, decision_id, event_type, event_key, priority,
       payload, retry_count, max_retries, next_retry_at, created_at, last_error, locked_until
FROM claimed
ORDER BY CASE priority WHEN 'strong' THEN 0 ELSE 1 END, id ASC
`, limit, fmt.Sprintf("%d milliseconds", lockFor.Milliseconds()))
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()
	out := make([]service.ContentModerationOutboxEvent, 0, limit)
	for rows.Next() {
		var event service.ContentModerationOutboxEvent
		var payloadRaw []byte
		if err := rows.Scan(
			&event.ID,
			&event.DecisionID,
			&event.EventType,
			&event.EventKey,
			&event.Priority,
			&payloadRaw,
			&event.RetryCount,
			&event.MaxRetries,
			&event.NextRetryAt,
			&event.CreatedAt,
			&event.LastError,
			&event.LeaseUntil,
		); err != nil {
			return nil, err
		}
		if len(payloadRaw) > 0 {
			event.Payload = map[string]any{}
			if err := json.Unmarshal(payloadRaw, &event.Payload); err != nil {
				return nil, err
			}
		}
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *contentModerationOutboxRepository) GetStatus(ctx context.Context, now time.Time) (*service.ContentModerationOutboxStatus, error) {
	if r == nil || r.db == nil {
		return &service.ContentModerationOutboxStatus{}, nil
	}
	var status service.ContentModerationOutboxStatus
	var oldestPending sql.NullTime
	var lastDeadLetter sql.NullTime
	var lastError sql.NullString
	err := r.db.QueryRowContext(ctx, `
SELECT
	COUNT(*) FILTER (WHERE status = 'pending') AS pending,
	COUNT(*) FILTER (WHERE status = 'retry') AS retry,
	COUNT(*) FILTER (WHERE status = 'processing') AS processing,
	COUNT(*) FILTER (WHERE status = 'succeeded') AS succeeded,
	COUNT(*) FILTER (WHERE status = 'dead_letter') AS dead_letter,
	MIN(created_at) FILTER (WHERE status IN ('pending', 'retry', 'processing')) AS oldest_pending_at,
	MAX(dead_letter_at) FILTER (WHERE status = 'dead_letter') AS last_dead_letter_at,
	(
		SELECT last_error
		FROM content_moderation_outbox
		WHERE last_error <> ''
		ORDER BY updated_at DESC, id DESC
		LIMIT 1
	) AS last_error
FROM content_moderation_outbox
`).Scan(
		&status.Pending,
		&status.Retry,
		&status.Processing,
		&status.Succeeded,
		&status.DeadLetter,
		&oldestPending,
		&lastDeadLetter,
		&lastError,
	)
	if err != nil {
		return nil, err
	}
	status.Enabled = true
	status.Healthy = status.DeadLetter == 0
	if oldestPending.Valid {
		status.OldestPendingAt = oldestPending.Time
		if now.After(oldestPending.Time) {
			status.OldestPendingAgeSeconds = int64(now.Sub(oldestPending.Time).Seconds())
		}
	}
	if lastDeadLetter.Valid {
		status.LastDeadLetterAt = lastDeadLetter.Time
	}
	if lastError.Valid {
		status.LastError = lastError.String
	}
	if status.OldestPendingAgeSeconds > int64((10 * time.Minute).Seconds()) {
		status.Healthy = false
	}
	return &status, nil
}

func (r *contentModerationOutboxRepository) ListDeadLetters(ctx context.Context, limit int) ([]service.ContentModerationOutboxEvent, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT id, decision_id, event_type, event_key, priority,
       payload, retry_count, max_retries, next_retry_at, created_at, last_error
FROM content_moderation_outbox
WHERE status = 'dead_letter'
ORDER BY dead_letter_at DESC NULLS LAST, id DESC
LIMIT $1
`, limit)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()
	return scanContentModerationOutboxEvents(rows, limit)
}

func (r *contentModerationOutboxRepository) ReplayDeadLetter(ctx context.Context, id int64) (bool, error) {
	if r == nil || r.db == nil || id <= 0 {
		return false, nil
	}
	result, err := r.db.ExecContext(ctx, `
UPDATE content_moderation_outbox
SET status = 'retry',
    retry_count = 0,
    next_retry_at = NOW(),
    locked_until = NULL,
    dead_letter_at = NULL,
    updated_at = NOW()
WHERE id = $1 AND status = 'dead_letter'
`, id)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (r *contentModerationOutboxRepository) Cleanup(ctx context.Context, succeededBefore time.Time, deadLetterBefore time.Time, limit int) (int64, error) {
	if r == nil || r.db == nil {
		return 0, nil
	}
	if limit <= 0 {
		limit = 5000
	}
	result, err := r.db.ExecContext(ctx, `
WITH doomed AS (
	SELECT id
	FROM content_moderation_outbox
	WHERE (status = 'succeeded' AND succeeded_at < $1)
	   OR (status = 'dead_letter' AND dead_letter_at < $2)
	ORDER BY id ASC
	LIMIT $3
)
DELETE FROM content_moderation_outbox o
USING doomed d
WHERE o.id = d.id
`, succeededBefore, deadLetterBefore, limit)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

type contentModerationOutboxRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanContentModerationOutboxEvents(rows contentModerationOutboxRows, capacity int) ([]service.ContentModerationOutboxEvent, error) {
	out := make([]service.ContentModerationOutboxEvent, 0, capacity)
	for rows.Next() {
		var event service.ContentModerationOutboxEvent
		var payloadRaw []byte
		if err := rows.Scan(
			&event.ID,
			&event.DecisionID,
			&event.EventType,
			&event.EventKey,
			&event.Priority,
			&payloadRaw,
			&event.RetryCount,
			&event.MaxRetries,
			&event.NextRetryAt,
			&event.CreatedAt,
			&event.LastError,
		); err != nil {
			return nil, err
		}
		if len(payloadRaw) > 0 {
			event.Payload = map[string]any{}
			if err := json.Unmarshal(payloadRaw, &event.Payload); err != nil {
				return nil, err
			}
		}
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *contentModerationOutboxRepository) MarkEventSucceeded(ctx context.Context, id int64, leaseUntil time.Time) error {
	if r == nil || r.db == nil {
		return errors.New("content moderation outbox repository is unavailable")
	}
	if id <= 0 || leaseUntil.IsZero() {
		return service.ErrContentModerationOutboxLeaseLost
	}
	result, err := r.db.ExecContext(ctx, `
UPDATE content_moderation_outbox
SET status = 'succeeded',
    locked_until = NULL,
    last_error = '',
    succeeded_at = NOW(),
    updated_at = NOW()
WHERE id = $1
  AND status = 'processing'
  AND locked_until = $2
`, id, leaseUntil)
	return contentModerationOutboxLeaseUpdateResult(result, err)
}

func (r *contentModerationOutboxRepository) ScheduleEventRetry(ctx context.Context, id int64, leaseUntil time.Time, retryCount int, nextRetryAt time.Time, lastError string) error {
	if r == nil || r.db == nil {
		return errors.New("content moderation outbox repository is unavailable")
	}
	if id <= 0 || leaseUntil.IsZero() {
		return service.ErrContentModerationOutboxLeaseLost
	}
	result, err := r.db.ExecContext(ctx, `
UPDATE content_moderation_outbox
SET status = 'retry',
    retry_count = $3,
    next_retry_at = $4,
    locked_until = NULL,
    last_error = $5,
    updated_at = NOW()
WHERE id = $1
  AND status = 'processing'
  AND locked_until = $2
`, id, leaseUntil, retryCount, nextRetryAt, lastError)
	return contentModerationOutboxLeaseUpdateResult(result, err)
}

func (r *contentModerationOutboxRepository) MarkEventDeadLetter(ctx context.Context, id int64, leaseUntil time.Time, lastError string) error {
	if r == nil || r.db == nil {
		return errors.New("content moderation outbox repository is unavailable")
	}
	if id <= 0 || leaseUntil.IsZero() {
		return service.ErrContentModerationOutboxLeaseLost
	}
	result, err := r.db.ExecContext(ctx, `
UPDATE content_moderation_outbox
SET status = 'dead_letter',
    locked_until = NULL,
    last_error = $3,
    dead_letter_at = NOW(),
    updated_at = NOW()
WHERE id = $1
  AND status = 'processing'
  AND locked_until = $2
`, id, leaseUntil, lastError)
	return contentModerationOutboxLeaseUpdateResult(result, err)
}

func contentModerationOutboxLeaseUpdateResult(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return service.ErrContentModerationOutboxLeaseLost
	}
	return nil
}
