package handler

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

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
// to recognized request-scoped overloads. A saturated queue falls back to the
// existing fixed delay so a transient local scheduler limit never turns into a
// client-visible 429 or 503.
func (h *OpenAIGatewayHandler) waitForOpenAIOverloadRetry(c *gin.Context, key string, delay time.Duration) (*service.OpenAIOverloadRetryLease, error) {
	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	}
	if h.overloadRetryScheduler == nil {
		h.overloadRetryScheduler = service.NewDefaultOpenAIOverloadRetryScheduler()
	}
	service.MarkOpsRetryWaitStarted(c)
	lease, err := h.overloadRetryScheduler.Acquire(ctx, key, delay)
	service.MarkOpsRetryWaitFinished(c)
	if errors.Is(err, service.ErrOpenAIOverloadRetryQueueFull) {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return lease, err
}
