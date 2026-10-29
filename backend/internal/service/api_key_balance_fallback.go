package service

import (
	"context"
	"fmt"
	"strconv"
	"time"

	gocache "github.com/patrickmn/go-cache"
)

// 订阅回落余额时的候选分组与用户专属倍率缓存，避免订阅用尽后每个请求都查库。
const balanceFallbackGroupCacheTTL = 30 * time.Second

// FindBalanceFallbackGroup 返回用户可绑定的、同平台有效倍率最低的余额（标准）分组。
// 有效倍率优先取用户专属倍率，否则取分组默认倍率；倍率相同时按 SortOrder、ID 取靠前者。
// 没有合适分组时返回 (nil, nil)。
func (s *APIKeyService) FindBalanceFallbackGroup(ctx context.Context, user *User, platform string) (*Group, error) {
	if s == nil || s.groupRepo == nil || user == nil || platform == "" {
		return nil, nil
	}
	groups, err := s.balanceFallbackCandidates(ctx, platform)
	if err != nil {
		return nil, err
	}
	rates, err := s.balanceFallbackUserRates(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	var best *Group
	bestRate := 0.0
	for i := range groups {
		group := &groups[i]
		if !user.CanBindGroup(group.ID, group.IsExclusive) {
			continue
		}
		rate := group.RateMultiplier
		if override, ok := rates[group.ID]; ok {
			rate = override
		}
		// 候选已按 SortOrder、ID 排序，严格小于即可保持并列时的顺序。
		if best == nil || rate < bestRate {
			best, bestRate = group, rate
		}
	}
	if best == nil {
		return nil, nil
	}
	cloned := *best
	return &cloned, nil
}

func (s *APIKeyService) balanceFallbackCandidates(ctx context.Context, platform string) ([]Group, error) {
	cache := s.balanceFallbackCacheL1()
	if cached, ok := cache.Get("groups:" + platform); ok {
		return cached.([]Group), nil
	}
	all, err := s.groupRepo.ListActiveByPlatform(ctx, platform)
	if err != nil {
		return nil, fmt.Errorf("list balance fallback groups: %w", err)
	}
	candidates := make([]Group, 0, len(all))
	for _, group := range all {
		// 订阅分组不参与余额回落；仅限 Claude Code 的分组会拒绝其他客户端；
		// 没有可用账号的分组必然调度失败。
		if group.IsSubscriptionType() || !group.IsActive() || group.Platform != platform ||
			group.ClaudeCodeOnly || group.ActiveAccountCount <= 0 {
			continue
		}
		candidates = append(candidates, group)
	}
	cache.Set("groups:"+platform, candidates, gocache.DefaultExpiration)
	return candidates, nil
}

func (s *APIKeyService) balanceFallbackUserRates(ctx context.Context, userID int64) (map[int64]float64, error) {
	if s.userGroupRateRepo == nil {
		return nil, nil
	}
	cache := s.balanceFallbackCacheL1()
	key := "rates:" + strconv.FormatInt(userID, 10)
	if cached, ok := cache.Get(key); ok {
		return cached.(map[int64]float64), nil
	}
	rates, err := s.userGroupRateRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user group rates: %w", err)
	}
	cache.Set(key, rates, gocache.DefaultExpiration)
	return rates, nil
}

func (s *APIKeyService) balanceFallbackCacheL1() *gocache.Cache {
	s.balanceFallbackCacheOnce.Do(func() {
		s.balanceFallbackCache = gocache.New(balanceFallbackGroupCacheTTL, 2*balanceFallbackGroupCacheTTL)
	})
	return s.balanceFallbackCache
}
