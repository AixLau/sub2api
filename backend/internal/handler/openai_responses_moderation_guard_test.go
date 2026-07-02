package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIResponsesHTTP_UsesModerationGuardBeforeImagePermissionAndForwarding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	guard := &moderationGuardSpy{decision: &service.ContentModerationDecision{
		Blocked:    true,
		StatusCode: http.StatusForbidden,
		Message:    "guard blocked responses",
		Action:     service.ContentModerationActionBlock,
	}}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"model":"gpt-5.1","input":"guard-risk","tools":[{"type":"image_generation"}]}`
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	setGatewayAuthContextForModerationTest(c)

	h := &OpenAIGatewayHandler{
		moderationGuard:          guard,
		contentModerationService: newDisabledContentModerationServiceForHandlerTest(t),
		gatewayService:           &service.OpenAIGatewayService{},
		billingCacheService:      &service.BillingCacheService{},
		apiKeyService:            &service.APIKeyService{},
		concurrencyHelper:        NewConcurrencyHelper(service.NewConcurrencyService(&concurrencyCacheMock{}), SSEPingFormatNone, time.Second),
		errorPassthroughService:  nil,
		maxAccountSwitches:       1,
	}

	result := h.EnterOpenAIHTTPGatewayPipeline(c, openAIResponsesHTTPRouteMetaForTest())

	require.True(t, result.Stop)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), "guard blocked responses")
	require.NotContains(t, w.Body.String(), service.ImageGenerationPermissionMessage())
	require.Len(t, guard.calls, 1)
	require.Equal(t, service.ContentModerationProtocolOpenAIResponses, guard.calls[0].Protocol)
	require.Equal(t, "gpt-5.1", guard.calls[0].Model)
	require.Equal(t, []byte(body), guard.calls[0].Body)
}
