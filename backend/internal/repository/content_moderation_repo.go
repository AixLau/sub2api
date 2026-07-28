package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type contentModerationRepository struct {
	db *sql.DB
}

func NewContentModerationRepository(db *sql.DB) service.ContentModerationRepository {
	return &contentModerationRepository{db: db}
}

func (r *contentModerationRepository) GetSemanticReviewUsageStats(ctx context.Context, since time.Time) (*service.ContentModerationSemanticReviewUsageStats, error) {
	stats := &service.ContentModerationSemanticReviewUsageStats{WindowHours: 24}
	if err := r.db.QueryRowContext(ctx, `
SELECT
    COUNT(*),
    COUNT(*) FILTER (WHERE model = $2),
    COUNT(*) FILTER (WHERE model = $3),
    COUNT(*) FILTER (WHERE model NOT IN ($2, $3)),
    COALESCE(SUM(input_tokens + cache_creation_tokens + cache_read_tokens), 0),
    COALESCE(SUM(output_tokens), 0),
    COALESCE(ROUND(AVG(duration_ms)), 0)::BIGINT
FROM usage_logs
WHERE source = 'content_moderation' AND created_at >= $1`,
		since,
		service.ContentModerationSemanticReviewPrimaryModel,
		service.ContentModerationSemanticReviewFallbackModel,
	).Scan(
		&stats.TotalCalls,
		&stats.PrimaryCalls,
		&stats.FallbackCalls,
		&stats.OtherCalls,
		&stats.InputTokens,
		&stats.OutputTokens,
		&stats.AvgLatencyMS,
	); err != nil {
		return nil, fmt.Errorf("get semantic review usage stats: %w", err)
	}
	stats.Available = true
	return stats, nil
}

func (r *contentModerationRepository) CreateLog(ctx context.Context, log *service.ContentModerationLog) error {
	if log == nil {
		return nil
	}
	categoryScores, err := json.Marshal(log.CategoryScores)
	if err != nil {
		return fmt.Errorf("marshal moderation category scores: %w", err)
	}
	thresholdSnapshot, err := json.Marshal(log.ThresholdSnapshot)
	if err != nil {
		return fmt.Errorf("marshal moderation thresholds: %w", err)
	}
	truncateReasons := log.TruncateReasons
	if truncateReasons == nil {
		truncateReasons = []string{}
	}
	truncateReasonsJSON, err := json.Marshal(truncateReasons)
	if err != nil {
		return fmt.Errorf("marshal moderation truncate reasons: %w", err)
	}
	metadata := log.Metadata
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}
	var metadataObject map[string]any
	if err := json.Unmarshal(metadata, &metadataObject); err != nil || metadataObject == nil {
		return fmt.Errorf("invalid moderation metadata JSON")
	}
	var userID any
	if log.UserID != nil {
		userID = *log.UserID
	}
	var apiKeyID any
	if log.APIKeyID != nil {
		apiKeyID = *log.APIKeyID
	}
	var groupID any
	if log.GroupID != nil {
		groupID = *log.GroupID
	}
	var accountID any
	if log.AccountID != nil {
		accountID = *log.AccountID
	}
	var latency any
	if log.UpstreamLatencyMS != nil {
		latency = *log.UpstreamLatencyMS
	}
	err = r.db.QueryRowContext(ctx, `
INSERT INTO content_moderation_logs (
    decision_id, request_id, user_id, user_email, api_key_id, api_key_name, group_id, group_name,
    account_id, account_name, account_type,
    endpoint, provider, model, mode, action, flagged, highest_category, highest_score,
    category_scores, threshold_snapshot, input_excerpt, upstream_latency_ms, error, metadata,
    matched_keyword, keyword_category, keyword_severity, keyword_action, effective_keyword_action,
    risk_context_type, risk_context_reason, review_status, review_note, reviewed_by, reviewed_at,
    violation_count, auto_banned, email_sent, queue_delay_ms,
    decision_source, moderation_provider, moderation_model, source_origin,
    selected_source, selected_source_role, selected_fragment_runes,
	    decision_cache_hit, duplicate_retry_count, user_violation_eligible, truncate_reasons
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8,
    $9, $10, $11,
    $12, $13, $14, $15, $16, $17, $18, $19,
    $20::jsonb, $21::jsonb, $22, $23, $24, $25::jsonb,
    $26, $27, $28, $29, $30,
    $31, $32, $33, $34, $35, $36,
    $37, $38, $39, $40,
    $41, $42, $43, $44, $45, $46, $47, $48, $49, $50, $51::jsonb
) ON CONFLICT (decision_id) WHERE decision_id <> '' DO UPDATE SET
    queue_delay_ms = COALESCE(EXCLUDED.queue_delay_ms, content_moderation_logs.queue_delay_ms),
    violation_count = GREATEST(content_moderation_logs.violation_count, EXCLUDED.violation_count),
    auto_banned = content_moderation_logs.auto_banned OR EXCLUDED.auto_banned,
    email_sent = content_moderation_logs.email_sent OR EXCLUDED.email_sent,
	    decision_cache_hit = content_moderation_logs.decision_cache_hit OR EXCLUDED.decision_cache_hit,
	    duplicate_retry_count = GREATEST(content_moderation_logs.duplicate_retry_count, EXCLUDED.duplicate_retry_count),
	    truncate_reasons = CASE
	        WHEN EXCLUDED.truncate_reasons <> '[]'::jsonb THEN EXCLUDED.truncate_reasons
	        ELSE content_moderation_logs.truncate_reasons
	    END
RETURNING id, created_at`,
		log.DecisionID, log.RequestID, userID, log.UserEmail, apiKeyID, log.APIKeyName, groupID, log.GroupName,
		accountID, log.AccountName, log.AccountType,
		log.Endpoint, log.Provider, log.Model, log.Mode, log.Action, log.Flagged, log.HighestCategory, log.HighestScore,
		string(categoryScores), string(thresholdSnapshot), log.InputExcerpt, latency, log.Error, string(metadata),
		log.MatchedKeyword, log.KeywordCategory, log.KeywordSeverity, log.KeywordAction, log.EffectiveKeywordAction,
		log.RiskContextType, log.RiskContextReason, log.ReviewStatus, log.ReviewNote, nullableInt64Ptr(log.ReviewedBy), log.ReviewedAt,
		log.ViolationCount, log.AutoBanned, log.EmailSent, nullableIntPtr(log.QueueDelayMS),
		log.DecisionSource, log.ModerationProvider, log.ModerationModel, log.SourceOrigin,
		log.SelectedSource, log.SelectedSourceRole, log.SelectedFragmentRunes,
		log.DecisionCacheHit, log.DuplicateRetryCount, log.UserViolationEligible, string(truncateReasonsJSON),
	).Scan(&log.ID, &log.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert content moderation log: %w", err)
	}
	return nil
}

func (r *contentModerationRepository) ListLogs(ctx context.Context, filter service.ContentModerationLogFilter) ([]service.ContentModerationLog, *pagination.PaginationResult, error) {
	where, args := buildContentModerationLogWhere(filter)
	whereSQL := "WHERE " + strings.Join(where, " AND ")

	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM content_moderation_logs l "+whereSQL, args...).Scan(&total); err != nil {
		return nil, nil, fmt.Errorf("count content moderation logs: %w", err)
	}

	params := filter.Pagination
	if params.Page <= 0 {
		params.Page = 1
	}
	if params.PageSize <= 0 {
		params.PageSize = 20
	}
	if params.PageSize > 100 {
		params.PageSize = 100
	}
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, params.Limit(), params.Offset())
	rows, err := r.db.QueryContext(ctx, `
SELECT
    l.id, l.request_id, l.user_id, l.user_email, l.api_key_id, l.api_key_name, l.group_id, l.group_name,
    l.account_id, l.account_name, l.account_type,
    l.endpoint, l.provider, l.model, l.mode, l.action, l.flagged, l.highest_category, l.highest_score,
    l.category_scores, l.threshold_snapshot, l.input_excerpt, l.upstream_latency_ms, l.error, l.metadata,
    COALESCE(l.matched_keyword, ''), COALESCE(l.keyword_category, ''), COALESCE(l.keyword_severity, ''),
    COALESCE(l.keyword_action, ''), COALESCE(l.effective_keyword_action, ''),
    COALESCE(l.risk_context_type, ''), COALESCE(l.risk_context_reason, ''),
    COALESCE(l.review_status, ''), COALESCE(l.review_note, ''), l.reviewed_by, l.reviewed_at,
    l.violation_count, l.auto_banned, l.email_sent, COALESCE(u.status, ''), l.queue_delay_ms,
    (rs.id IS NOT NULL), COALESCE(rs.body_bytes, 0), COALESCE(rs.truncated, FALSE),
    COALESCE(l.decision_source, ''), COALESCE(l.moderation_provider, ''), COALESCE(l.moderation_model, ''),
    COALESCE(l.source_origin, ''), COALESCE(l.selected_source, ''), COALESCE(l.selected_source_role, ''),
    COALESCE(l.selected_fragment_runes, 0), COALESCE(l.decision_cache_hit, FALSE),
	    COALESCE(l.duplicate_retry_count, 0), COALESCE(l.user_violation_eligible, FALSE),
	    COALESCE(l.truncate_reasons, '[]'::jsonb), (es.id IS NOT NULL),
    l.created_at
FROM content_moderation_logs l
LEFT JOIN users u ON u.id = l.user_id
LEFT JOIN content_moderation_raw_request_snapshots rs ON rs.log_id = l.id
LEFT JOIN content_moderation_evidence_snapshots es ON es.log_id = l.id `+whereSQL+`
ORDER BY l.created_at DESC, l.id DESC
LIMIT $`+fmt.Sprint(len(queryArgs)-1)+` OFFSET $`+fmt.Sprint(len(queryArgs)),
		queryArgs...,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("list content moderation logs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]service.ContentModerationLog, 0)
	for rows.Next() {
		var item service.ContentModerationLog
		var userID, apiKeyID, groupID, accountID, latency, queueDelay, reviewedBy sql.NullInt64
		var accountName, accountType sql.NullString
		var reviewedAt sql.NullTime
		var scoresRaw, thresholdsRaw, metadataRaw, truncateReasonsRaw []byte
		if err := rows.Scan(
			&item.ID,
			&item.RequestID,
			&userID,
			&item.UserEmail,
			&apiKeyID,
			&item.APIKeyName,
			&groupID,
			&item.GroupName,
			&accountID,
			&accountName,
			&accountType,
			&item.Endpoint,
			&item.Provider,
			&item.Model,
			&item.Mode,
			&item.Action,
			&item.Flagged,
			&item.HighestCategory,
			&item.HighestScore,
			&scoresRaw,
			&thresholdsRaw,
			&item.InputExcerpt,
			&latency,
			&item.Error,
			&metadataRaw,
			&item.MatchedKeyword,
			&item.KeywordCategory,
			&item.KeywordSeverity,
			&item.KeywordAction,
			&item.EffectiveKeywordAction,
			&item.RiskContextType,
			&item.RiskContextReason,
			&item.ReviewStatus,
			&item.ReviewNote,
			&reviewedBy,
			&reviewedAt,
			&item.ViolationCount,
			&item.AutoBanned,
			&item.EmailSent,
			&item.UserStatus,
			&queueDelay,
			&item.RawRequestAvailable,
			&item.RawRequestBytes,
			&item.RawRequestTruncated,
			&item.DecisionSource,
			&item.ModerationProvider,
			&item.ModerationModel,
			&item.SourceOrigin,
			&item.SelectedSource,
			&item.SelectedSourceRole,
			&item.SelectedFragmentRunes,
			&item.DecisionCacheHit,
			&item.DuplicateRetryCount,
			&item.UserViolationEligible,
			&truncateReasonsRaw,
			&item.EvidenceAvailable,
			&item.CreatedAt,
		); err != nil {
			return nil, nil, fmt.Errorf("scan content moderation log: %w", err)
		}
		if userID.Valid {
			v := userID.Int64
			item.UserID = &v
		}
		if apiKeyID.Valid {
			v := apiKeyID.Int64
			item.APIKeyID = &v
		}
		if groupID.Valid {
			v := groupID.Int64
			item.GroupID = &v
		}
		if accountID.Valid {
			v := accountID.Int64
			item.AccountID = &v
		}
		item.AccountName = accountName.String
		item.AccountType = accountType.String
		if latency.Valid {
			v := int(latency.Int64)
			item.UpstreamLatencyMS = &v
		}
		if queueDelay.Valid {
			v := int(queueDelay.Int64)
			item.QueueDelayMS = &v
		}
		if reviewedBy.Valid {
			v := reviewedBy.Int64
			item.ReviewedBy = &v
		}
		if reviewedAt.Valid {
			t := reviewedAt.Time
			item.ReviewedAt = &t
		}
		item.CategoryScores = map[string]float64{}
		_ = json.Unmarshal(scoresRaw, &item.CategoryScores)
		item.ThresholdSnapshot = map[string]float64{}
		_ = json.Unmarshal(thresholdsRaw, &item.ThresholdSnapshot)
		item.Metadata = append(json.RawMessage(nil), metadataRaw...)
		if len(item.Metadata) == 0 {
			item.Metadata = json.RawMessage(`{}`)
		}
		item.TruncateReasons = []string{}
		_ = json.Unmarshal(truncateReasonsRaw, &item.TruncateReasons)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate content moderation logs: %w", err)
	}
	return items, paginationResultFromTotal(total, params), nil
}

func (r *contentModerationRepository) CountFlaggedByUserSince(ctx context.Context, userID int64, since time.Time, excludeCyberPolicy bool) (int, error) {
	if userID <= 0 {
		return 0, nil
	}
	// SQL 中的 'cyber_policy' 字面量须与 service.ContentModerationActionCyberPolicy 保持一致。
	// pending/false_positive 不是已确认违规；空 review_status 是旧日志，仍沿用原计数语义。
	// candidate 日志仅在证据可归责或经人工确认后计数；空 source_origin 保留旧日志兼容性。
	var count int
	err := r.db.QueryRowContext(ctx, `
WITH last_auto_ban AS (
    SELECT MAX(created_at) AS at
    FROM content_moderation_logs
    WHERE user_id = $1 AND auto_banned = TRUE
      AND (COALESCE(mode, '') <> 'observe' OR review_status = 'confirmed_violation')
      AND (
          COALESCE(source_origin, '') = ''
          OR user_violation_eligible = TRUE
          OR review_status = 'confirmed_violation'
      )
)
SELECT COUNT(*)
FROM content_moderation_logs
WHERE user_id = $1
  AND flagged = TRUE
  AND action <> 'hash_block'
  AND action <> 'keyword_review'
  AND (action <> 'semantic_review_deferred' OR review_status = 'confirmed_violation')
  AND action NOT IN ('prompt_filter_observe', 'prompt_filter_warn', 'prompt_filter_review')
  AND COALESCE(review_status, '') NOT IN ('pending', 'false_positive')
  AND (COALESCE(mode, '') <> 'observe' OR review_status = 'confirmed_violation')
  AND (
      COALESCE(source_origin, '') = ''
      OR user_violation_eligible = TRUE
      OR review_status = 'confirmed_violation'
  )
  AND ($3::bool IS FALSE OR action <> 'cyber_policy')
  AND created_at >= $2
  AND created_at > COALESCE((SELECT at FROM last_auto_ban), '-infinity'::timestamptz)
`, userID, since, excludeCyberPolicy).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count user content moderation flagged logs: %w", err)
	}
	return count, nil
}

func (r *contentModerationRepository) UpdateLogEmailSent(ctx context.Context, id int64, sent bool) error {
	_, err := r.db.ExecContext(ctx, `UPDATE content_moderation_logs SET email_sent = $1 WHERE id = $2`, sent, id)
	if err != nil {
		return fmt.Errorf("update content moderation log email_sent: %w", err)
	}
	return nil
}

func (r *contentModerationRepository) UpdateLogViolationCountByDecisionID(ctx context.Context, decisionID string, count int) error {
	if strings.TrimSpace(decisionID) == "" {
		return nil
	}
	result, err := r.db.ExecContext(ctx, `
UPDATE content_moderation_logs
SET violation_count = GREATEST(COALESCE(violation_count, 0), $1)
WHERE decision_id = $2
`, count, decisionID)
	if err != nil {
		return fmt.Errorf("update content moderation log violation_count: %w", err)
	}
	return requireSingleContentModerationLogUpdate(result, "update content moderation log violation_count")
}

func (r *contentModerationRepository) UpdateLogAccountActionByDecisionID(ctx context.Context, decisionID string, violationCount int, autoBanned bool) error {
	if strings.TrimSpace(decisionID) == "" {
		return nil
	}
	result, err := r.db.ExecContext(ctx, `
UPDATE content_moderation_logs
SET violation_count = GREATEST(COALESCE(violation_count, 0), $1),
    auto_banned = COALESCE(auto_banned, FALSE) OR $2
WHERE decision_id = $3
`, violationCount, autoBanned, decisionID)
	if err != nil {
		return fmt.Errorf("update content moderation log account action: %w", err)
	}
	return requireSingleContentModerationLogUpdate(result, "update content moderation log account action")
}

func (r *contentModerationRepository) UpdateLogEmailSentByDecisionID(ctx context.Context, decisionID string, sent bool) error {
	if strings.TrimSpace(decisionID) == "" {
		return nil
	}
	result, err := r.db.ExecContext(ctx, `UPDATE content_moderation_logs SET email_sent = $1 WHERE decision_id = $2`, sent, decisionID)
	if err != nil {
		return fmt.Errorf("update content moderation log email_sent by decision: %w", err)
	}
	return requireSingleContentModerationLogUpdate(result, "update content moderation log email_sent by decision")
}

func requireSingleContentModerationLogUpdate(result sql.Result, operation string) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s result: %w", operation, err)
	}
	if affected != 1 {
		return fmt.Errorf("%s: expected 1 row, updated %d", operation, affected)
	}
	return nil
}

func (r *contentModerationRepository) GetLogAutoBannedByDecisionID(ctx context.Context, decisionID string) (bool, error) {
	if strings.TrimSpace(decisionID) == "" {
		return false, nil
	}
	var autoBanned bool
	err := r.db.QueryRowContext(ctx, `
SELECT auto_banned
FROM content_moderation_logs
WHERE decision_id = $1
`, decisionID).Scan(&autoBanned)
	if err != nil {
		return false, fmt.Errorf("get content moderation log auto_banned by decision: %w", err)
	}
	return autoBanned, nil
}

func (r *contentModerationRepository) GetLogNotificationDeliveredByDecisionID(ctx context.Context, decisionID, kind string) (bool, error) {
	if strings.TrimSpace(decisionID) == "" || strings.TrimSpace(kind) == "" {
		return false, nil
	}
	var delivered bool
	err := r.db.QueryRowContext(ctx, `
SELECT COALESCE(metadata->'notification_deliveries'->>$2, '') = 'true'
FROM content_moderation_logs
WHERE decision_id = $1
`, decisionID, kind).Scan(&delivered)
	if err != nil {
		return false, fmt.Errorf("get content moderation notification delivery state: %w", err)
	}
	return delivered, nil
}

func (r *contentModerationRepository) MarkLogNotificationDeliveredByDecisionID(ctx context.Context, decisionID, kind string, emailSent bool) error {
	if strings.TrimSpace(decisionID) == "" || strings.TrimSpace(kind) == "" {
		return nil
	}
	result, err := r.db.ExecContext(ctx, `
UPDATE content_moderation_logs
SET metadata = jsonb_set(
	        CASE
	            WHEN jsonb_typeof(metadata) = 'object' THEN metadata
	            ELSE '{}'::jsonb
	        END,
	        '{notification_deliveries}',
	        (
	            CASE
	                WHEN jsonb_typeof(metadata->'notification_deliveries') = 'object'
	                    THEN metadata->'notification_deliveries'
	                ELSE '{}'::jsonb
	            END
	        ) || jsonb_build_object($1, TRUE),
	        TRUE
	    ),
    email_sent = COALESCE(email_sent, FALSE) OR $2
WHERE decision_id = $3
`, kind, emailSent, decisionID)
	if err != nil {
		return fmt.Errorf("mark content moderation notification delivered: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read content moderation notification delivery update result: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("mark content moderation notification delivered: decision %q not found", decisionID)
	}
	return nil
}

func (r *contentModerationRepository) ReplaceSemanticReviewDeadLetterByDecisionID(ctx context.Context, log *service.ContentModerationLog) (bool, error) {
	if log == nil || strings.TrimSpace(log.DecisionID) == "" {
		return false, nil
	}
	categoryScores, err := json.Marshal(log.CategoryScores)
	if err != nil {
		return false, fmt.Errorf("marshal moderation category scores: %w", err)
	}
	thresholdSnapshot, err := json.Marshal(log.ThresholdSnapshot)
	if err != nil {
		return false, fmt.Errorf("marshal moderation thresholds: %w", err)
	}
	metadata := log.Metadata
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}
	var metadataObject map[string]any
	if err := json.Unmarshal(metadata, &metadataObject); err != nil || metadataObject == nil {
		return false, fmt.Errorf("invalid moderation metadata JSON")
	}
	truncateReasons := log.TruncateReasons
	if truncateReasons == nil {
		truncateReasons = []string{}
	}
	truncateReasonsJSON, err := json.Marshal(truncateReasons)
	if err != nil {
		return false, fmt.Errorf("marshal moderation truncate reasons: %w", err)
	}
	err = r.db.QueryRowContext(ctx, `
UPDATE content_moderation_logs
SET action = $1,
    flagged = $2,
    highest_category = $3,
    highest_score = $4,
    category_scores = $5::jsonb,
    threshold_snapshot = $6::jsonb,
    input_excerpt = $7,
    upstream_latency_ms = $8,
    error = $9,
    metadata = $10::jsonb,
    keyword_category = $11,
    keyword_severity = $12,
    effective_keyword_action = $13,
    risk_context_type = $14,
    risk_context_reason = $15,
    review_status = CASE WHEN reviewed_at IS NULL THEN $16 ELSE review_status END,
    decision_source = $17,
    moderation_provider = $18,
    moderation_model = $19,
    source_origin = $20,
    selected_source = $21,
    selected_source_role = $22,
    selected_fragment_runes = $23,
    user_violation_eligible = $24,
    truncate_reasons = $25::jsonb,
    queue_delay_ms = COALESCE($26, queue_delay_ms),
    decision_cache_hit = decision_cache_hit OR $27,
    duplicate_retry_count = GREATEST(duplicate_retry_count, $28)
WHERE decision_id = $29
  AND action = 'error'
  AND decision_source = 'semantic_review'
  AND reviewed_at IS NULL
RETURNING id, created_at
`,
		log.Action,
		log.Flagged,
		log.HighestCategory,
		log.HighestScore,
		string(categoryScores),
		string(thresholdSnapshot),
		log.InputExcerpt,
		nullableIntPtr(log.UpstreamLatencyMS),
		log.Error,
		string(metadata),
		log.KeywordCategory,
		log.KeywordSeverity,
		log.EffectiveKeywordAction,
		log.RiskContextType,
		log.RiskContextReason,
		log.ReviewStatus,
		log.DecisionSource,
		log.ModerationProvider,
		log.ModerationModel,
		log.SourceOrigin,
		log.SelectedSource,
		log.SelectedSourceRole,
		log.SelectedFragmentRunes,
		log.UserViolationEligible,
		string(truncateReasonsJSON),
		nullableIntPtr(log.QueueDelayMS),
		log.DecisionCacheHit,
		log.DuplicateRetryCount,
		log.DecisionID,
	).Scan(&log.ID, &log.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("replace semantic review dead-letter log: %w", err)
	}
	return true, nil
}

func (r *contentModerationRepository) CreateRawRequestSnapshot(ctx context.Context, snapshot *service.ContentModerationRawRequestSnapshot) error {
	if r == nil || r.db == nil || snapshot == nil || snapshot.LogID <= 0 || strings.TrimSpace(snapshot.BodyEncrypted) == "" {
		return nil
	}
	err := r.db.QueryRowContext(ctx, `
INSERT INTO content_moderation_raw_request_snapshots (
    log_id, request_id, body_encrypted, body_bytes, truncated
) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (log_id) DO UPDATE SET
    request_id = EXCLUDED.request_id,
    body_encrypted = EXCLUDED.body_encrypted,
    body_bytes = EXCLUDED.body_bytes,
    truncated = EXCLUDED.truncated
RETURNING id, created_at`,
		snapshot.LogID,
		snapshot.RequestID,
		snapshot.BodyEncrypted,
		snapshot.BodyBytes,
		snapshot.Truncated,
	).Scan(&snapshot.ID, &snapshot.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert content moderation raw request snapshot: %w", err)
	}
	return nil
}

func (r *contentModerationRepository) GetRawRequestSnapshotByLogID(ctx context.Context, logID int64) (*service.ContentModerationRawRequestSnapshot, error) {
	if r == nil || r.db == nil || logID <= 0 {
		return nil, infraerrors.NotFound("CONTENT_MODERATION_RAW_REQUEST_NOT_FOUND", "原始请求快照不存在")
	}
	var snapshot service.ContentModerationRawRequestSnapshot
	err := r.db.QueryRowContext(ctx, `
SELECT id, log_id, request_id, body_encrypted, body_bytes, truncated, created_at
FROM content_moderation_raw_request_snapshots
WHERE log_id = $1`,
		logID,
	).Scan(
		&snapshot.ID,
		&snapshot.LogID,
		&snapshot.RequestID,
		&snapshot.BodyEncrypted,
		&snapshot.BodyBytes,
		&snapshot.Truncated,
		&snapshot.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, infraerrors.NotFound("CONTENT_MODERATION_RAW_REQUEST_NOT_FOUND", "原始请求快照不存在")
		}
		return nil, fmt.Errorf("get content moderation raw request snapshot: %w", err)
	}
	return &snapshot, nil
}

func (r *contentModerationRepository) CreateEvidenceSnapshot(ctx context.Context, snapshot *service.ContentModerationEvidenceSnapshot) error {
	if r == nil || r.db == nil || snapshot == nil || snapshot.LogID <= 0 || strings.TrimSpace(snapshot.PayloadEncrypted) == "" {
		return nil
	}
	selection, err := json.Marshal(snapshot.Selection)
	if err != nil {
		return fmt.Errorf("marshal content moderation evidence selection: %w", err)
	}
	err = r.db.QueryRowContext(ctx, `
INSERT INTO content_moderation_evidence_snapshots (
    log_id, request_id, selection, payload_encrypted, payload_hmac, payload_runes
) VALUES ($1, $2, $3::jsonb, $4, $5, $6)
ON CONFLICT (log_id) DO UPDATE SET
    request_id = EXCLUDED.request_id,
    selection = EXCLUDED.selection,
    payload_encrypted = EXCLUDED.payload_encrypted,
    payload_hmac = EXCLUDED.payload_hmac,
    payload_runes = EXCLUDED.payload_runes
RETURNING id, created_at`,
		snapshot.LogID,
		snapshot.RequestID,
		string(selection),
		snapshot.PayloadEncrypted,
		snapshot.PayloadHMAC,
		snapshot.PayloadRunes,
	).Scan(&snapshot.ID, &snapshot.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert content moderation evidence snapshot: %w", err)
	}
	return nil
}

func (r *contentModerationRepository) GetEvidenceSnapshotByLogID(ctx context.Context, logID int64) (*service.ContentModerationEvidenceSnapshot, error) {
	if r == nil || r.db == nil || logID <= 0 {
		return nil, infraerrors.NotFound("CONTENT_MODERATION_EVIDENCE_NOT_FOUND", "审核送审证据不存在")
	}
	var snapshot service.ContentModerationEvidenceSnapshot
	var selectionRaw []byte
	err := r.db.QueryRowContext(ctx, `
SELECT id, log_id, request_id, selection, payload_encrypted, payload_hmac, payload_runes, created_at
FROM content_moderation_evidence_snapshots
WHERE log_id = $1`, logID).Scan(
		&snapshot.ID,
		&snapshot.LogID,
		&snapshot.RequestID,
		&selectionRaw,
		&snapshot.PayloadEncrypted,
		&snapshot.PayloadHMAC,
		&snapshot.PayloadRunes,
		&snapshot.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, infraerrors.NotFound("CONTENT_MODERATION_EVIDENCE_NOT_FOUND", "审核送审证据不存在")
		}
		return nil, fmt.Errorf("get content moderation evidence snapshot: %w", err)
	}
	snapshot.Selection = map[string]any{}
	if len(selectionRaw) > 0 {
		if err := json.Unmarshal(selectionRaw, &snapshot.Selection); err != nil {
			return nil, fmt.Errorf("decode content moderation evidence selection: %w", err)
		}
	}
	return &snapshot, nil
}

func (r *contentModerationRepository) IncrementDuplicateRetryCount(ctx context.Context, decisionID string) error {
	if r == nil || r.db == nil || strings.TrimSpace(decisionID) == "" {
		return nil
	}
	if _, err := r.db.ExecContext(ctx, `
UPDATE content_moderation_logs
SET duplicate_retry_count = duplicate_retry_count + 1,
    decision_cache_hit = TRUE
WHERE decision_id = $1`, decisionID); err != nil {
		return fmt.Errorf("increment content moderation duplicate retry count: %w", err)
	}
	return nil
}

func (r *contentModerationRepository) ReviewLog(ctx context.Context, id int64, input service.ContentModerationLogReviewInput) (*service.ContentModerationLog, error) {
	if id <= 0 {
		return nil, fmt.Errorf("invalid content moderation log id")
	}
	var reviewedBy any
	if input.ReviewedBy > 0 {
		reviewedBy = input.ReviewedBy
	}
	rows, err := r.db.QueryContext(ctx, `
WITH updated AS (
    UPDATE content_moderation_logs
    SET review_status = $2,
        review_note = $3,
        reviewed_by = $4,
        reviewed_at = NOW()
    WHERE id = $1
    RETURNING *
)
SELECT
    l.id, l.request_id, l.user_id, l.user_email, l.api_key_id, l.api_key_name, l.group_id, l.group_name,
    l.endpoint, l.provider, l.model, l.mode, l.action, l.flagged, l.highest_category, l.highest_score,
    l.category_scores, l.threshold_snapshot, l.input_excerpt, l.upstream_latency_ms, l.error, l.metadata,
    COALESCE(l.matched_keyword, ''), COALESCE(l.keyword_category, ''), COALESCE(l.keyword_severity, ''),
    COALESCE(l.keyword_action, ''), COALESCE(l.effective_keyword_action, ''),
    COALESCE(l.risk_context_type, ''), COALESCE(l.risk_context_reason, ''),
    COALESCE(l.review_status, ''), COALESCE(l.review_note, ''), l.reviewed_by, l.reviewed_at,
    l.violation_count, l.auto_banned, l.email_sent, COALESCE(u.status, ''), l.queue_delay_ms,
    (rs.id IS NOT NULL), COALESCE(rs.body_bytes, 0), COALESCE(rs.truncated, FALSE),
    COALESCE(l.decision_source, ''), COALESCE(l.moderation_provider, ''), COALESCE(l.moderation_model, ''),
    COALESCE(l.source_origin, ''), COALESCE(l.selected_source, ''), COALESCE(l.selected_source_role, ''),
    COALESCE(l.selected_fragment_runes, 0), COALESCE(l.decision_cache_hit, FALSE),
	    COALESCE(l.duplicate_retry_count, 0), COALESCE(l.user_violation_eligible, FALSE),
	    COALESCE(l.truncate_reasons, '[]'::jsonb), (es.id IS NOT NULL),
    l.created_at
FROM updated l
LEFT JOIN users u ON u.id = l.user_id
LEFT JOIN content_moderation_raw_request_snapshots rs ON rs.log_id = l.id
LEFT JOIN content_moderation_evidence_snapshots es ON es.log_id = l.id
`, id, input.Status, input.Note, reviewedBy)
	if err != nil {
		return nil, fmt.Errorf("review content moderation log: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items, err := scanContentModerationLogRows(rows)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, infraerrors.NotFound("CONTENT_MODERATION_LOG_NOT_FOUND", "审核记录不存在")
	}
	return &items[0], nil
}

func scanContentModerationLogRows(rows *sql.Rows) ([]service.ContentModerationLog, error) {
	items := make([]service.ContentModerationLog, 0)
	for rows.Next() {
		var item service.ContentModerationLog
		var userID, apiKeyID, groupID, latency, queueDelay, reviewedBy sql.NullInt64
		var reviewedAt sql.NullTime
		var scoresRaw, thresholdsRaw, metadataRaw, truncateReasonsRaw []byte
		if err := rows.Scan(
			&item.ID,
			&item.RequestID,
			&userID,
			&item.UserEmail,
			&apiKeyID,
			&item.APIKeyName,
			&groupID,
			&item.GroupName,
			&item.Endpoint,
			&item.Provider,
			&item.Model,
			&item.Mode,
			&item.Action,
			&item.Flagged,
			&item.HighestCategory,
			&item.HighestScore,
			&scoresRaw,
			&thresholdsRaw,
			&item.InputExcerpt,
			&latency,
			&item.Error,
			&metadataRaw,
			&item.MatchedKeyword,
			&item.KeywordCategory,
			&item.KeywordSeverity,
			&item.KeywordAction,
			&item.EffectiveKeywordAction,
			&item.RiskContextType,
			&item.RiskContextReason,
			&item.ReviewStatus,
			&item.ReviewNote,
			&reviewedBy,
			&reviewedAt,
			&item.ViolationCount,
			&item.AutoBanned,
			&item.EmailSent,
			&item.UserStatus,
			&queueDelay,
			&item.RawRequestAvailable,
			&item.RawRequestBytes,
			&item.RawRequestTruncated,
			&item.DecisionSource,
			&item.ModerationProvider,
			&item.ModerationModel,
			&item.SourceOrigin,
			&item.SelectedSource,
			&item.SelectedSourceRole,
			&item.SelectedFragmentRunes,
			&item.DecisionCacheHit,
			&item.DuplicateRetryCount,
			&item.UserViolationEligible,
			&truncateReasonsRaw,
			&item.EvidenceAvailable,
			&item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan content moderation log: %w", err)
		}
		if userID.Valid {
			v := userID.Int64
			item.UserID = &v
		}
		if apiKeyID.Valid {
			v := apiKeyID.Int64
			item.APIKeyID = &v
		}
		if groupID.Valid {
			v := groupID.Int64
			item.GroupID = &v
		}
		if latency.Valid {
			v := int(latency.Int64)
			item.UpstreamLatencyMS = &v
		}
		if queueDelay.Valid {
			v := int(queueDelay.Int64)
			item.QueueDelayMS = &v
		}
		if reviewedBy.Valid {
			v := reviewedBy.Int64
			item.ReviewedBy = &v
		}
		if reviewedAt.Valid {
			t := reviewedAt.Time
			item.ReviewedAt = &t
		}
		item.CategoryScores = map[string]float64{}
		_ = json.Unmarshal(scoresRaw, &item.CategoryScores)
		item.ThresholdSnapshot = map[string]float64{}
		_ = json.Unmarshal(thresholdsRaw, &item.ThresholdSnapshot)
		item.Metadata = append(json.RawMessage(nil), metadataRaw...)
		if len(item.Metadata) == 0 {
			item.Metadata = json.RawMessage(`{}`)
		}
		item.TruncateReasons = []string{}
		_ = json.Unmarshal(truncateReasonsRaw, &item.TruncateReasons)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate content moderation logs: %w", err)
	}
	return items, nil
}

func (r *contentModerationRepository) CleanupExpiredLogs(ctx context.Context, hitBefore time.Time, nonHitBefore time.Time) (*service.ContentModerationCleanupResult, error) {
	result := &service.ContentModerationCleanupResult{FinishedAt: time.Now()}
	if r == nil || r.db == nil {
		return result, nil
	}
	hitExec, err := r.db.ExecContext(ctx, `
DELETE FROM content_moderation_logs
WHERE flagged = TRUE AND created_at < $1
`, hitBefore)
	if err != nil {
		return nil, fmt.Errorf("delete expired hit content moderation logs: %w", err)
	}
	result.DeletedHit, _ = hitExec.RowsAffected()

	nonHitExec, err := r.db.ExecContext(ctx, `
DELETE FROM content_moderation_logs
WHERE flagged = FALSE AND created_at < $1
`, nonHitBefore)
	if err != nil {
		return nil, fmt.Errorf("delete expired non-hit content moderation logs: %w", err)
	}
	result.DeletedNonHit, _ = nonHitExec.RowsAffected()

	result.FinishedAt = time.Now()
	return result, nil
}

func nullableIntPtr(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInt64Ptr(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func buildContentModerationLogWhere(filter service.ContentModerationLogFilter) ([]string, []any) {
	where := []string{"l.id IS NOT NULL"}
	args := make([]any, 0)
	// Result filters are intentionally mutually exclusive. A flagged row can
	// also be blocked or awaiting review, but those are more actionable states
	// than the generic "hit" bucket.
	blockedActions := "'block', 'hash_block', 'keyword_block', 'prompt_filter_block', 'semantic_review_reject', 'semantic_review_deferred', 'semantic_review_unavailable', 'semantic_review_incomplete', 'cyber_policy', 'cyber_policy_session_blocked'"
	reviewActions := "'keyword_review', 'prompt_filter_review', 'semantic_review_review'"
	notBlockedOrReview := "l.action NOT IN (" + blockedActions + ") AND l.action NOT IN (" + reviewActions + ") AND COALESCE(l.review_status, '') <> 'pending'"
	add := func(expr string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(expr, len(args)))
	}
	switch strings.ToLower(strings.TrimSpace(filter.Result)) {
	case "hit", "flagged":
		where = append(where, "l.flagged = TRUE AND l.error = '' AND "+notBlockedOrReview)
	case "blocked", "block":
		where = append(where, "l.action IN ("+blockedActions+")")
	case "review":
		where = append(where, "(l.action IN ("+reviewActions+") OR l.review_status = 'pending') AND l.action NOT IN ("+blockedActions+")")
	case "keyword_review":
		where = append(where, "l.action = 'keyword_review'")
	case "pass", "allow":
		where = append(where, "l.flagged = FALSE AND l.error = '' AND "+notBlockedOrReview)
	case "error":
		where = append(where, "l.error <> '' AND "+notBlockedOrReview)
	}
	if decisionSource := strings.TrimSpace(filter.DecisionSource); decisionSource != "" {
		add("l.decision_source = $%d", decisionSource)
	}
	if reviewStatus := strings.TrimSpace(filter.ReviewStatus); reviewStatus != "" {
		add("l.review_status = $%d", reviewStatus)
	}
	if filter.GroupID != nil {
		add("l.group_id = $%d", *filter.GroupID)
	}
	if endpoint := strings.TrimSpace(filter.Endpoint); endpoint != "" {
		add("l.endpoint = $%d", endpoint)
	}
	if search := strings.TrimSpace(filter.Search); search != "" {
		like := "%" + search + "%"
		args = append(args, like, like, like, like, like, like, like, like, like, like)
		idx := len(args) - 9
		clauses := []string{
			fmt.Sprintf("l.request_id ILIKE $%d", idx),
			fmt.Sprintf("l.user_email ILIKE $%d", idx+1),
			fmt.Sprintf("l.api_key_name ILIKE $%d", idx+2),
			fmt.Sprintf("l.model ILIKE $%d", idx+3),
			fmt.Sprintf("l.matched_keyword ILIKE $%d", idx+4),
			fmt.Sprintf("l.keyword_category ILIKE $%d", idx+5),
			fmt.Sprintf("l.keyword_action ILIKE $%d", idx+6),
			fmt.Sprintf("l.effective_keyword_action ILIKE $%d", idx+7),
			fmt.Sprintf("l.risk_context_type ILIKE $%d", idx+8),
			fmt.Sprintf("l.review_status ILIKE $%d", idx+9),
		}
		if filter.SearchInputExcerpt {
			args = append(args, like)
			clauses = append(clauses, fmt.Sprintf("l.input_excerpt ILIKE $%d", len(args)))
		}
		where = append(where, "("+strings.Join(clauses, " OR ")+")")
	}
	if filter.From != nil && !filter.From.IsZero() {
		add("l.created_at >= $%d", *filter.From)
	}
	if filter.To != nil && !filter.To.IsZero() {
		add("l.created_at <= $%d", *filter.To)
	}
	return where, args
}
