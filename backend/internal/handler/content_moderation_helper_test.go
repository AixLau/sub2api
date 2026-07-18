package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/moderationcoverage"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestContentModerationDeferredActionsUseRetryableNonViolationResponses(t *testing.T) {
	tests := []struct {
		action string
		code   string
	}{
		{service.ContentModerationActionSemanticReviewDeferred, "content_review_required"},
		{service.ContentModerationActionSemanticReviewUnavailable, "content_review_unavailable"},
		{service.ContentModerationActionSemanticReviewIncomplete, "content_review_incomplete"},
	}
	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			decision := &service.ContentModerationDecision{Blocked: true, Action: tt.action}
			require.True(t, contentModerationIsNonViolationDeferred(decision))
			require.Equal(t, http.StatusServiceUnavailable, contentModerationStatus(decision))
			require.Equal(t, tt.code, contentModerationErrorCode(decision))
		})
	}
}

func TestContentModerationReceiptUsesDecisionPolicyRevisionAndNeverAllowsNilDecision(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	markContentModerationReceipt(c, service.ContentModerationProtocolOpenAIResponses, "", &service.ContentModerationDecision{
		Allowed:        true,
		Action:         service.ContentModerationActionAllow,
		PolicyRevision: "policy-v2",
	}, false)
	receipt, ok := moderationcoverage.ModerationReceiptFromContext(c)
	require.True(t, ok)
	require.Equal(t, "policy-v2", receipt.PolicyRevision)
	require.True(t, receipt.ForwardAllowed)

	markContentModerationReceipt(c, service.ContentModerationProtocolOpenAIResponses, "", nil, false)
	receipt, ok = moderationcoverage.ModerationReceiptFromContext(c)
	require.True(t, ok)
	require.Equal(t, "error", receipt.Outcome)
	require.False(t, receipt.ForwardAllowed)
}

func TestContentModerationCheckErrorDecisionAllowsRequest(t *testing.T) {
	decision := contentModerationCheckErrorDecision()

	require.NotNil(t, decision)
	require.True(t, decision.Allowed)
	require.False(t, decision.Blocked)
	require.False(t, decision.Flagged)
	require.Zero(t, decision.StatusCode)
	require.Equal(t, service.ContentModerationActionError, decision.Action)
	require.Empty(t, decision.Message)
}

func TestOpenAIContentModerationNilServiceFailsOpen(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	decision := (&OpenAIGatewayHandler{}).checkContentModeration(
		c,
		zap.NewNop(),
		nil,
		middleware2.AuthSubject{UserID: 42},
		service.ContentModerationProtocolOpenAIChat,
		"gpt-test",
		[]byte(`{"messages":[{"role":"user","content":"hello"}]}`),
	)

	require.NotNil(t, decision)
	require.True(t, decision.Allowed)
	require.False(t, decision.Blocked)
	require.Zero(t, decision.StatusCode)
	require.Equal(t, service.ContentModerationActionError, decision.Action)
}
