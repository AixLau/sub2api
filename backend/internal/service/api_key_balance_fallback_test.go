//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type balanceFallbackGroupRepoStub struct {
	groupRepoStub
	groups    []Group
	listCalls int
}

func (s *balanceFallbackGroupRepoStub) ListActiveByPlatform(_ context.Context, platform string) ([]Group, error) {
	s.listCalls++
	out := make([]Group, 0, len(s.groups))
	for _, g := range s.groups {
		if g.Platform == platform {
			out = append(out, g)
		}
	}
	return out, nil
}

type balanceFallbackRateRepoStub struct {
	userGroupRateRepoStubForGroupRate
	rates map[int64]float64
}

func (s *balanceFallbackRateRepoStub) GetByUserID(context.Context, int64) (map[int64]float64, error) {
	return s.rates, nil
}

func TestFindBalanceFallbackGroupPicksLowestEffectiveRate(t *testing.T) {
	standard := func(id int64, rate float64, mutate ...func(*Group)) Group {
		g := Group{ID: id, Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true, SubscriptionType: SubscriptionTypeStandard, RateMultiplier: rate, ActiveAccountCount: 1, SortOrder: int(id)}
		for _, m := range mutate {
			m(&g)
		}
		return g
	}
	groupRepo := &balanceFallbackGroupRepoStub{groups: []Group{
		standard(1, 1.0),
		standard(2, 0.1, func(g *Group) { g.SubscriptionType = SubscriptionTypeSubscription }),
		standard(3, 0.2, func(g *Group) { g.IsExclusive = true }),
		standard(4, 0.3, func(g *Group) { g.ActiveAccountCount = 0 }),
		standard(5, 0.3, func(g *Group) { g.Platform = PlatformAnthropic }),
		standard(6, 0.8),
		standard(7, 0.8),
	}}

	t.Run("default rates", func(t *testing.T) {
		svc := NewAPIKeyService(nil, nil, groupRepo, nil, &balanceFallbackRateRepoStub{}, nil, nil)
		group, err := svc.FindBalanceFallbackGroup(context.Background(), &User{ID: 9}, PlatformOpenAI)
		require.NoError(t, err)
		require.NotNil(t, group)
		require.Equal(t, int64(6), group.ID, "lowest bindable rate wins, ties keep sort order")
	})

	t.Run("exclusive group allowed and user rate override", func(t *testing.T) {
		svc := NewAPIKeyService(nil, nil, groupRepo, nil, &balanceFallbackRateRepoStub{rates: map[int64]float64{1: 0.05}}, nil, nil)
		group, err := svc.FindBalanceFallbackGroup(context.Background(), &User{ID: 9, AllowedGroups: []int64{3}}, PlatformOpenAI)
		require.NoError(t, err)
		require.Equal(t, int64(1), group.ID, "user-specific rate overrides group default")

		svc = NewAPIKeyService(nil, nil, groupRepo, nil, &balanceFallbackRateRepoStub{}, nil, nil)
		group, err = svc.FindBalanceFallbackGroup(context.Background(), &User{ID: 10, AllowedGroups: []int64{3}}, PlatformOpenAI)
		require.NoError(t, err)
		require.Equal(t, int64(3), group.ID, "allowed exclusive group is a candidate")
	})

	t.Run("no candidate", func(t *testing.T) {
		svc := NewAPIKeyService(nil, nil, groupRepo, nil, nil, nil, nil)
		group, err := svc.FindBalanceFallbackGroup(context.Background(), &User{ID: 9}, PlatformGemini)
		require.NoError(t, err)
		require.Nil(t, group)

		calls := groupRepo.listCalls
		_, _ = svc.FindBalanceFallbackGroup(context.Background(), &User{ID: 9}, PlatformGemini)
		require.Equal(t, calls, groupRepo.listCalls, "candidate list should be served from cache")
	})
}
