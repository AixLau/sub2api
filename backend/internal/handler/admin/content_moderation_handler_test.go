package admin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type contentModerationHandlerReviewRepo struct {
	mu   sync.Mutex
	logs []service.ContentModerationLog
	raw  map[int64]service.ContentModerationRawRequestSnapshot
}

type contentModerationAdminAuditCapture struct {
	mu     sync.Mutex
	events []*logger.LogEvent
}

func (s *contentModerationAdminAuditCapture) WriteLogEvent(event *logger.LogEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
}

func (s *contentModerationAdminAuditCapture) snapshot() []*logger.LogEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*logger.LogEvent(nil), s.events...)
}

func (r *contentModerationHandlerReviewRepo) CreateLog(ctx context.Context, log *service.ContentModerationLog) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if log != nil {
		r.logs = append(r.logs, *log)
	}
	return nil
}

func (r *contentModerationHandlerReviewRepo) ListLogs(ctx context.Context, filter service.ContentModerationLogFilter) ([]service.ContentModerationLog, *pagination.PaginationResult, error) {
	return r.logs, &pagination.PaginationResult{Total: int64(len(r.logs)), Page: 1, PageSize: 20}, nil
}

func (r *contentModerationHandlerReviewRepo) CountFlaggedByUserSince(ctx context.Context, userID int64, since time.Time, excludeCyberPolicy bool) (int, error) {
	return 0, nil
}

func (r *contentModerationHandlerReviewRepo) CleanupExpiredLogs(ctx context.Context, hitBefore time.Time, nonHitBefore time.Time) (*service.ContentModerationCleanupResult, error) {
	return &service.ContentModerationCleanupResult{}, nil
}

func (r *contentModerationHandlerReviewRepo) UpdateLogEmailSent(ctx context.Context, id int64, sent bool) error {
	return nil
}

func (r *contentModerationHandlerReviewRepo) UpdateLogViolationCountByDecisionID(ctx context.Context, decisionID string, count int) error {
	return nil
}

func (r *contentModerationHandlerReviewRepo) UpdateLogAccountActionByDecisionID(ctx context.Context, decisionID string, violationCount int, autoBanned bool) error {
	return nil
}

func (r *contentModerationHandlerReviewRepo) UpdateLogEmailSentByDecisionID(ctx context.Context, decisionID string, sent bool) error {
	return nil
}

func (r *contentModerationHandlerReviewRepo) ReviewLog(ctx context.Context, id int64, input service.ContentModerationLogReviewInput) (*service.ContentModerationLog, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for idx := range r.logs {
		if r.logs[idx].ID != id {
			continue
		}
		r.logs[idx].ReviewStatus = input.Status
		r.logs[idx].ReviewNote = input.Note
		if input.ReviewedBy > 0 {
			reviewer := input.ReviewedBy
			r.logs[idx].ReviewedBy = &reviewer
		}
		now := time.Now()
		r.logs[idx].ReviewedAt = &now
		out := r.logs[idx]
		return &out, nil
	}
	return nil, infraerrors.NotFound("CONTENT_MODERATION_LOG_NOT_FOUND", "审核记录不存在")
}

func (r *contentModerationHandlerReviewRepo) CreateRawRequestSnapshot(ctx context.Context, snapshot *service.ContentModerationRawRequestSnapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.raw == nil {
		r.raw = map[int64]service.ContentModerationRawRequestSnapshot{}
	}
	cp := *snapshot
	r.raw[cp.LogID] = cp
	return nil
}

func (r *contentModerationHandlerReviewRepo) GetRawRequestSnapshotByLogID(ctx context.Context, logID int64) (*service.ContentModerationRawRequestSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snapshot, ok := r.raw[logID]
	if !ok {
		return nil, infraerrors.NotFound("CONTENT_MODERATION_RAW_REQUEST_NOT_FOUND", "原始请求快照不存在")
	}
	return &snapshot, nil
}

func TestContentModerationHandlerReviewLog(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auditSink := &contentModerationAdminAuditCapture{}
	logger.SetSink(auditSink)
	t.Cleanup(func() { logger.SetSink(nil) })
	repo := &contentModerationHandlerReviewRepo{logs: []service.ContentModerationLog{{
		ID:           42,
		Action:       service.ContentModerationActionKeywordReview,
		ReviewStatus: service.ContentModerationReviewStatusPending,
	}}}
	svc := service.NewContentModerationService(nil, repo, nil, nil, nil, nil, nil)
	h := NewContentModerationHandler(svc)
	router := gin.New()
	router.PATCH("/admin/risk-control/logs/:id/review", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
		h.ReviewLog(c)
	})
	body, err := json.Marshal(map[string]string{
		"status": service.ContentModerationReviewStatusFalsePositive,
		"note":   "Codex ambient safety prompt",
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/admin/risk-control/logs/42/review", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var payload struct {
		Data service.ContentModerationLog `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.Equal(t, service.ContentModerationReviewStatusFalsePositive, payload.Data.ReviewStatus)
	require.Equal(t, "Codex ambient safety prompt", payload.Data.ReviewNote)
	require.NotNil(t, payload.Data.ReviewedBy)
	require.Equal(t, int64(7), *payload.Data.ReviewedBy)
	require.NotNil(t, payload.Data.ReviewedAt)

	events := auditSink.snapshot()
	require.Len(t, events, 2)
	require.Equal(t, "attempt", events[0].Fields["result"])
	require.Equal(t, "success", events[1].Fields["result"])
	for _, event := range events {
		require.Equal(t, "risk_control.logs.review", event.Fields["action"])
		require.Equal(t, "42", event.Fields["target_id"])
		require.Equal(t, int64(7), event.Fields["operator_id"])
		require.NotContains(t, event.Fields, "note")
		after, ok := event.Fields["after"].(map[string]any)
		require.True(t, ok)
		require.Equal(t, true, after["review_note_supplied"])
		require.NotContains(t, after, "review_note")
		require.NotContains(t, after, "input_excerpt")
	}
}

func TestContentModerationHandlerReviewLogFailureEmitsFailedAudit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auditSink := &contentModerationAdminAuditCapture{}
	logger.SetSink(auditSink)
	t.Cleanup(func() { logger.SetSink(nil) })

	repo := &contentModerationHandlerReviewRepo{}
	h := NewContentModerationHandler(service.NewContentModerationService(nil, repo, nil, nil, nil, nil, nil))
	router := gin.New()
	router.PATCH("/admin/risk-control/logs/:id/review", h.ReviewLog)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/admin/risk-control/logs/404/review", strings.NewReader(`{"status":"confirmed_violation","note":"must not leak"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
	events := auditSink.snapshot()
	require.Len(t, events, 2)
	require.Equal(t, "attempt", events[0].Fields["result"])
	require.Equal(t, "failed", events[1].Fields["result"])
	require.Equal(t, "CONTENT_MODERATION_LOG_NOT_FOUND", events[1].Fields["error_code"])
	encoded, err := json.Marshal(events)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "must not leak")
}

func TestContentModerationConfigChangedFieldsOmitsValues(t *testing.T) {
	apiKey := "secret-key"
	keywords := []string{"sensitive prompt phrase"}
	enabled := true
	fields := contentModerationConfigChangedFields(contentModerationConfigRequest{
		Enabled:         &enabled,
		APIKey:          &apiKey,
		BlockedKeywords: &keywords,
	})

	require.ElementsMatch(t, []string{"enabled", "api_key", "blocked_keywords"}, fields)
	encoded, err := json.Marshal(fields)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), apiKey)
	require.NotContains(t, string(encoded), keywords[0])
}

func TestContentModerationHandlerGetRawRequestSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &contentModerationHandlerReviewRepo{raw: map[int64]service.ContentModerationRawRequestSnapshot{
		42: {
			LogID:         42,
			RequestID:     "req-raw",
			BodyEncrypted: "enc:" + base64.StdEncoding.EncodeToString([]byte(`{"model":"gpt-5","input":"full prompt"}`)),
			BodyBytes:     39,
			Truncated:     false,
			CreatedAt:     time.Now(),
		},
	}}
	svc := service.NewContentModerationService(nil, repo, nil, nil, nil, nil, nil)
	svc.SetRawRequestSnapshotStore(repo, contentModerationHandlerTestEncryptor{})
	h := NewContentModerationHandler(svc)
	router := gin.New()
	router.GET("/admin/risk-control/logs/:id/raw-request", h.GetRawRequestSnapshot)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/risk-control/logs/42/raw-request", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var payload struct {
		Data service.ContentModerationRawRequestView `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.Equal(t, int64(42), payload.Data.LogID)
	require.Equal(t, "req-raw", payload.Data.RequestID)
	require.Equal(t, `{"model":"gpt-5","input":"full prompt"}`, payload.Data.Body)
	require.False(t, payload.Data.Truncated)
}

type contentModerationHandlerTestEncryptor struct{}

func (contentModerationHandlerTestEncryptor) Encrypt(plaintext string) (string, error) {
	return "enc:" + base64.StdEncoding.EncodeToString([]byte(plaintext)), nil
}

func (contentModerationHandlerTestEncryptor) Decrypt(ciphertext string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(ciphertext, "enc:"))
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}
