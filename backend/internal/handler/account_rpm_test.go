package handler

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type completedRPMCache struct {
	limitIDs []int64
	service.RPMCache
	ids         []int64
	err         error
	contextErr  error
	hasDeadline bool
}

func (c *completedRPMCache) IncrementRecentRPM(ctx context.Context, id int64) (int, error) {
	c.ids = append(c.ids, id)
	c.contextErr = ctx.Err()
	_, c.hasDeadline = ctx.Deadline()
	return len(c.ids), c.err
}

// A full billing queue proves that counting is synchronous and independent of
// asynchronously recording usage, including successful requests with zero tokens.
func saturatedRPMBillingPool(t *testing.T) *service.UsageRecordWorkerPool {
	t.Helper()
	pool := newUsageRecordTestPool(t)
	release := make(chan struct{})
	started := make(chan struct{})
	pool.Submit(func(context.Context) { close(started); <-release })
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	for i := 0; i < 8; i++ {
		pool.Submit(func(context.Context) {})
	}
	t.Cleanup(func() { close(release) })
	return pool
}

func TestAccountRPMCompletionStages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, platform := range []string{"anthropic", "gemini", "antigravity", "openai", "grok", "deepseek", "kimi", "zhipu", "minimax", "opencode_go", "typesafe"} {
		t.Run(platform, func(t *testing.T) {
			cache := &completedRPMCache{}
			pool := saturatedRPMBillingPool(t)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
			account := &service.Account{ID: 42, Platform: platform, Type: service.AccountTypeAPIKey}
			apiKey := &service.APIKey{ID: 1, User: &service.User{ID: 1}}
			if platform == "anthropic" || platform == "gemini" || platform == "antigravity" {
				h := &GatewayHandler{rpmCache: cache, usageRecordWorkerPool: pool}
				stage := GatewayUsageStage{Handler: h, Account: account, APIKey: apiKey, Result: &service.ForwardResult{}}
				stage.ForwardErrored = true
				stage.RunUsage(c)
				require.Empty(t, cache.ids)
				stage.ForwardErrored = false
				stage.Result.ClientDisconnect = true
				stage.RunUsage(c)
				require.Empty(t, cache.ids)
				stage.Result.ClientDisconnect = false
				stage.RunUsage(c)
			} else {
				h := &OpenAIGatewayHandler{rpmCache: cache, usageRecordWorkerPool: pool}
				stage := OpenAIHTTPUsageStage{Handler: h, Account: account, APIKey: apiKey, Result: &service.OpenAIForwardResult{}}
				stage.Source = service.UsageSourceFailedUpstream
				stage.RunUsage(c)
				require.Empty(t, cache.ids)
				stage.Source = service.UsageSourceGateway
				stage.ForwardErrored = true
				stage.RunUsage(c)
				require.Empty(t, cache.ids)
				stage.ForwardErrored = false
				stage.RunUsage(c)
			}
			require.Equal(t, []int64{42}, cache.ids)
			require.True(t, cache.hasDeadline)
		})
	}
}

func TestAccountRPMWebSocketTurns(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cache := &completedRPMCache{}
	pool := saturatedRPMBillingPool(t)
	h := &OpenAIGatewayHandler{rpmCache: cache, usageRecordWorkerPool: pool, gatewayService: &service.OpenAIGatewayService{}}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/v1/responses", nil)
	stage := OpenAIWebSocketUsageStage{Handler: h, Account: &service.Account{ID: 43, Platform: "openai", Type: service.AccountTypeAPIKey}, APIKey: &service.APIKey{ID: 1, User: &service.User{ID: 1}}}
	stage.RunUsage(c)
	require.Empty(t, cache.ids, "idle connection does not count")
	stage.Result = &service.OpenAIForwardResult{OpenAIWSMode: true, UpstreamTerminalEvent: "response.failed"}
	stage.RunUsage(c)
	require.Empty(t, cache.ids)
	stage.Result.UpstreamTerminalEvent = "response.completed"
	stage.TurnErr = errors.New("disconnected")
	stage.RunUsage(c)
	require.Empty(t, cache.ids)
	stage.TurnErr = nil
	stage.RunUsage(c)
	stage.RunUsage(c)
	require.Equal(t, []int64{43, 43}, cache.ids, "one count per completed turn")
}

func TestAccountRPMDetachedContextAndCacheFailure(t *testing.T) {
	cache := &completedRPMCache{err: errors.New("redis unavailable")}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.NotPanics(t, func() { recordAccountRPM(ctx, cache, &service.Account{ID: 42}) })
	require.NoError(t, cache.contextErr)
	require.True(t, cache.hasDeadline)
	require.Equal(t, []int64{42}, cache.ids, "a failed increment is not retried")
}

func (c *completedRPMCache) IncrementRPM(ctx context.Context, id int64) (int, error) {
	c.limitIDs = append(c.limitIDs, id)
	return len(c.limitIDs), nil
}

func TestAccountRPMKeepsExistingLimiterSeparate(t *testing.T) {
	cache := &completedRPMCache{}
	recordAccountRPM(context.Background(), cache, &service.Account{ID: 42, Platform: "anthropic", Type: service.AccountTypeOAuth, Extra: map[string]any{"base_rpm": 10}})
	recordAccountRPM(context.Background(), cache, &service.Account{ID: 43, Platform: "anthropic", Type: service.AccountTypeOAuth})
	recordAccountRPM(context.Background(), cache, &service.Account{ID: 44, Platform: "openai", Type: service.AccountTypeOAuth})
	require.Equal(t, []int64{42, 43, 44}, cache.ids)
	require.Empty(t, cache.limitIDs, "monitoring must not change the scheduling limiter")
}
