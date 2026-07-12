//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type opsStatsAccountRepoStub struct {
	AccountRepository
	accounts []Account
}

func (r *opsStatsAccountRepoStub) ListOpsAccountsForStats(context.Context, string, *int64) ([]Account, error) {
	return r.accounts, nil
}

type opsStatsConcurrencyCacheStub struct {
	ConcurrencyCache
	loads map[int64]*AccountLoadInfo
}

func (c *opsStatsConcurrencyCacheStub) GetAccountsLoadBatch(context.Context, []AccountWithConcurrency) (map[int64]*AccountLoadInfo, error) {
	return c.loads, nil
}

func TestGetConcurrencyStatsUsesOnlyAvailableAccountCapacity(t *testing.T) {
	t.Parallel()

	now := time.Now()
	rateLimitedUntil := now.Add(time.Hour)
	overloadedUntil := now.Add(time.Hour)
	tempUnschedulableUntil := now.Add(time.Hour)
	group := &Group{ID: 10, Name: "default", Platform: PlatformOpenAI}

	accounts := []Account{
		{ID: 1, Name: "available", Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, Concurrency: 3, Groups: []*Group{group}},
		{ID: 2, Name: "disabled", Platform: PlatformOpenAI, Status: StatusDisabled, Schedulable: true, Concurrency: 100, Groups: []*Group{group}},
		{ID: 3, Name: "failed", Platform: PlatformOpenAI, Status: StatusError, Schedulable: true, Concurrency: 100, Groups: []*Group{group}},
		{ID: 4, Name: "manually disabled", Platform: PlatformOpenAI, Status: StatusActive, Schedulable: false, Concurrency: 100, Groups: []*Group{group}},
		{ID: 5, Name: "rate limited", Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, Concurrency: 100, RateLimitResetAt: &rateLimitedUntil, Groups: []*Group{group}},
		{ID: 6, Name: "overloaded", Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, Concurrency: 100, OverloadUntil: &overloadedUntil, Groups: []*Group{group}},
		{ID: 7, Name: "temporarily unschedulable", Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, Concurrency: 100, TempUnschedulableUntil: &tempUnschedulableUntil, Groups: []*Group{group}},
	}

	svc := &OpsService{
		accountRepo: &opsStatsAccountRepoStub{accounts: accounts},
		concurrencyService: NewConcurrencyService(&opsStatsConcurrencyCacheStub{
			loads: map[int64]*AccountLoadInfo{
				1: {CurrentConcurrency: 2, WaitingCount: 1},
				2: {CurrentConcurrency: 99, WaitingCount: 99},
			},
		}),
	}

	platform, groups, account, _, err := svc.GetConcurrencyStats(context.Background(), "", nil)
	require.NoError(t, err)
	require.Len(t, account, len(accounts), "unavailable accounts remain visible in account detail rows")
	require.Equal(t, int64(3), platform[PlatformOpenAI].MaxCapacity)
	require.Equal(t, int64(2), platform[PlatformOpenAI].CurrentInUse)
	require.Equal(t, int64(1), platform[PlatformOpenAI].WaitingInQueue)
	require.Equal(t, int64(3), groups[group.ID].MaxCapacity)
}
