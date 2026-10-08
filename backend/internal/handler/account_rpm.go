package handler

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"go.uber.org/zap"
)

// recordAccountRPM runs at successful request completion, independently of billing.
// A canceled client context must not discard an already completed upstream request.
func recordAccountRPM(ctx context.Context, cache service.RPMCache, account *service.Account) {
	if cache == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if _, err := cache.IncrementRecentRPM(ctx, account.ID); err != nil {
		logger.L().Warn("gateway.rpm_increment_failed", zap.Int64("account_id", account.ID), zap.Error(err))
	}
}
