//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSchedulerBackgroundWriterLeaseOwnership(t *testing.T) {
	ctx := context.Background()
	cache, mr := newSchedulerCacheUnitWithRedis(t)

	ok, err := cache.TryAcquireBackgroundWriter(ctx, "a", 30*time.Second)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = cache.TryAcquireBackgroundWriter(ctx, "b", 30*time.Second)
	require.NoError(t, err)
	require.False(t, ok, "second owner must not acquire a held lease")

	held, err := cache.RenewBackgroundWriter(ctx, "b", 30*time.Second)
	require.NoError(t, err)
	require.False(t, held, "non-owner must not renew")

	held, err = cache.RenewBackgroundWriter(ctx, "a", 30*time.Second)
	require.NoError(t, err)
	require.True(t, held)

	require.NoError(t, cache.ReleaseBackgroundWriter(ctx, "b"))
	require.True(t, mr.Exists(schedulerBackgroundWriterLeaseKey), "non-owner release must not delete the lease")

	require.NoError(t, cache.ReleaseBackgroundWriter(ctx, "a"))
	require.False(t, mr.Exists(schedulerBackgroundWriterLeaseKey))

	ok, err = cache.TryAcquireBackgroundWriter(ctx, "b", 30*time.Second)
	require.NoError(t, err)
	require.True(t, ok)

	mr.FastForward(31 * time.Second)
	held, err = cache.RenewBackgroundWriter(ctx, "b", 30*time.Second)
	require.NoError(t, err)
	require.False(t, held, "expired lease cannot be renewed")
}

func TestSchedulerBackgroundWriterPreferredMarker(t *testing.T) {
	ctx := context.Background()
	cache, mr := newSchedulerCacheUnitWithRedis(t)

	owner, err := cache.PreferredBackgroundWriter(ctx)
	require.NoError(t, err)
	require.Empty(t, owner)

	require.NoError(t, cache.HeartbeatPreferredBackgroundWriter(ctx, "p", 30*time.Second))
	owner, err = cache.PreferredBackgroundWriter(ctx)
	require.NoError(t, err)
	require.Equal(t, "p", owner)

	require.NoError(t, cache.ClearPreferredBackgroundWriter(ctx, "other"))
	owner, err = cache.PreferredBackgroundWriter(ctx)
	require.NoError(t, err)
	require.Equal(t, "p", owner, "only the declaring owner may clear the marker")

	require.NoError(t, cache.ClearPreferredBackgroundWriter(ctx, "p"))
	owner, err = cache.PreferredBackgroundWriter(ctx)
	require.NoError(t, err)
	require.Empty(t, owner)

	require.NoError(t, cache.HeartbeatPreferredBackgroundWriter(ctx, "p", 30*time.Second))
	mr.FastForward(31 * time.Second)
	owner, err = cache.PreferredBackgroundWriter(ctx)
	require.NoError(t, err)
	require.Empty(t, owner, "marker expires when the preferred instance stops heartbeating")
}
