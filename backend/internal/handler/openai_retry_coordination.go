package handler

import (
	"context"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const openAIOverloadRetryQueueFullJitter = 250 * time.Millisecond

func openAIOverloadRetryKey(account *service.Account, model string) string {
	platform := "openai"
	if account != nil && strings.TrimSpace(account.Platform) != "" {
		platform = service.NormalizeOpenAICompatiblePlatform(account.Platform)
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = "unknown"
	}
	return platform + "/" + model
}

// waitForOpenAIOverloadRetry applies the shared provider/model retry lane only
// to recognized request-scoped overloads. A saturated queue uses the original
// retry delay plus a small jitter and continues the request retry chain. This
// keeps local queue pressure from becoming a client-visible 503.
func (h *OpenAIGatewayHandler) waitForOpenAIOverloadRetry(c *gin.Context, key string, delay time.Duration) error {
	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	}
	if h.overloadRetryScheduler == nil {
		h.overloadRetryScheduler = service.NewDefaultOpenAIOverloadRetryScheduler()
	}
	service.MarkOpsRetryWaitStarted(c)
	err := h.overloadRetryScheduler.Acquire(ctx, key, delay)
	if err == service.ErrOpenAIOverloadRetryQueueFull {
		fallbackDelay := delay
		if fallbackDelay <= 0 {
			fallbackDelay = 500 * time.Millisecond
		}
		fallbackDelay += time.Duration(time.Now().UnixNano() % int64(openAIOverloadRetryQueueFullJitter))
		timer := time.NewTimer(fallbackDelay)
		defer timer.Stop()
		select {
		case <-timer.C:
			err = nil
		case <-ctx.Done():
			err = ctx.Err()
		}
	}
	service.MarkOpsRetryWaitFinished(c)
	return err
}
