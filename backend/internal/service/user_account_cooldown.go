package service

import (
	"context"
	"log/slog"
	"time"
)

const (
	defaultUserAccountCooldownSeconds = 60
	maxUserAccountCooldownSeconds     = 3600
)

func recordUserAccountCooldown(ctx context.Context, cache GatewayCache, userID, accountID int64, ttl time.Duration) {
	if cache == nil || userID <= 0 || accountID <= 0 || ttl <= 0 {
		return
	}
	if err := cache.SetUserAccountCooldown(ctx, userID, accountID, ttl); err != nil {
		slog.Warn("failed to set user account cooldown", "user_id", userID, "account_id", accountID, "error", err)
	}
}

func UserAccountCooldownTTL() time.Duration {
	return time.Duration(defaultUserAccountCooldownSeconds) * time.Second
}

func (s *GatewayService) UserAccountCooldownTTL(ctx context.Context) time.Duration {
	if s == nil || s.settingService == nil {
		return UserAccountCooldownTTL()
	}
	return s.settingService.GetUserAccountCooldownTTL(ctx)
}

func (s *OpenAIGatewayService) UserAccountCooldownTTL(ctx context.Context) time.Duration {
	if s == nil || s.settingService == nil {
		return UserAccountCooldownTTL()
	}
	return s.settingService.GetUserAccountCooldownTTL(ctx)
}

func mergeUserAccountCooldowns(ctx context.Context, cache GatewayCache, excludedIDs map[int64]struct{}, userID int64) map[int64]struct{} {
	if cache == nil || userID <= 0 {
		return excludedIDs
	}
	cooldowns, err := cache.GetUserAccountCooldowns(ctx, userID)
	if err != nil {
		slog.Warn("failed to get user account cooldowns", "user_id", userID, "error", err)
		return excludedIDs
	}
	if len(cooldowns) == 0 {
		return excludedIDs
	}
	merged := make(map[int64]struct{}, len(excludedIDs)+len(cooldowns))
	for id := range excludedIDs {
		merged[id] = struct{}{}
	}
	for id := range cooldowns {
		merged[id] = struct{}{}
	}
	return merged
}

func (s *GatewayService) mergeUserAccountCooldowns(ctx context.Context, excludedIDs map[int64]struct{}, userID int64) map[int64]struct{} {
	if s == nil {
		return excludedIDs
	}
	return mergeUserAccountCooldowns(ctx, s.cache, excludedIDs, userID)
}

func (s *GatewayService) CooldownUserAccount(ctx context.Context, userID, accountID int64, ttl time.Duration) {
	if s == nil {
		return
	}
	recordUserAccountCooldown(ctx, s.cache, userID, accountID, ttl)
}

func (s *OpenAIGatewayService) mergeUserAccountCooldowns(ctx context.Context, excludedIDs map[int64]struct{}, userID int64) map[int64]struct{} {
	if s == nil {
		return excludedIDs
	}
	return mergeUserAccountCooldowns(ctx, s.cache, excludedIDs, userID)
}

func (s *OpenAIGatewayService) CooldownUserAccount(ctx context.Context, userID, accountID int64, ttl time.Duration) {
	if s == nil {
		return
	}
	recordUserAccountCooldown(ctx, s.cache, userID, accountID, ttl)
}
