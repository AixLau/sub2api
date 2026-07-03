package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type contentModerationHandlerReviewRepo struct {
	mu   sync.Mutex
	logs []service.ContentModerationLog
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

func TestContentModerationHandlerReviewLog(t *testing.T) {
	gin.SetMode(gin.TestMode)
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
}
