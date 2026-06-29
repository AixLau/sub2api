package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIResponsesWebSocket_InitialFrameUsesModerationGuardBeforeForwarding(t *testing.T) {
	gin.SetMode(gin.TestMode)

	guard := &moderationGuardSpy{decision: &service.ContentModerationDecision{
		Blocked:    true,
		StatusCode: http.StatusForbidden,
		Message:    "guard blocked responses ws",
		Action:     service.ContentModerationActionBlock,
	}}
	var userSlotAcquireCalls int32
	cache := &concurrencyCacheMock{
		acquireUserSlotFn: func(ctx context.Context, userID int64, maxConcurrency int, requestID string) (bool, error) {
			atomic.AddInt32(&userSlotAcquireCalls, 1)
			return false, nil
		},
	}
	h := &OpenAIGatewayHandler{
		moderationGuard:          guard,
		contentModerationService: newDisabledContentModerationServiceForHandlerTest(t),
		gatewayService:           &service.OpenAIGatewayService{},
		billingCacheService:      &service.BillingCacheService{},
		apiKeyService:            &service.APIKeyService{},
		concurrencyHelper:        NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, time.Second),
		maxAccountSwitches:       1,
	}
	wsServer := newOpenAIWSHandlerTestServer(t, h, middleware.AuthSubject{UserID: 1, Concurrency: 1})
	defer wsServer.Close()

	dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
	clientConn, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(wsServer.URL, "http")+"/openai/v1/responses", nil)
	cancelDial()
	require.NoError(t, err)
	defer func() {
		_ = clientConn.CloseNow()
	}()

	firstPayload := `{"type":"response.create","model":"gpt-5.7","stream":false,"input":"guard-risk"}`
	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	err = clientConn.Write(writeCtx, coderws.MessageText, []byte(firstPayload))
	cancelWrite()
	require.NoError(t, err)

	readCtx, cancelRead := context.WithTimeout(context.Background(), 3*time.Second)
	_, payload, readErr := clientConn.Read(readCtx)
	cancelRead()
	if readErr == nil {
		require.Contains(t, string(payload), "content_policy_violation")
		require.Contains(t, string(payload), "guard blocked responses ws")

		readCtx, cancelRead = context.WithTimeout(context.Background(), 3*time.Second)
		_, _, readErr = clientConn.Read(readCtx)
		cancelRead()
	}
	require.Error(t, readErr)
	var closeErr coderws.CloseError
	require.True(t, errors.As(readErr, &closeErr), "expected websocket close error, got %T: %v", readErr, readErr)
	require.Equal(t, coderws.StatusPolicyViolation, closeErr.Code)
	require.Contains(t, closeErr.Reason, "guard blocked responses ws")

	require.Len(t, guard.calls, 1)
	require.Equal(t, service.ContentModerationProtocolOpenAIResponses, guard.calls[0].Protocol)
	require.Equal(t, "gpt-5.7", guard.calls[0].Model)
	require.Equal(t, []byte(firstPayload), guard.calls[0].Body)
	require.Zero(t, atomic.LoadInt32(&userSlotAcquireCalls), "blocked initial moderation must stop before account selection or forwarding")
}
