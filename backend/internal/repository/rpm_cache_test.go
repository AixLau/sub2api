package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestAccountRPMMinuteCounts(t *testing.T) {
	server := miniredis.RunT(t)
	now := time.Date(2026, 10, 8, 12, 0, 59, 0, time.UTC)
	server.SetTime(now)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := NewRPMCache(client)
	ctx := context.Background()
	var wg sync.WaitGroup
	errors := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := cache.IncrementRPM(ctx, 42); errors <- err }()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	counts, err := cache.GetRPMBatch(ctx, []int64{42, 43})
	require.NoError(t, err)
	require.Equal(t, map[int64]int{42: 12, 43: 0}, counts)
	server.SetTime(now.Add(time.Second))
	counts, err = cache.GetRPMBatch(ctx, []int64{42, 43})
	require.NoError(t, err)
	require.Equal(t, map[int64]int{42: 0, 43: 0}, counts)
	value, err := cache.IncrementRPM(ctx, 43)
	require.NoError(t, err)
	require.Equal(t, 1, value)
	require.Equal(t, 120*time.Second, server.TTL(fmt.Sprintf("rpm:43:%d", now.Add(time.Second).Unix()/60)))
}

func TestAccountRPMReadFailureIsNotZero(t *testing.T) {
	server := miniredis.RunT(t)
	now := time.Now()
	server.SetTime(now)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := NewRPMCache(client)
	require.NoError(t, server.Set(fmt.Sprintf("rpm:42:%d", now.Unix()/60), "invalid"))
	counts, err := cache.GetRPMBatch(context.Background(), []int64{42, 43})
	require.Error(t, err)
	require.Nil(t, counts)
	server.SetError("ERR unavailable")
	counts, err = cache.GetRPMBatch(context.Background(), []int64{43})
	require.Error(t, err)
	require.Nil(t, counts)
}

func TestAccountRPMRollingWindow(t *testing.T) {
	server := miniredis.RunT(t)
	start := time.Date(2026, 10, 8, 12, 0, 59, 500000000, time.UTC)
	server.SetTime(start)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := NewRPMCache(client)
	ctx := context.Background()
	// Several processes and completions sharing the exact Redis timestamp must
	// retain separate members rather than overwrite a timestamp-based member.
	otherClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = otherClient.Close() })
	otherCache := NewRPMCache(otherClient)
	_, err := cache.IncrementRecentRPM(ctx, 42)
	require.NoError(t, err)
	_, err = otherCache.IncrementRecentRPM(ctx, 42)
	require.NoError(t, err)
	server.SetTime(start.Add(time.Second)) // Cross the calendar-minute boundary.
	counts, err := cache.GetRecentRPMBatch(ctx, []int64{42, 43})
	require.NoError(t, err)
	require.Equal(t, map[int64]int{42: 2, 43: 0}, counts)
	_, err = cache.IncrementRecentRPM(ctx, 42)
	require.NoError(t, err)
	server.SetTime(start.Add(60*time.Second - time.Microsecond))
	counts, err = cache.GetRecentRPMBatch(ctx, []int64{42})
	require.NoError(t, err)
	require.Equal(t, 3, counts[42])
	server.SetTime(start.Add(60 * time.Second))
	counts, err = cache.GetRecentRPMBatch(ctx, []int64{42})
	require.NoError(t, err)
	require.Equal(t, 1, counts[42], "events exactly 60 seconds old leave the window")
	server.SetTime(start.Add(61 * time.Second))
	counts, err = cache.GetRecentRPMBatch(ctx, []int64{42})
	require.NoError(t, err)
	require.Equal(t, 0, counts[42])
	_, err = cache.IncrementRecentRPM(ctx, 43)
	require.NoError(t, err)
	require.Equal(t, 60*time.Second, server.TTL(recentRPMKey(43)))
	server.FastForward(60 * time.Second)
	require.False(t, server.Exists(recentRPMKey(43)), "idle accounts leave no permanent metrics keys")
}

func TestAccountRPMRollingReadFailure(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := NewRPMCache(client)
	require.NoError(t, server.Set(recentRPMKey(42), "invalid type"))
	counts, err := cache.GetRecentRPMBatch(context.Background(), []int64{42, 43})
	require.Error(t, err)
	require.Nil(t, counts)
	server.SetError("ERR unavailable")
	counts, err = cache.GetRecentRPMBatch(context.Background(), []int64{43})
	require.Error(t, err)
	require.Nil(t, counts)
}
