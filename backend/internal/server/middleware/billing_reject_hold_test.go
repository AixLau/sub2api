//go:build unit

package middleware

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func billingRejectHoldTestConfig(holdSeconds int) *config.Config {
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.APIKeyAuth.BillingRejectHold = config.BillingRejectHoldConfig{
		Enabled:            true,
		Threshold:          1,
		WindowSeconds:      60,
		InitialHoldSeconds: holdSeconds,
		MaxHoldSeconds:     holdSeconds,
		IdleResetSeconds:   holdSeconds + 60,
		MaxHeldPerUser:     4,
		MaxHeldTotal:       4,
		Capacity:           256,
	}
	return cfg
}

func billingRejectHoldTestService(cfg *config.Config, balance *atomic.Int64) (*service.APIKeyService, string) {
	const key = "hold-test-key"
	repo := &stubApiKeyRepo{
		getByKey: func(ctx context.Context, k string) (*service.APIKey, error) {
			if k != key {
				return nil, service.ErrAPIKeyNotFound
			}
			user := &service.User{ID: 77, Role: service.RoleUser, Status: service.StatusActive, Balance: float64(balance.Load()), Concurrency: 3}
			return &service.APIKey{ID: 700, UserID: user.ID, Key: key, Status: service.StatusActive, User: user}, nil
		},
	}
	return service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg), key
}

func doBillingHoldRequest(router *gin.Engine, key string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/t", nil)
	req.Header.Set("x-api-key", key)
	router.ServeHTTP(w, req)
	return w
}

func TestBillingRejectHoldReleasesHeldRequestAfterTopUp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var balance atomic.Int64
	cfg := billingRejectHoldTestConfig(60)
	apiKeyService, key := billingRejectHoldTestService(cfg, &balance)
	router := newAuthTestRouter(apiKeyService, nil, cfg)

	// 阈值内：立即拒绝，提示不变。
	first := doBillingHoldRequest(router, key)
	require.Equal(t, http.StatusForbidden, first.Code)
	requireAPIKeyAuthError(t, first, "INSUFFICIENT_BALANCE", "当前账户余额不足，请充值后重试，充值地址：https://aixlau.me/purchase")

	// 超过阈值：请求被挂起。
	held := make(chan *httptest.ResponseRecorder, 1)
	go func() { held <- doBillingHoldRequest(router, key) }()
	require.Eventually(t, func() bool { return apiKeyService.BillingRejectHoldHealth().Held == 1 }, time.Second, 5*time.Millisecond)

	// 解除信号到了但余额仍不足：继续挂起。
	apiKeyService.ReleaseBillingRejectHold(77)
	select {
	case <-held:
		t.Fatal("request must stay held while balance is still insufficient")
	case <-time.After(100 * time.Millisecond):
	}

	// 充值后解除：被挂起的请求直接放行，而不是返回 403。
	balance.Store(10)
	apiKeyService.ReleaseBillingRejectHold(77)
	select {
	case w := <-held:
		require.Equal(t, http.StatusOK, w.Code)
	case <-time.After(2 * time.Second):
		t.Fatal("held request was not released after top-up")
	}
	health := apiKeyService.BillingRejectHoldHealth()
	require.EqualValues(t, 0, health.Held)
	require.EqualValues(t, 0, health.Tracked)

	// 余额再次耗尽：重新从立即拒绝开始计数。
	balance.Store(0)
	again := doBillingHoldRequest(router, key)
	require.Equal(t, http.StatusForbidden, again.Code)
}

func TestBillingRejectHoldRejectsWhenHoldExpires(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var balance atomic.Int64
	cfg := billingRejectHoldTestConfig(1)
	apiKeyService, key := billingRejectHoldTestService(cfg, &balance)
	router := newAuthTestRouter(apiKeyService, nil, cfg)

	require.Equal(t, http.StatusForbidden, doBillingHoldRequest(router, key).Code)

	started := time.Now()
	w := doBillingHoldRequest(router, key)
	require.GreaterOrEqual(t, time.Since(started), 900*time.Millisecond)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Equal(t, "close", w.Header().Get("Connection"))
	requireAPIKeyAuthError(t, w, "INSUFFICIENT_BALANCE", "当前账户余额不足，请充值后重试，充值地址：https://aixlau.me/purchase")
	require.EqualValues(t, 1, apiKeyService.BillingRejectHoldHealth().Expired)
}

func TestBillingRejectHoldStopsWhenClientDisconnects(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var balance atomic.Int64
	cfg := billingRejectHoldTestConfig(60)
	apiKeyService, key := billingRejectHoldTestService(cfg, &balance)
	router := newAuthTestRouter(apiKeyService, nil, cfg)

	require.Equal(t, http.StatusForbidden, doBillingHoldRequest(router, key).Code)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/t", nil).WithContext(ctx)
		req.Header.Set("x-api-key", key)
		router.ServeHTTP(w, req)
		done <- w
	}()
	require.Eventually(t, func() bool { return apiKeyService.BillingRejectHoldHealth().Held == 1 }, time.Second, 5*time.Millisecond)
	cancel()
	select {
	case w := <-done:
		require.Equal(t, statusClientClosedRequest, w.Code)
	case <-time.After(2 * time.Second):
		t.Fatal("held request did not stop after client disconnect")
	}
	require.EqualValues(t, 0, apiKeyService.BillingRejectHoldHealth().Held)
}

func TestBillingRejectHoldGoogleStyleExpiry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var balance atomic.Int64
	cfg := billingRejectHoldTestConfig(1)
	apiKeyService, key := billingRejectHoldTestService(cfg, &balance)
	router := gin.New()
	router.Use(APIKeyAuthWithSubscriptionGoogle(apiKeyService, nil, cfg))
	router.GET("/t", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

	require.Equal(t, http.StatusForbidden, doBillingHoldRequest(router, key).Code)
	started := time.Now()
	w := doBillingHoldRequest(router, key)
	require.GreaterOrEqual(t, time.Since(started), 900*time.Millisecond)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), "PERMISSION_DENIED")
}

// 真实 HTTP 服务器上验证：挂起期间不读请求体，充值放行后处理函数仍能读到完整请求体。
func TestBillingRejectHoldReleasedRequestKeepsItsBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var balance atomic.Int64
	cfg := billingRejectHoldTestConfig(60)
	apiKeyService, key := billingRejectHoldTestService(cfg, &balance)
	router := gin.New()
	router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(apiKeyService, nil, cfg)))
	router.POST("/v1/responses", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.String(http.StatusBadRequest, err.Error())
			return
		}
		c.String(http.StatusOK, strconv.Itoa(len(body)))
	})
	server := httptest.NewServer(router)
	defer server.Close()

	post := func() (int, string) {
		req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/responses", bytes.NewReader(make([]byte, 2<<20)))
		require.NoError(t, err)
		req.Header.Set("x-api-key", key)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}

	status, _ := post()
	require.Equal(t, http.StatusForbidden, status)

	type result struct {
		status int
		body   string
	}
	held := make(chan result, 1)
	go func() {
		status, body := post()
		held <- result{status, body}
	}()
	require.Eventually(t, func() bool { return apiKeyService.BillingRejectHoldHealth().Held == 1 }, 2*time.Second, 5*time.Millisecond)

	balance.Store(10)
	apiKeyService.ReleaseBillingRejectHold(77)
	select {
	case r := <-held:
		require.Equal(t, http.StatusOK, r.status)
		require.Equal(t, strconv.Itoa(2<<20), r.body)
	case <-time.After(5 * time.Second):
		t.Fatal("held request was not released")
	}
}
