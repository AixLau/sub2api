package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

func TestOpenAIResponsesWebSocket_SubsequentFrameUsesModerationGuardAndBlocksBeforeForward(t *testing.T) {
	gin.SetMode(gin.TestMode)

	firstFrameCh := make(chan []byte, 1)
	secondFrameCh := make(chan []byte, 1)
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{CompressionMode: coderws.CompressionContextTakeover})
		if err != nil {
			return
		}
		defer func() {
			_ = conn.CloseNow()
		}()

		readCtx, cancelRead := context.WithTimeout(r.Context(), 3*time.Second)
		_, firstPayload, readErr := conn.Read(readCtx)
		cancelRead()
		if readErr == nil {
			firstFrameCh <- firstPayload
		}

		writeCtx, cancelWrite := context.WithTimeout(r.Context(), 3*time.Second)
		_ = conn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.created","response":{"id":"resp_ws_guard_first","model":"gpt-5.1"}}`))
		cancelWrite()
		writeCtx, cancelWrite = context.WithTimeout(r.Context(), 3*time.Second)
		_ = conn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.completed","response":{"id":"resp_ws_guard_first","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`))
		cancelWrite()

		readCtx, cancelRead = context.WithTimeout(r.Context(), 500*time.Millisecond)
		_, secondPayload, readErr := conn.Read(readCtx)
		cancelRead()
		if readErr == nil {
			secondFrameCh <- secondPayload
		}
	}))
	defer upstreamServer.Close()

	guard := &openAIResponsesWSFrameModerationGuardSpy{decisions: []*service.ContentModerationDecision{
		nil,
		{
			Blocked:    true,
			StatusCode: http.StatusForbidden,
			Message:    "guard blocked follow-up frame",
			Action:     service.ContentModerationActionBlock,
		},
	}}
	groupID := int64(4203)
	cfg := newOpenAIResponsesWSFrameModerationGuardTestConfig()
	accountRepo := &openAIWSUsageHandlerAccountRepoStub{account: service.Account{
		ID:          9904,
		Name:        "openai-ws-frame-moderation",
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeAPIKey,
		Status:      service.StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": upstreamServer.URL,
		},
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
			"openai_apikey_responses_websockets_v2_mode":    service.OpenAIWSIngressModePassthrough,
		},
	}}
	billingCacheSvc := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	gatewaySvc := service.NewOpenAIGatewayService(
		accountRepo,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		cfg,
		nil,
		nil,
		service.NewBillingService(cfg, nil),
		nil,
		billingCacheSvc,
		nil,
		&service.DeferredService{},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	cache := &concurrencyCacheMock{
		acquireUserSlotFn: func(ctx context.Context, userID int64, maxConcurrency int, requestID string) (bool, error) {
			return true, nil
		},
		acquireAccountSlotFn: func(ctx context.Context, accountID int64, maxConcurrency int, requestID string) (bool, error) {
			return true, nil
		},
	}
	h := &OpenAIGatewayHandler{
		moderationGuard:          guard,
		contentModerationService: newDisabledContentModerationServiceForHandlerTest(t),
		gatewayService:           gatewaySvc,
		billingCacheService:      billingCacheSvc,
		apiKeyService:            &service.APIKeyService{},
		concurrencyHelper:        NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, time.Second),
		maxAccountSwitches:       1,
	}

	apiKey := &service.APIKey{
		ID:      1803,
		GroupID: &groupID,
		User:    &service.User{ID: 1703, Status: service.StatusActive},
		Group:   &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Status: service.StatusActive},
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAPIKey), apiKey)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.User.ID, Concurrency: 1})
		c.Next()
	})
	router.GET("/openai/v1/responses", markOpenAIWebSocketGatewayPipelineEntrypointForTest(), h.ResponsesWebSocket)
	handlerServer := httptest.NewServer(router)
	defer handlerServer.Close()

	dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
	clientConn, _, err := coderws.Dial(
		dialCtx,
		"ws"+strings.TrimPrefix(handlerServer.URL, "http")+"/openai/v1/responses",
		&coderws.DialOptions{CompressionMode: coderws.CompressionContextTakeover},
	)
	cancelDial()
	require.NoError(t, err)
	defer func() {
		_ = clientConn.CloseNow()
	}()

	firstPayload := []byte(`{"type":"response.create","model":"gpt-5.1","stream":true}`)
	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	err = clientConn.Write(writeCtx, coderws.MessageText, firstPayload)
	cancelWrite()
	require.NoError(t, err)

	readCtx, cancelRead := context.WithTimeout(context.Background(), 3*time.Second)
	_, event, err := clientConn.Read(readCtx)
	cancelRead()
	require.NoError(t, err)
	require.Equal(t, "response.created", gjson.GetBytes(event, "type").String())

	readCtx, cancelRead = context.WithTimeout(context.Background(), 3*time.Second)
	_, event, err = clientConn.Read(readCtx)
	cancelRead()
	require.NoError(t, err)
	require.Equal(t, "response.completed", gjson.GetBytes(event, "type").String())

	followupPayload := []byte(`{"type":"response.create","model":"gpt-5.2","input":"guard-risk"}`)
	writeCtx, cancelWrite = context.WithTimeout(context.Background(), 3*time.Second)
	err = clientConn.Write(writeCtx, coderws.MessageText, followupPayload)
	cancelWrite()
	require.NoError(t, err)

	readCtx, cancelRead = context.WithTimeout(context.Background(), 3*time.Second)
	_, event, err = clientConn.Read(readCtx)
	cancelRead()
	require.NoError(t, err)
	require.Equal(t, "error", gjson.GetBytes(event, "type").String())
	require.Equal(t, "content_policy_violation", gjson.GetBytes(event, "error.code").String())
	require.Equal(t, "guard blocked follow-up frame", gjson.GetBytes(event, "error.message").String())

	_, _, err = clientConn.Read(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "guard blocked follow-up frame")

	require.Len(t, guard.calls, 2)
	require.Equal(t, service.ContentModerationProtocolOpenAIResponses, guard.calls[1].Protocol)
	require.Equal(t, "gpt-5.2", guard.calls[1].Model)
	require.Equal(t, followupPayload, guard.calls[1].Body)

	select {
	case got := <-firstFrameCh:
		require.JSONEq(t, string(firstPayload), string(got))
	case <-time.After(3 * time.Second):
		t.Fatal("waiting for upstream first frame timed out")
	}
	select {
	case got := <-secondFrameCh:
		t.Fatalf("blocked follow-up frame was forwarded upstream: %s", string(got))
	case <-time.After(700 * time.Millisecond):
	}
}

type openAIResponsesWSFrameModerationGuardSpy struct {
	decisions []*service.ContentModerationDecision
	calls     []moderationGuardInput
}

func (s *openAIResponsesWSFrameModerationGuardSpy) Check(c *gin.Context, reqLog *zap.Logger, input moderationGuardInput) *service.ContentModerationDecision {
	s.calls = append(s.calls, input)
	if len(s.decisions) == 0 {
		return nil
	}
	decision := s.decisions[0]
	s.decisions = s.decisions[1:]
	return decision
}

func newOpenAIResponsesWSFrameModerationGuardTestConfig() *config.Config {
	cfg := &config.Config{}
	cfg.RunMode = config.RunModeSimple
	cfg.Default.RateMultiplier = 1
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3
	return cfg
}
