package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestImageConcurrencyLimiter_DefaultDisabledAllowsRequests(t *testing.T) {
	limiter := &imageConcurrencyLimiter{}

	release, acquired := limiter.TryAcquire(false, 1)

	require.True(t, acquired)
	require.Nil(t, release)
}

func TestImageConcurrencyLimiter_RejectsWhenLimitReachedAndAllowsAfterRelease(t *testing.T) {
	limiter := &imageConcurrencyLimiter{}

	release, acquired := limiter.TryAcquire(true, 1)
	require.True(t, acquired)
	require.NotNil(t, release)

	secondRelease, secondAcquired := limiter.TryAcquire(true, 1)
	require.False(t, secondAcquired)
	require.Nil(t, secondRelease)

	release()
	thirdRelease, thirdAcquired := limiter.TryAcquire(true, 1)
	require.True(t, thirdAcquired)
	require.NotNil(t, thirdRelease)
	thirdRelease()
}

func TestImageConcurrencyLimiter_WaitsUntilSlotReleased(t *testing.T) {
	limiter := &imageConcurrencyLimiter{}
	release, acquired := limiter.Acquire(context.Background(), true, 1, true, time.Second, 1)
	require.True(t, acquired)
	require.NotNil(t, release)

	acquiredCh := make(chan func(), 1)
	go func() {
		waitRelease, waitAcquired := limiter.Acquire(context.Background(), true, 1, true, time.Second, 1)
		require.True(t, waitAcquired)
		acquiredCh <- waitRelease
	}()

	time.Sleep(20 * time.Millisecond)
	release()

	select {
	case waitRelease := <-acquiredCh:
		require.NotNil(t, waitRelease)
		waitRelease()
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for image concurrency slot")
	}
}

func TestImageConcurrencyLimiter_WaitTimesOut(t *testing.T) {
	limiter := &imageConcurrencyLimiter{}
	release, acquired := limiter.Acquire(context.Background(), true, 1, true, time.Second, 1)
	require.True(t, acquired)
	require.NotNil(t, release)
	defer release()

	waitRelease, waitAcquired := limiter.Acquire(context.Background(), true, 1, true, 10*time.Millisecond, 1)

	require.False(t, waitAcquired)
	require.Nil(t, waitRelease)
}

func TestImageConcurrencyLimiter_MaxWaitingRequestsRejectsOverflow(t *testing.T) {
	limiter := &imageConcurrencyLimiter{}
	release, acquired := limiter.Acquire(context.Background(), true, 1, true, time.Second, 1)
	require.True(t, acquired)
	require.NotNil(t, release)
	defer release()

	waitingStarted := make(chan struct{})
	waitingDone := make(chan struct{})
	go func() {
		close(waitingStarted)
		waitRelease, waitAcquired := limiter.Acquire(context.Background(), true, 1, true, time.Second, 1)
		if waitAcquired && waitRelease != nil {
			waitRelease()
		}
		close(waitingDone)
	}()
	<-waitingStarted
	time.Sleep(20 * time.Millisecond)

	overflowRelease, overflowAcquired := limiter.Acquire(context.Background(), true, 1, true, time.Second, 1)

	require.False(t, overflowAcquired)
	require.Nil(t, overflowRelease)
	release()
	<-waitingDone
}

func TestOpenAIGatewayHandlerAcquireImageGenerationSlot_Returns429WhenFull(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)

	h := &OpenAIGatewayHandler{
		cfg: &config.Config{
			Gateway: config.GatewayConfig{
				ImageConcurrency: config.ImageConcurrencyConfig{
					Enabled:               true,
					MaxConcurrentRequests: 1,
					OverflowMode:          config.ImageConcurrencyOverflowModeReject,
				},
			},
		},
		imageLimiter: &imageConcurrencyLimiter{},
	}
	release, acquired := h.acquireImageGenerationSlot(c, false)
	require.True(t, acquired)
	require.NotNil(t, release)
	defer release()

	blockedRelease, blocked := h.acquireImageGenerationSlot(c, false)

	require.False(t, blocked)
	require.Nil(t, blockedRelease)
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.Equal(t, "rate_limit_error", gjson.GetBytes(rec.Body.Bytes(), "error.type").String())
	require.Contains(t, rec.Body.String(), "图片生成并发已达上限")
}

func TestOpenAIGatewayHandlerAcquireImageGenerationSlot_LimitsPerUserIndependently(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &OpenAIGatewayHandler{
		cfg: &config.Config{
			Gateway: config.GatewayConfig{
				ImageConcurrency: config.ImageConcurrencyConfig{
					Enabled:                      true,
					MaxConcurrentRequestsPerUser: 1,
					OverflowMode:                 config.ImageConcurrencyOverflowModeReject,
				},
			},
		},
		imageLimiter: &imageConcurrencyLimiter{},
	}
	userOne := newImageConcurrencyUserContext(20)
	releaseOne, acquiredOne := h.acquireImageGenerationSlot(userOne, false)
	require.True(t, acquiredOne)
	require.NotNil(t, releaseOne)
	defer releaseOne()

	blocked := newImageConcurrencyUserContext(20)
	blockedRelease, blockedAcquired := h.acquireImageGenerationSlot(blocked, false)
	require.False(t, blockedAcquired)
	require.Nil(t, blockedRelease)
	require.Equal(t, http.StatusTooManyRequests, blocked.Writer.Status())

	userTwo := newImageConcurrencyUserContext(21)
	releaseTwo, acquiredTwo := h.acquireImageGenerationSlot(userTwo, false)
	require.True(t, acquiredTwo)
	require.NotNil(t, releaseTwo)
	releaseTwo()

	releaseOne()
	afterRelease := newImageConcurrencyUserContext(20)
	nextRelease, nextAcquired := h.acquireImageGenerationSlot(afterRelease, false)
	require.True(t, nextAcquired)
	require.NotNil(t, nextRelease)
	nextRelease()
}

func TestOpenAIGatewayHandlerAcquireImageGenerationSlot_UserLimitGrowsWithIdleSlots(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &OpenAIGatewayHandler{
		cfg: &config.Config{
			Gateway: config.GatewayConfig{
				ImageConcurrency: config.ImageConcurrencyConfig{
					Enabled:                      true,
					MaxConcurrentRequests:        6,
					MaxConcurrentRequestsPerUser: 4,
					OverflowMode:                 config.ImageConcurrencyOverflowModeReject,
				},
			},
		},
		imageLimiter: &imageConcurrencyLimiter{},
	}

	releaseUser20A, acquired := h.acquireImageGenerationSlot(newImageConcurrencyUserContext(20), false)
	require.True(t, acquired)
	defer releaseUser20A()
	releaseUser20B, acquired := h.acquireImageGenerationSlot(newImageConcurrencyUserContext(20), false)
	require.True(t, acquired)
	defer releaseUser20B()

	var otherReleases []func()
	for _, userID := range []int64{21, 22, 23} {
		release, otherAcquired := h.acquireImageGenerationSlot(newImageConcurrencyUserContext(userID), false)
		require.True(t, otherAcquired)
		require.NotNil(t, release)
		otherReleases = append(otherReleases, release)
	}

	blocked := newImageConcurrencyUserContext(20)
	blockedRelease, blockedAcquired := h.acquireImageGenerationSlot(blocked, false)
	require.False(t, blockedAcquired)
	require.Nil(t, blockedRelease)
	require.Equal(t, http.StatusTooManyRequests, blocked.Writer.Status())

	for _, release := range otherReleases {
		release()
	}
	releaseUser20C, acquired := h.acquireImageGenerationSlot(newImageConcurrencyUserContext(20), false)
	require.True(t, acquired)
	require.NotNil(t, releaseUser20C)
	releaseUser20C()
}

func TestOpenAIGatewayHandlerResponses_ImageIntentRejectedByImageConcurrency(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"model":"gpt-5.4","input":"draw","tools":[{"type":"image_generation"}]}`
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	groupID := int64(1)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		ID:      10,
		GroupID: &groupID,
		Group: &service.Group{
			ID:                   groupID,
			AllowImageGeneration: true,
		},
		User: &service.User{ID: 20},
	})
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 20, Concurrency: 1})

	h := &OpenAIGatewayHandler{
		gatewayService:           &service.OpenAIGatewayService{},
		billingCacheService:      &service.BillingCacheService{},
		apiKeyService:            &service.APIKeyService{},
		contentModerationService: newDisabledContentModerationServiceForHandlerTest(t),
		concurrencyHelper:        &ConcurrencyHelper{concurrencyService: service.NewConcurrencyService(&helperConcurrencyCacheStub{userSeq: []bool{true}})},
		errorPassthroughService:  nil,
		cfg: &config.Config{Gateway: config.GatewayConfig{ImageConcurrency: config.ImageConcurrencyConfig{
			Enabled:               true,
			MaxConcurrentRequests: 1,
			OverflowMode:          config.ImageConcurrencyOverflowModeReject,
		}}},
		imageLimiter: &imageConcurrencyLimiter{},
	}
	release, acquired := h.acquireImageGenerationSlot(c, false)
	require.True(t, acquired)
	require.NotNil(t, release)
	defer release()
	rec.Body.Reset()
	rec.Code = 0

	result := h.EnterOpenAIHTTPGatewayPipeline(c, openAIResponsesHTTPRouteMetaForTest())

	require.True(t, result.Stop)
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.Equal(t, "rate_limit_error", gjson.GetBytes(rec.Body.Bytes(), "error.type").String())
	require.Contains(t, rec.Body.String(), "图片生成并发已达上限")
}

func TestOpenAIGatewayHandlerResponses_TextOnlyNotRejectedByImageConcurrency(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"model":"gpt-5.4","input":"write code"}`
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	groupID := int64(1)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		ID:      10,
		GroupID: &groupID,
		Group: &service.Group{
			ID:                   groupID,
			AllowImageGeneration: true,
		},
		User: &service.User{ID: 20},
	})
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 20, Concurrency: 1})

	h := &OpenAIGatewayHandler{
		gatewayService:           &service.OpenAIGatewayService{},
		billingCacheService:      service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, &config.Config{RunMode: config.RunModeSimple}, nil),
		apiKeyService:            &service.APIKeyService{},
		contentModerationService: newDisabledContentModerationServiceForHandlerTest(t),
		concurrencyHelper:        &ConcurrencyHelper{concurrencyService: service.NewConcurrencyService(&helperConcurrencyCacheStub{userSeq: []bool{true}})},
		cfg: &config.Config{Gateway: config.GatewayConfig{ImageConcurrency: config.ImageConcurrencyConfig{
			Enabled:               true,
			MaxConcurrentRequests: 1,
			OverflowMode:          config.ImageConcurrencyOverflowModeReject,
		}}},
		imageLimiter: &imageConcurrencyLimiter{},
	}
	release, acquired := h.acquireImageGenerationSlot(c, false)
	require.True(t, acquired)
	require.NotNil(t, release)
	defer release()
	rec.Body.Reset()
	rec.Code = 0

	h.Responses(c)

	require.NotEqual(t, http.StatusTooManyRequests, rec.Code)
	require.NotContains(t, rec.Body.String(), "Image generation concurrency limit exceeded")
}

func newImageConcurrencyUserContext(userID int64) *gin.Context {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: userID})
	return c
}
