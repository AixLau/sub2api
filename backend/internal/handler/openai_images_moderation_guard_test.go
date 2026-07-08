package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/moderationcoverage"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

func TestOpenAIImages_UsesModerationGuardBeforeImageSlotAndForward(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"model":"gpt-image-2","prompt":"guard-risk","response_format":"b64_json"}`

	parseRec := httptest.NewRecorder()
	parseCtx, _ := gin.CreateTestContext(parseRec)
	parseReq := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(body))
	parseReq.Header.Set("Content-Type", "application/json")
	parseCtx.Request = parseReq
	parsed, err := (&service.OpenAIGatewayService{}).ParseOpenAIImagesRequest(parseCtx, []byte(body))
	require.NoError(t, err)
	expectedModerationBody := parsed.ModerationBody()
	require.NotEmpty(t, expectedModerationBody)

	guard := &openAIImagesModerationGuardSpy{decision: &service.ContentModerationDecision{
		Blocked:    true,
		StatusCode: http.StatusForbidden,
		Message:    "guard blocked images",
		Action:     service.ContentModerationActionBlock,
	}}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	setOpenAIImagesModerationGuardAuthContext(c)
	_, ok := middleware.GetAPIKeyFromContext(c)
	require.True(t, ok)

	h := &OpenAIGatewayHandler{
		moderationGuard:          guard,
		gatewayService:           &service.OpenAIGatewayService{},
		billingCacheService:      &service.BillingCacheService{},
		apiKeyService:            &service.APIKeyService{},
		contentModerationService: nil,
		concurrencyHelper:        &ConcurrencyHelper{concurrencyService: &service.ConcurrencyService{}},
		cfg: &config.Config{Gateway: config.GatewayConfig{ImageConcurrency: config.ImageConcurrencyConfig{
			Enabled:               true,
			MaxConcurrentRequests: 1,
			OverflowMode:          config.ImageConcurrencyOverflowModeReject,
		}}},
		imageLimiter: &imageConcurrencyLimiter{},
	}
	release, acquired := h.imageLimiter.Acquire(c.Request.Context(), true, 1, false, 0, 0)
	require.True(t, acquired)
	require.NotNil(t, release)
	defer release()

	result := h.EnterOpenAIHTTPGatewayPipeline(c, openAIImagesHTTPRouteMetaForTest())

	require.True(t, result.Stop)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Equal(t, "content_policy_violation", gjson.GetBytes(rec.Body.Bytes(), "error.type").String())
	require.Contains(t, rec.Body.String(), "guard blocked images")
	require.NotContains(t, rec.Body.String(), "Image generation concurrency limit exceeded")
	gotMessage, ok := c.Get(service.OpsDiagnosticMessageKey)
	require.True(t, ok)
	require.Contains(t, gotMessage, "内容审计拦截")
	gotDetail, ok := c.Get(service.OpsDiagnosticDetailKey)
	require.True(t, ok)
	require.Equal(t, "content_moderation", gjson.Get(gotDetail.(string), "source").String())
	require.Equal(t, service.ContentModerationActionBlock, gjson.Get(gotDetail.(string), "action").String())
	require.Equal(t, "guard blocked images", gjson.Get(gotDetail.(string), "client_message").String())
	require.Len(t, guard.calls, 1)
	require.Equal(t, service.ContentModerationProtocolOpenAIImages, guard.calls[0].Protocol)
	require.Equal(t, parsed.Model, guard.calls[0].Model)
	require.JSONEq(t, string(expectedModerationBody), string(guard.calls[0].Body))
	require.NotEqual(t, body, string(guard.calls[0].Body))
}

func openAIImagesHTTPRouteMetaForTest() moderationcoverage.Entry {
	return moderationcoverage.Entry{
		Method:             http.MethodPost,
		Path:               "/v1/images/generations",
		Handler:            "OpenAIGatewayHandler.Images",
		Upstream:           true,
		ModerationRequired: true,
		Protocol:           service.ContentModerationProtocolOpenAIImages,
		Pipeline:           moderationcoverage.PipelineOpenAIHTTP,
	}
}

type openAIImagesModerationGuardSpy struct {
	decision *service.ContentModerationDecision
	calls    []moderationGuardInput
}

func (s *openAIImagesModerationGuardSpy) Check(c *gin.Context, reqLog *zap.Logger, input moderationGuardInput) *service.ContentModerationDecision {
	s.calls = append(s.calls, input)
	return s.decision
}

func setOpenAIImagesModerationGuardAuthContext(c *gin.Context) {
	groupID := int64(2)
	apiKey := &service.APIKey{
		ID:      101,
		Name:    "test-key",
		GroupID: &groupID,
		User:    &service.User{ID: 7, Email: "user@example.com", Concurrency: 1},
		Group: &service.Group{
			ID:                   groupID,
			Name:                 "public",
			Platform:             service.PlatformOpenAI,
			AllowImageGeneration: true,
		},
	}
	c.Set(string(middleware.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7, Concurrency: 1})
	c.Set(string(middleware.ContextKeyUserRole), service.RoleUser)
}
