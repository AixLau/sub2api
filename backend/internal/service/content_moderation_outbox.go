package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

const (
	ContentModerationOutboxEventLogWrite       = "content_moderation_log_write"
	ContentModerationOutboxEventViolationCount = "violation_count_update"
	ContentModerationOutboxEventUserAutoBan    = "user_auto_ban"
	ContentModerationOutboxEventHashBlock      = "hash_block_record"
	ContentModerationOutboxEventEmail          = "email_notification"
	ContentModerationOutboxEventAdminAlert     = "admin_alert"
	ContentModerationOutboxEventSemanticReview = "semantic_review"

	ContentModerationOutboxPriorityStrong = "strong"
	ContentModerationOutboxPriorityWeak   = "weak"
)

const (
	contentModerationOutboxDefaultClaimLimit  = 20
	contentModerationOutboxLockDuration       = 2 * time.Minute
	contentModerationOutboxPollInterval       = time.Second
	contentModerationOutboxStrongMaxRetries   = 20
	contentModerationOutboxWeakMaxRetries     = 5
	contentModerationOutboxCleanupBatch       = 5000
	contentModerationOutboxSucceededKeepDays  = 7
	contentModerationOutboxDeadLetterKeepDays = 90
	contentModerationOutboxRecordMaxBytes     = 2 * 1024 * 1024
	contentModerationOutboxRecordEncoding     = "gzip_base64"
	contentModerationOutboxEventTimeout       = time.Duration(ContentModerationSemanticReviewMaxTimeoutMS)*time.Millisecond + 2*contentModerationPersistenceTimeout
	contentModerationOutboxAutoBanRecovery    = "auto_ban_applied_audit_state_unpersisted"
	contentModerationOutboxDeliveryRecovery   = "notification_delivered_audit_state_unpersisted:"
)

var ErrContentModerationOutboxLeaseLost = errors.New("content moderation outbox lease lost")

type ContentModerationOutboxRepository interface {
	EnqueueEvents(ctx context.Context, events []ContentModerationOutboxEvent) (int, error)
	ClaimDueEvents(ctx context.Context, now time.Time, limit int, lockFor time.Duration) ([]ContentModerationOutboxEvent, error)
	MarkEventSucceeded(ctx context.Context, id int64, leaseUntil time.Time) error
	ScheduleEventRetry(ctx context.Context, id int64, leaseUntil time.Time, retryCount int, nextRetryAt time.Time, lastError string) error
	MarkEventDeadLetter(ctx context.Context, id int64, leaseUntil time.Time, lastError string) error
	GetStatus(ctx context.Context, now time.Time) (*ContentModerationOutboxStatus, error)
	ListDeadLetters(ctx context.Context, limit int) ([]ContentModerationOutboxEvent, error)
	ReplayDeadLetter(ctx context.Context, id int64) (bool, error)
	Cleanup(ctx context.Context, succeededBefore time.Time, deadLetterBefore time.Time, limit int) (int64, error)
}

type ContentModerationOutboxEvent struct {
	ID          int64          `json:"id,omitempty"`
	DecisionID  string         `json:"decision_id"`
	EventType   string         `json:"event_type"`
	EventKey    string         `json:"event_key,omitempty"`
	Priority    string         `json:"priority"`
	Payload     map[string]any `json:"payload,omitempty"`
	RetryCount  int            `json:"retry_count,omitempty"`
	MaxRetries  int            `json:"max_retries,omitempty"`
	NextRetryAt time.Time      `json:"next_retry_at,omitempty"`
	CreatedAt   time.Time      `json:"created_at,omitempty"`
	LastError   string         `json:"last_error,omitempty"`
	LeaseUntil  time.Time      `json:"-"`
}

type ContentModerationOutboxStatus struct {
	Enabled                 bool      `json:"enabled"`
	Healthy                 bool      `json:"healthy"`
	Pending                 int64     `json:"pending"`
	Retry                   int64     `json:"retry"`
	Processing              int64     `json:"processing"`
	Succeeded               int64     `json:"succeeded"`
	DeadLetter              int64     `json:"dead_letter"`
	OldestPendingAgeSeconds int64     `json:"oldest_pending_age_seconds"`
	OldestPendingAt         time.Time `json:"oldest_pending_at,omitempty"`
	LastError               string    `json:"last_error,omitempty"`
	LastDeadLetterAt        time.Time `json:"last_dead_letter_at,omitempty"`
	LastCleanupAt           time.Time `json:"last_cleanup_at,omitempty"`
	LastCleanupDeleted      int64     `json:"last_cleanup_deleted"`
}

type contentModerationOutboxPayload struct {
	SchemaVersion   int                                           `json:"schema_version,omitempty"`
	Log             *ContentModerationLog                         `json:"log,omitempty"`
	Config          *ContentModerationConfig                      `json:"config,omitempty"`
	RecordEncrypted string                                        `json:"record_encrypted,omitempty"`
	RecordEncoding  string                                        `json:"record_encoding,omitempty"`
	InputHash       string                                        `json:"input_hash,omitempty"`
	EmailKind       string                                        `json:"email_kind,omitempty"`
	SemanticReview  *contentModerationSemanticReviewOutboxPayload `json:"semantic_review,omitempty"`
}

type contentModerationOutboxRecord struct {
	Log       *ContentModerationLog    `json:"log"`
	Config    *ContentModerationConfig `json:"config,omitempty"`
	InputHash string                   `json:"input_hash,omitempty"`
}

type contentModerationAccountActionStateRepository interface {
	GetLogAutoBannedByDecisionID(ctx context.Context, decisionID string) (bool, error)
}

type contentModerationNotificationDeliveryStateRepository interface {
	GetLogNotificationDeliveredByDecisionID(ctx context.Context, decisionID, kind string) (bool, error)
	MarkLogNotificationDeliveredByDecisionID(ctx context.Context, decisionID, kind string, emailSent bool) error
}

// SetOutboxRepository configures outbox processing before Start is called.
func (s *ContentModerationService) SetOutboxRepository(repo ContentModerationOutboxRepository) {
	if s == nil {
		return
	}
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	if s.runtimeStarted || s.runtimeClosed {
		return
	}
	s.outboxRepo = repo
}

func (s *ContentModerationService) contentModerationOutboxRepository() ContentModerationOutboxRepository {
	if s == nil {
		return nil
	}
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	return s.outboxRepo
}

func (s *ContentModerationService) enqueueModerationOutboxRecord(input ContentModerationCheckInput, cfg *ContentModerationConfig, log *ContentModerationLog, inputHash string, recordHash bool, applySideEffects bool) bool {
	outboxRepo := s.contentModerationOutboxRepository()
	if outboxRepo == nil || log == nil {
		return false
	}
	if strings.TrimSpace(log.DecisionID) == "" {
		log.DecisionID = contentModerationDecisionID(input, log, inputHash)
	}
	if !contentModerationOutboxEnforcementEligible(log) {
		recordHash = false
		applySideEffects = false
	}
	payload, err := s.encryptedContentModerationOutboxPayload(log, cfg, inputHash)
	if err != nil {
		slog.Warn("content_moderation.outbox_encrypt_failed", "decision_id", log.DecisionID, "error", err)
		return false
	}
	events := []ContentModerationOutboxEvent{
		newContentModerationOutboxEvent(log.DecisionID, ContentModerationOutboxEventLogWrite, "", ContentModerationOutboxPriorityStrong, payload),
	}
	if recordHash {
		events = append(events, newContentModerationOutboxEvent(log.DecisionID, ContentModerationOutboxEventHashBlock, "", ContentModerationOutboxPriorityStrong, payload))
	}
	if applySideEffects && log.Flagged {
		if log.UserID != nil && *log.UserID > 0 {
			events = append(events,
				newContentModerationOutboxEvent(log.DecisionID, ContentModerationOutboxEventViolationCount, "", ContentModerationOutboxPriorityStrong, payload),
				newContentModerationOutboxEvent(log.DecisionID, ContentModerationOutboxEventUserAutoBan, "", ContentModerationOutboxPriorityStrong, payload),
			)
		}
		if cfg != nil && cfg.EmailOnHit && strings.TrimSpace(log.UserEmail) != "" {
			emailPayload := payload
			emailPayload.EmailKind = "violation"
			events = append(events, newContentModerationOutboxEvent(log.DecisionID, ContentModerationOutboxEventEmail, "violation", ContentModerationOutboxPriorityWeak, emailPayload))
		}
		events = append(events, newContentModerationOutboxEvent(log.DecisionID, ContentModerationOutboxEventAdminAlert, "", ContentModerationOutboxPriorityWeak, payload))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := outboxRepo.EnqueueEvents(ctx, events); err != nil {
		slog.Warn("content_moderation.outbox_enqueue_failed",
			"user_id", input.UserID,
			"endpoint", input.Endpoint,
			"action", log.Action,
			"decision_id", log.DecisionID,
			"error", err)
		return false
	}
	return true
}

func contentModerationOutboxEnforcementEligible(log *ContentModerationLog) bool {
	if log == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(log.Mode), ContentModerationModeObserve) {
		return false
	}
	// Empty source_origin identifies legacy and ordinary-provider logs, which
	// predate the explicit candidate-attribution field.
	return strings.TrimSpace(log.SourceOrigin) == "" || log.UserViolationEligible
}

func (s *ContentModerationService) encryptedContentModerationOutboxPayload(log *ContentModerationLog, cfg *ContentModerationConfig, inputHash string) (contentModerationOutboxPayload, error) {
	if s == nil || s.rawRequestEncryptor == nil {
		return contentModerationOutboxPayload{}, errors.New("content moderation outbox encryptor is unavailable")
	}
	record := contentModerationOutboxRecord{
		Log:       cloneContentModerationLog(log),
		Config:    safeContentModerationConfigForOutbox(cfg),
		InputHash: inputHash,
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return contentModerationOutboxPayload{}, fmt.Errorf("marshal content moderation outbox record: %w", err)
	}
	encoded, err := encodeContentModerationOutboxRecord(raw)
	if err != nil {
		return contentModerationOutboxPayload{}, err
	}
	encrypted, err := s.rawRequestEncryptor.Encrypt(encoded)
	if err != nil {
		return contentModerationOutboxPayload{}, fmt.Errorf("encrypt content moderation outbox record: %w", err)
	}
	return contentModerationOutboxPayload{
		SchemaVersion:   2,
		RecordEncrypted: encrypted,
		RecordEncoding:  contentModerationOutboxRecordEncoding,
	}, nil
}

func encodeContentModerationOutboxRecord(raw []byte) (string, error) {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(raw); err != nil {
		_ = writer.Close()
		return "", fmt.Errorf("compress content moderation outbox record: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("close content moderation outbox compressor: %w", err)
	}
	return base64.StdEncoding.EncodeToString(compressed.Bytes()), nil
}

func decodeContentModerationOutboxRecord(value, encoding string) ([]byte, error) {
	if strings.TrimSpace(encoding) == "" {
		return []byte(value), nil
	}
	if encoding != contentModerationOutboxRecordEncoding {
		return nil, fmt.Errorf("unsupported content moderation outbox record encoding %q", encoding)
	}
	compressed, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("decode content moderation outbox record: %w", err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("open content moderation outbox record: %w", err)
	}
	defer reader.Close()
	limited := io.LimitReader(reader, contentModerationOutboxRecordMaxBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("decompress content moderation outbox record: %w", err)
	}
	if len(raw) > contentModerationOutboxRecordMaxBytes {
		return nil, errors.New("content moderation outbox record exceeds decompressed size limit")
	}
	return raw, nil
}

func newContentModerationOutboxEvent(decisionID, eventType, eventKey, priority string, payload contentModerationOutboxPayload) ContentModerationOutboxEvent {
	return ContentModerationOutboxEvent{
		DecisionID: strings.TrimSpace(decisionID),
		EventType:  eventType,
		EventKey:   eventKey,
		Priority:   priority,
		Payload:    contentModerationOutboxPayloadMap(payload),
		MaxRetries: ContentModerationOutboxDefaultMaxRetries(priority),
	}
}

func ContentModerationOutboxDefaultMaxRetries(priority string) int {
	if priority == ContentModerationOutboxPriorityWeak {
		return contentModerationOutboxWeakMaxRetries
	}
	return contentModerationOutboxStrongMaxRetries
}

func contentModerationDecisionID(input ContentModerationCheckInput, log *ContentModerationLog, inputHash string) string {
	if log != nil && strings.TrimSpace(log.DecisionID) != "" {
		return trimContentModerationDecisionID(strings.TrimSpace(log.DecisionID))
	}
	if strings.TrimSpace(input.RequestID) != "" {
		return trimContentModerationDecisionID("cm_" + strings.TrimSpace(input.RequestID) + "_" + contentModerationRandomToken())
	}
	if strings.TrimSpace(inputHash) != "" {
		return trimContentModerationDecisionID("cm_" + strings.TrimSpace(inputHash)[:min(16, len(strings.TrimSpace(inputHash)))] + "_" + contentModerationRandomToken())
	}
	return trimContentModerationDecisionID("cm_" + strconv.FormatInt(time.Now().UnixNano(), 36) + "_" + contentModerationRandomToken())
}

func trimContentModerationDecisionID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 128 {
		return value
	}
	return value[:111] + "_" + contentModerationRandomToken()
}

func contentModerationRandomToken() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return fmt.Sprintf("%x", b[:])
}

func contentModerationOutboxPayloadMap(payload contentModerationOutboxPayload) map[string]any {
	// Outbox records survive process restarts and are visible to operators. The
	// moderation API keys are not needed by any outbox side effect, so never
	// serialize them into JSONB.
	payload.Config = safeContentModerationConfigForOutbox(payload.Config)
	raw, err := json.Marshal(payload)
	if err != nil {
		return map[string]any{}
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	return out
}

func contentModerationOutboxPayloadFromMap(payload map[string]any) (contentModerationOutboxPayload, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return contentModerationOutboxPayload{}, err
	}
	var out contentModerationOutboxPayload
	if err := json.Unmarshal(raw, &out); err != nil {
		return contentModerationOutboxPayload{}, err
	}
	return out, nil
}

func (s *ContentModerationService) outboxWorker(runtimeCtx context.Context, pollInterval time.Duration) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-runtimeCtx.Done():
			return
		case <-ticker.C:
			if err := s.ProcessContentModerationOutboxOnce(runtimeCtx, contentModerationOutboxDefaultClaimLimit); err != nil && runtimeCtx.Err() == nil {
				slog.Warn("content_moderation.outbox_worker_failed", "error", err)
			}
		}
	}
}

func (s *ContentModerationService) ProcessContentModerationOutboxOnce(ctx context.Context, limit int) error {
	outboxRepo := s.contentModerationOutboxRepository()
	if outboxRepo == nil {
		return nil
	}
	if limit <= 0 {
		limit = contentModerationOutboxDefaultClaimLimit
	}
	for processed := 0; processed < limit; processed++ {
		if ctx.Err() != nil {
			break
		}
		events, err := outboxRepo.ClaimDueEvents(ctx, time.Now(), 1, contentModerationOutboxLockDuration)
		if err != nil {
			return err
		}
		if len(events) == 0 {
			break
		}
		event := events[0]
		eventCtx, eventCancel := context.WithTimeout(ctx, contentModerationOutboxEventTimeout)
		processErr := s.processContentModerationOutboxEvent(eventCtx, outboxRepo, event)
		if processErr != nil {
			s.handleContentModerationOutboxEventFailure(eventCtx, outboxRepo, event, processErr)
			eventCancel()
			if ctx.Err() != nil {
				break
			}
			continue
		}
		stateCtx, stateCancel := contentModerationDetachedContext(eventCtx, contentModerationPersistenceTimeout)
		if err := outboxRepo.MarkEventSucceeded(stateCtx, event.ID, event.LeaseUntil); err != nil {
			slog.Warn("content_moderation.outbox_mark_succeeded_failed", "event_id", event.ID, "error", err)
		} else if event.EventType == ContentModerationOutboxEventSemanticReview {
			s.asyncProcessed.Add(1)
		}
		stateCancel()
		eventCancel()
		if ctx.Err() != nil {
			break
		}
	}
	return nil
}

func (s *ContentModerationService) contentModerationOutboxStatus(ctx context.Context) ContentModerationOutboxStatus {
	outboxRepo := s.contentModerationOutboxRepository()
	if outboxRepo == nil {
		return ContentModerationOutboxStatus{Enabled: false, Healthy: false}
	}
	status, err := outboxRepo.GetStatus(ctx, time.Now())
	if err != nil {
		slog.Warn("content_moderation.outbox_status_failed", "error", err)
		return ContentModerationOutboxStatus{Enabled: true, Healthy: false, LastError: err.Error()}
	}
	if status == nil {
		return ContentModerationOutboxStatus{Enabled: true, Healthy: false}
	}
	status.Enabled = true
	status.LastCleanupDeleted = s.lastOutboxCleanupDeleted.Load()
	return *status
}

func (s *ContentModerationService) ListContentModerationOutboxDeadLetters(ctx context.Context, limit int) ([]ContentModerationOutboxEvent, error) {
	outboxRepo := s.contentModerationOutboxRepository()
	if outboxRepo == nil {
		return []ContentModerationOutboxEvent{}, nil
	}
	return outboxRepo.ListDeadLetters(ctx, limit)
}

func (s *ContentModerationService) ReplayContentModerationOutboxDeadLetter(ctx context.Context, id int64) (bool, error) {
	outboxRepo := s.contentModerationOutboxRepository()
	if outboxRepo == nil || id <= 0 {
		return false, nil
	}
	return outboxRepo.ReplayDeadLetter(ctx, id)
}

func (s *ContentModerationService) cleanupContentModerationOutbox(ctx context.Context, now time.Time) {
	outboxRepo := s.contentModerationOutboxRepository()
	if outboxRepo == nil {
		return
	}
	deleted, err := outboxRepo.Cleanup(
		ctx,
		now.AddDate(0, 0, -contentModerationOutboxSucceededKeepDays),
		now.AddDate(0, 0, -contentModerationOutboxDeadLetterKeepDays),
		contentModerationOutboxCleanupBatch,
	)
	if err != nil {
		slog.Warn("content_moderation.outbox_cleanup_failed", "error", err)
		return
	}
	s.lastOutboxCleanupDeleted.Store(deleted)
}

func (s *ContentModerationService) CleanupContentModerationOutbox(ctx context.Context) (int64, error) {
	outboxRepo := s.contentModerationOutboxRepository()
	if outboxRepo == nil {
		return 0, nil
	}
	now := time.Now()
	deleted, err := outboxRepo.Cleanup(
		ctx,
		now.AddDate(0, 0, -contentModerationOutboxSucceededKeepDays),
		now.AddDate(0, 0, -contentModerationOutboxDeadLetterKeepDays),
		contentModerationOutboxCleanupBatch,
	)
	if err != nil {
		return 0, err
	}
	s.lastOutboxCleanupDeleted.Store(deleted)
	return deleted, nil
}

func (s *ContentModerationService) handleContentModerationOutboxEventFailure(ctx context.Context, outboxRepo ContentModerationOutboxRepository, event ContentModerationOutboxEvent, err error) {
	if s == nil || outboxRepo == nil {
		return
	}
	stateCtx, stateCancel := contentModerationDetachedContext(ctx, contentModerationPersistenceTimeout)
	defer stateCancel()
	nextRetry := event.RetryCount + 1
	failureText := contentModerationOutboxFailureText(event, err)
	backoff := time.Duration(nextRetry*nextRetry) * time.Second
	if event.Priority == ContentModerationOutboxPriorityWeak && backoff > time.Minute {
		backoff = time.Minute
	}
	if event.Priority == ContentModerationOutboxPriorityStrong && backoff > 10*time.Minute {
		backoff = 10 * time.Minute
	}
	if nextRetry >= event.MaxRetries {
		if markErr := outboxRepo.MarkEventDeadLetter(stateCtx, event.ID, event.LeaseUntil, failureText); markErr != nil {
			slog.Warn("content_moderation.outbox_dead_letter_failed", "event_id", event.ID, "error", markErr)
			return
		}
		if event.EventType == ContentModerationOutboxEventSemanticReview {
			if auditErr := s.persistSemanticReviewDeadLetterLog(stateCtx, event, errors.New(failureText)); auditErr != nil {
				replayed, replayErr := outboxRepo.ReplayDeadLetter(stateCtx, event.ID)
				if replayErr != nil || !replayed {
					slog.Warn("content_moderation.outbox_dead_letter_audit_replay_failed",
						"event_id", event.ID,
						"audit_error", auditErr,
						"replay_error", replayErr,
						"replayed", replayed,
					)
				} else {
					slog.Warn("content_moderation.outbox_dead_letter_audit_retry_scheduled", "event_id", event.ID, "error", auditErr)
				}
				return
			}
			s.asyncErrors.Add(1)
			s.asyncDropped.Add(1)
		}
		return
	}
	if retryErr := outboxRepo.ScheduleEventRetry(stateCtx, event.ID, event.LeaseUntil, nextRetry, time.Now().Add(backoff), failureText); retryErr != nil {
		slog.Warn("content_moderation.outbox_retry_schedule_failed", "event_id", event.ID, "error", retryErr)
	}
}

func contentModerationOutboxFailureText(event ContentModerationOutboxEvent, err error) string {
	failureText := "content moderation outbox event failed"
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		failureText = err.Error()
	}
	for _, marker := range []string{
		contentModerationOutboxAutoBanRecovery,
		contentModerationOutboxDeliveryRecoveryMarker("email_violation"),
		contentModerationOutboxDeliveryRecoveryMarker("email_account_disabled"),
		contentModerationOutboxDeliveryRecoveryMarker("admin_alert"),
	} {
		if strings.Contains(event.LastError, marker) && !strings.Contains(failureText, marker) {
			return marker + ": " + failureText
		}
	}
	return failureText
}

func (s *ContentModerationService) processContentModerationOutboxEvent(ctx context.Context, outboxRepo ContentModerationOutboxRepository, event ContentModerationOutboxEvent) error {
	payload, err := contentModerationOutboxPayloadFromMap(event.Payload)
	if err != nil {
		return err
	}
	payload, err = s.decryptContentModerationOutboxPayload(payload)
	if err != nil {
		return err
	}
	switch event.EventType {
	case ContentModerationOutboxEventLogWrite:
		return s.ensureContentModerationOutboxLog(ctx, payload)
	case ContentModerationOutboxEventHashBlock:
		if err := s.ensureContentModerationOutboxLog(ctx, payload); err != nil {
			return err
		}
		if !contentModerationOutboxEnforcementEligible(payload.Log) {
			return nil
		}
		if s.hashCache == nil || strings.TrimSpace(payload.InputHash) == "" {
			return nil
		}
		return s.hashCache.RecordFlaggedInputHash(ctx, payload.InputHash)
	case ContentModerationOutboxEventViolationCount:
		if err := s.ensureContentModerationOutboxLog(ctx, payload); err != nil {
			return err
		}
		if !contentModerationOutboxEnforcementEligible(payload.Log) {
			return nil
		}
		count, err := s.contentModerationViolationCount(ctx, payload.Config, payload.Log)
		if err != nil {
			return err
		}
		if s.repo != nil && payload.Log != nil {
			return s.repo.UpdateLogViolationCountByDecisionID(ctx, payload.Log.DecisionID, count)
		}
		return nil
	case ContentModerationOutboxEventUserAutoBan:
		if err := s.ensureContentModerationOutboxLog(ctx, payload); err != nil {
			return err
		}
		if !contentModerationOutboxEnforcementEligible(payload.Log) {
			return nil
		}
		autoBanRecovery := strings.Contains(event.LastError, contentModerationOutboxAutoBanRecovery)
		autoBanPreviouslyRecorded := false
		stateRepo, ok := s.repo.(contentModerationAccountActionStateRepository)
		if !ok {
			return errors.New("content moderation account action recovery repository is unavailable")
		}
		if payload.Log != nil {
			autoBanPreviouslyRecorded, err = stateRepo.GetLogAutoBannedByDecisionID(ctx, payload.Log.DecisionID)
			if err != nil {
				return err
			}
		}
		if autoBanPreviouslyRecorded || autoBanRecovery {
			payload.Log.AutoBanned = true
		}
		_, count, err := s.applyContentModerationAutoBan(ctx, payload.Config, payload.Log)
		if err != nil {
			return err
		}
		if s.repo != nil && payload.Log != nil {
			if err := s.repo.UpdateLogAccountActionByDecisionID(ctx, payload.Log.DecisionID, count, payload.Log.AutoBanned); err != nil {
				if payload.Log.AutoBanned {
					return fmt.Errorf("%s: %w", contentModerationOutboxAutoBanRecovery, err)
				}
				return err
			}
		}
		if payload.Log.AutoBanned &&
			payload.Config != nil && payload.Log != nil && strings.TrimSpace(payload.Log.UserEmail) != "" {
			emailPayload := payload
			emailPayload.EmailKind = "account_disabled"
			if strings.TrimSpace(emailPayload.RecordEncrypted) != "" {
				emailPayload.Log = nil
				emailPayload.Config = nil
			}
			if _, err := outboxRepo.EnqueueEvents(ctx, []ContentModerationOutboxEvent{
				newContentModerationOutboxEvent(payload.Log.DecisionID, ContentModerationOutboxEventEmail, "account_disabled", ContentModerationOutboxPriorityWeak, emailPayload),
			}); err != nil {
				return err
			}
		}
		return nil
	case ContentModerationOutboxEventEmail:
		if err := s.ensureContentModerationOutboxLog(ctx, payload); err != nil {
			return err
		}
		if !contentModerationOutboxEnforcementEligible(payload.Log) {
			return nil
		}
		kind := contentModerationOutboxNotificationKind(event.EventType, payload.EmailKind)
		delivered, recovering, deliveryRepo, err := s.contentModerationOutboxNotificationDeliveryState(ctx, event, payload, kind)
		if err != nil {
			return err
		}
		if delivered {
			return nil
		}
		if recovering {
			return s.markContentModerationOutboxNotificationDelivered(ctx, deliveryRepo, payload, kind, true)
		}
		sent, err := s.sendContentModerationOutboxEmail(ctx, payload)
		if err != nil {
			return err
		}
		if sent {
			return s.markContentModerationOutboxNotificationDelivered(ctx, deliveryRepo, payload, kind, true)
		}
		return nil
	case ContentModerationOutboxEventAdminAlert:
		if err := s.ensureContentModerationOutboxLog(ctx, payload); err != nil {
			return err
		}
		if !contentModerationOutboxEnforcementEligible(payload.Log) {
			return nil
		}
		kind := contentModerationOutboxNotificationKind(event.EventType, payload.EmailKind)
		delivered, recovering, deliveryRepo, err := s.contentModerationOutboxNotificationDeliveryState(ctx, event, payload, kind)
		if err != nil {
			return err
		}
		if delivered {
			return nil
		}
		if recovering {
			return s.markContentModerationOutboxNotificationDelivered(ctx, deliveryRepo, payload, kind, false)
		}
		sent, err := s.sendContentModerationAdminAlert(ctx, payload)
		if err != nil {
			return err
		}
		if sent {
			return s.markContentModerationOutboxNotificationDelivered(ctx, deliveryRepo, payload, kind, false)
		}
		return nil
	case ContentModerationOutboxEventSemanticReview:
		return s.processContentModerationSemanticReviewEvent(ctx, payload)
	default:
		return fmt.Errorf("unknown content moderation outbox event type %q", event.EventType)
	}
}

func (s *ContentModerationService) decryptContentModerationOutboxPayload(payload contentModerationOutboxPayload) (contentModerationOutboxPayload, error) {
	if strings.TrimSpace(payload.RecordEncrypted) == "" {
		return payload, nil
	}
	if s == nil || s.rawRequestEncryptor == nil {
		return contentModerationOutboxPayload{}, errors.New("content moderation outbox decryptor is unavailable")
	}
	plain, err := s.rawRequestEncryptor.Decrypt(payload.RecordEncrypted)
	if err != nil {
		return contentModerationOutboxPayload{}, fmt.Errorf("decrypt content moderation outbox record: %w", err)
	}
	raw, err := decodeContentModerationOutboxRecord(plain, payload.RecordEncoding)
	if err != nil {
		return contentModerationOutboxPayload{}, err
	}
	var record contentModerationOutboxRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return contentModerationOutboxPayload{}, fmt.Errorf("decode content moderation outbox record: %w", err)
	}
	payload.Log = record.Log
	payload.Config = record.Config
	if strings.TrimSpace(record.InputHash) != "" {
		payload.InputHash = record.InputHash
	}
	return payload, nil
}

func (s *ContentModerationService) ensureContentModerationOutboxLog(ctx context.Context, payload contentModerationOutboxPayload) error {
	if s == nil {
		return errors.New("content moderation service is unavailable")
	}
	if s.repo == nil {
		return errors.New("content moderation repository is unavailable")
	}
	if payload.Log == nil {
		return errors.New("content moderation outbox log is missing")
	}
	return s.repo.CreateLog(ctx, payload.Log)
}

func (s *ContentModerationService) contentModerationViolationCount(ctx context.Context, cfg *ContentModerationConfig, log *ContentModerationLog) (int, error) {
	if s == nil || cfg == nil || log == nil || !log.Flagged || log.UserID == nil || *log.UserID <= 0 {
		return 0, nil
	}
	if s.repo == nil || cfg.ViolationWindowHours <= 0 {
		return 1, nil
	}
	since := time.Now().Add(-time.Duration(cfg.ViolationWindowHours) * time.Hour)
	count, err := s.repo.CountFlaggedByUserSince(ctx, *log.UserID, since, cfg.CyberPolicyExcludeFromBanCount)
	if err != nil {
		return 0, err
	}
	if count <= 0 {
		return 1, nil
	}
	return count, nil
}

func (s *ContentModerationService) applyContentModerationAutoBan(ctx context.Context, cfg *ContentModerationConfig, log *ContentModerationLog) (bool, int, error) {
	count, err := s.contentModerationViolationCount(ctx, cfg, log)
	if err != nil {
		return false, 0, err
	}
	if s == nil || cfg == nil || log == nil || !log.Flagged || log.UserID == nil || *log.UserID <= 0 {
		return false, count, nil
	}
	log.ViolationCount = count
	if !cfg.AutoBanEnabled || cfg.BanThreshold <= 0 || count < cfg.BanThreshold || s.userRepo == nil {
		return false, count, nil
	}
	user, err := s.userRepo.GetByID(ctx, *log.UserID)
	if err != nil {
		return false, count, err
	}
	if user.IsAdmin() {
		return false, count, nil
	}
	if user.Status == StatusDisabled {
		return false, count, nil
	}
	user.Status = StatusDisabled
	if err := s.userRepo.Update(ctx, user, UserUpdateFields{Status: true}); err != nil {
		return false, count, err
	}
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, *log.UserID)
	}
	log.AutoBanned = true
	return true, count, nil
}

func (s *ContentModerationService) sendContentModerationOutboxEmail(ctx context.Context, payload contentModerationOutboxPayload) (bool, error) {
	if s == nil || s.emailService == nil || payload.Log == nil || payload.Config == nil || strings.TrimSpace(payload.Log.UserEmail) == "" {
		return false, nil
	}
	switch payload.EmailKind {
	case "account_disabled":
		if err := s.sendAccountDisabledEmail(ctx, payload.Config, payload.Log); err != nil {
			if notificationEmailDeliveryWasAccepted(err) {
				return true, nil
			}
			return false, err
		}
		return true, nil
	default:
		if err := s.sendViolationEmail(ctx, payload.Config, payload.Log); err != nil {
			if notificationEmailDeliveryWasAccepted(err) {
				return true, nil
			}
			return false, err
		}
		return true, nil
	}
}

func contentModerationOutboxNotificationKind(eventType, emailKind string) string {
	switch eventType {
	case ContentModerationOutboxEventEmail:
		if strings.EqualFold(strings.TrimSpace(emailKind), "account_disabled") {
			return "email_account_disabled"
		}
		return "email_violation"
	case ContentModerationOutboxEventAdminAlert:
		return "admin_alert"
	default:
		return ""
	}
}

func contentModerationOutboxDeliveryRecoveryMarker(kind string) string {
	return contentModerationOutboxDeliveryRecovery + kind
}

func (s *ContentModerationService) contentModerationOutboxNotificationDeliveryState(
	ctx context.Context,
	event ContentModerationOutboxEvent,
	payload contentModerationOutboxPayload,
	kind string,
) (bool, bool, contentModerationNotificationDeliveryStateRepository, error) {
	if strings.TrimSpace(kind) == "" || payload.Log == nil {
		return false, false, nil, errors.New("content moderation notification delivery identity is missing")
	}
	deliveryRepo, ok := s.repo.(contentModerationNotificationDeliveryStateRepository)
	if !ok {
		return false, false, nil, errors.New("content moderation notification delivery repository is unavailable")
	}
	delivered, err := deliveryRepo.GetLogNotificationDeliveredByDecisionID(ctx, payload.Log.DecisionID, kind)
	if err != nil {
		return false, false, deliveryRepo, err
	}
	recovering := strings.Contains(event.LastError, contentModerationOutboxDeliveryRecoveryMarker(kind))
	return delivered, recovering, deliveryRepo, nil
}

func (s *ContentModerationService) markContentModerationOutboxNotificationDelivered(
	ctx context.Context,
	deliveryRepo contentModerationNotificationDeliveryStateRepository,
	payload contentModerationOutboxPayload,
	kind string,
	emailSent bool,
) error {
	if payload.Log == nil {
		return errors.New("content moderation outbox log is missing")
	}
	if deliveryRepo != nil {
		if err := deliveryRepo.MarkLogNotificationDeliveredByDecisionID(ctx, payload.Log.DecisionID, kind, emailSent); err != nil {
			return fmt.Errorf("%s: %w", contentModerationOutboxDeliveryRecoveryMarker(kind), err)
		}
		return nil
	}
	if emailSent && s.repo != nil {
		if err := s.repo.UpdateLogEmailSentByDecisionID(ctx, payload.Log.DecisionID, true); err != nil {
			return fmt.Errorf("%s: %w", contentModerationOutboxDeliveryRecoveryMarker(kind), err)
		}
	}
	return nil
}

func (s *ContentModerationService) sendContentModerationAdminAlert(ctx context.Context, payload contentModerationOutboxPayload) (bool, error) {
	if s == nil || s.emailService == nil || s.userRepo == nil || payload.Log == nil {
		return false, nil
	}
	admin, err := s.userRepo.GetFirstAdmin(ctx)
	if err != nil {
		return false, err
	}
	if admin == nil || strings.TrimSpace(admin.Email) == "" {
		return false, nil
	}
	subject := "Content moderation alert"
	body := fmt.Sprintf("Content moderation %s on %s for user %s, action=%s, category=%s, score=%.4f",
		payload.Log.DecisionID,
		payload.Log.Endpoint,
		payload.Log.UserEmail,
		payload.Log.Action,
		payload.Log.HighestCategory,
		payload.Log.HighestScore)
	if err := s.emailService.SendEmail(ctx, admin.Email, subject, body); err != nil {
		return false, err
	}
	return true, nil
}

func cloneContentModerationLog(log *ContentModerationLog) *ContentModerationLog {
	if log == nil {
		return nil
	}
	out := *log
	out.UserID = cloneInt64Ptr(log.UserID)
	out.APIKeyID = cloneInt64Ptr(log.APIKeyID)
	out.GroupID = cloneInt64Ptr(log.GroupID)
	out.CategoryScores = cloneFloatMap(log.CategoryScores)
	out.ThresholdSnapshot = cloneFloatMap(log.ThresholdSnapshot)
	out.TruncateReasons = append([]string(nil), log.TruncateReasons...)
	if log.UpstreamLatencyMS != nil {
		v := *log.UpstreamLatencyMS
		out.UpstreamLatencyMS = &v
	}
	if log.QueueDelayMS != nil {
		v := *log.QueueDelayMS
		out.QueueDelayMS = &v
	}
	return &out
}
