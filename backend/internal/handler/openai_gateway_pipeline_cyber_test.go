package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestOpenAIGatewayPipelineCheckCyberSessionBlocked(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-5.1","prompt_cache_key":"pipeline-session","input":"hello"}`)
	apiKey := &service.APIKey{ID: 7}
	c, w := newOpenAIGatewayPipelineCyberContext(http.MethodPost, "/v1/responses", body)
	blockKey := service.CyberSessionExplicitBlockKey(apiKey.ID, c, body)
	require.NotEmpty(t, blockKey)

	checker := &openAIGatewayPipelineCyberCheckerStub{
		enabled: true,
		blocked: map[string]bool{blockKey: true},
	}
	pipeline := newOpenAIGatewayPipeline(nil)
	pipeline.cyberSessionChecker = checker

	result := pipeline.CheckCyberSession(c, zap.NewNop(), openAIGatewayCyberSessionInput{
		APIKey: apiKey,
		Model:  "gpt-5.1",
		Body:   body,
		Format: cyberBlockFormatResponses,
	})

	require.NotNil(t, result)
	require.True(t, result.Blocked)
	require.Equal(t, blockKey, result.BlockKey)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), "session_blocked_by_cyber_policy")
	require.Contains(t, w.Body.String(), cyberSessionBlockedClientMessage(service.PlatformOpenAI))
	require.Equal(t, []string{blockKey}, checker.checkedKeys)
}

func TestOpenAIGatewayPipelineCheckCyberSessionAllowed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-5.1","prompt_cache_key":"pipeline-session","messages":[{"role":"user","content":"hello"}]}`)
	apiKey := &service.APIKey{ID: 7}
	c, w := newOpenAIGatewayPipelineCyberContext(http.MethodPost, "/v1/chat/completions", body)
	blockKey := service.CyberSessionExplicitBlockKey(apiKey.ID, c, body)
	require.NotEmpty(t, blockKey)

	checker := &openAIGatewayPipelineCyberCheckerStub{
		enabled: true,
		blocked: map[string]bool{blockKey: false},
	}
	pipeline := newOpenAIGatewayPipeline(nil)
	pipeline.cyberSessionChecker = checker

	result := pipeline.CheckCyberSession(c, zap.NewNop(), openAIGatewayCyberSessionInput{
		APIKey: apiKey,
		Model:  "gpt-5.1",
		Body:   body,
		Format: cyberBlockFormatChat,
	})

	require.NotNil(t, result)
	require.False(t, result.Blocked)
	require.Empty(t, result.BlockKey)
	require.Equal(t, http.StatusOK, w.Code)
	require.Empty(t, w.Body.String())
	require.Equal(t, []string{blockKey}, checker.checkedKeys)
}

func TestOpenAIGatewayPipelineCheckCyberSessionExplicitBlockKeyStable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-5.1","prompt_cache_key":"pipeline-session","input":"hello"}`)
	apiKey := &service.APIKey{ID: 7}
	c1, _ := newOpenAIGatewayPipelineCyberContext(http.MethodPost, "/v1/responses", body)
	c2, _ := newOpenAIGatewayPipelineCyberContext(http.MethodPost, "/v1/responses", body)
	blockKey := service.CyberSessionExplicitBlockKey(apiKey.ID, c1, body)
	otherBlockKey := service.CyberSessionExplicitBlockKey(8, c1, body)

	checker := &openAIGatewayPipelineCyberCheckerStub{
		enabled: true,
		blocked: map[string]bool{blockKey: true, otherBlockKey: true},
	}
	pipeline := newOpenAIGatewayPipeline(nil)
	pipeline.cyberSessionChecker = checker

	result1 := pipeline.CheckCyberSession(c1, zap.NewNop(), openAIGatewayCyberSessionInput{
		APIKey: apiKey,
		Model:  "gpt-5.1",
		Body:   body,
		Format: cyberBlockFormatResponses,
	})
	result2 := pipeline.CheckCyberSession(c2, zap.NewNop(), openAIGatewayCyberSessionInput{
		APIKey: apiKey,
		Model:  "gpt-5.1",
		Body:   body,
		Format: cyberBlockFormatResponses,
	})

	require.NotEmpty(t, result1.BlockKey)
	require.Equal(t, result1.BlockKey, result2.BlockKey)
	require.Equal(t, blockKey, result1.BlockKey)

	otherAPIKeyResult := pipeline.CheckCyberSession(c1, zap.NewNop(), openAIGatewayCyberSessionInput{
		APIKey: &service.APIKey{ID: 8},
		Model:  "gpt-5.1",
		Body:   body,
		Format: cyberBlockFormatResponses,
	})
	require.NotEqual(t, result1.BlockKey, otherAPIKeyResult.BlockKey)
}

func TestOpenAIGatewayPipelineCheckCyberSessionNilFallbacksDoNotPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-5.1","prompt_cache_key":"pipeline-session","input":"hello"}`)
	apiKey := &service.APIKey{ID: 7}
	c, w := newOpenAIGatewayPipelineCyberContext(http.MethodPost, "/v1/responses", body)

	var nilPipeline *OpenAIGatewayPipeline
	var result *OpenAICyberStageResult
	require.NotPanics(t, func() {
		result = nilPipeline.CheckCyberSession(c, zap.NewNop(), openAIGatewayCyberSessionInput{
			APIKey: apiKey,
			Model:  "gpt-5.1",
			Body:   body,
			Format: cyberBlockFormatResponses,
		})
	})
	require.NotNil(t, result)
	require.False(t, result.Blocked)
	require.Equal(t, http.StatusOK, w.Code)

	pipeline := newOpenAIGatewayPipeline(nil)
	require.NotPanics(t, func() {
		result = pipeline.CheckCyberSession(c, zap.NewNop(), openAIGatewayCyberSessionInput{
			APIKey: nil,
			Model:  "gpt-5.1",
			Body:   body,
			Format: cyberBlockFormatResponses,
		})
	})
	require.NotNil(t, result)
	require.False(t, result.Blocked)
}

func TestOpenAIGatewayHandlerCheckCyberSessionWithPipelineEnqueuesOpsEntry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetOpsErrorLoggerStateForTest(t)
	t.Cleanup(func() {
		resetOpsErrorLoggerStateForTest(t)
	})
	opsErrorLogOnce.Do(func() {})

	opsErrorLogMu.Lock()
	opsErrorLogQueue = make(chan opsErrorLogJob, 1)
	opsErrorLogMu.Unlock()

	body := []byte(`{"model":"gpt-5.1","prompt_cache_key":"pipeline-session","input":"hello"}`)
	apiKey := &service.APIKey{ID: 7, Name: "gateway-key", Key: "sk-test-cyber-pipeline"}
	c, w := newOpenAIGatewayPipelineCyberContext(http.MethodPost, "/v1/responses", body)
	blockKey := service.CyberSessionExplicitBlockKey(apiKey.ID, c, body)
	require.NotEmpty(t, blockKey)

	checker := &openAIGatewayPipelineCyberCheckerStub{
		enabled: true,
		blocked: map[string]bool{blockKey: true},
	}
	h := &OpenAIGatewayHandler{
		pipeline:   &OpenAIGatewayPipeline{cyberSessionChecker: checker},
		opsService: service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil),
	}

	blocked := h.checkCyberSessionWithPipeline(c, zap.NewNop(), openAIGatewayCyberSessionInput{
		APIKey:   apiKey,
		Protocol: service.ContentModerationProtocolOpenAIResponses,
		Model:    "gpt-5.1",
		Body:     body,
		Format:   cyberBlockFormatResponses,
	})

	require.True(t, blocked)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Equal(t, int64(1), OpsErrorLogEnqueuedTotal())

	opsErrorLogMu.RLock()
	queue := opsErrorLogQueue
	opsErrorLogMu.RUnlock()
	require.Len(t, queue, 1)
	job := <-queue
	require.NotNil(t, job.entry)
	require.Equal(t, "cyber_policy_session_blocked", job.entry.ErrorType)
	require.Equal(t, "session_block_key="+blockKey, job.entry.ErrorBody)
}

func newOpenAIGatewayPipelineCyberContext(method, target string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, target, strings.NewReader(string(body)))
	return c, w
}

type openAIGatewayPipelineCyberCheckerStub struct {
	enabled     bool
	blocked     map[string]bool
	checkedKeys []string
}

func (s *openAIGatewayPipelineCyberCheckerStub) FindCyberSessionBlockedForRequest(_ context.Context, apiKeyID int64, c *gin.Context, body []byte, _, _ string) string {
	key := service.CyberSessionExplicitBlockKey(apiKeyID, c, body)
	s.checkedKeys = append(s.checkedKeys, key)
	if s.enabled && s.blocked[key] {
		return key
	}
	return ""
}
