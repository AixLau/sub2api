//go:build unit

package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newSpilloverTestCache(t *testing.T) (*gatewayCache, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return &gatewayCache{rdb: rdb}, mr
}

func spilloverUpdate(expectedPrimary, expectedSecondary, selected int64, swap bool) service.OpenAIStickySpilloverBindingUpdate {
	return service.OpenAIStickySpilloverBindingUpdate{
		PrimaryKey: "openai:s", SecondaryKey: "openai:spill:s",
		ExpectedPrimaryID: expectedPrimary, ExpectedSecondaryID: expectedSecondary, SelectedID: selected,
		Swap: swap, PrimaryTTL: time.Hour, SecondaryTTL: 10 * time.Minute,
	}
}

func TestGatewayCache_UpdateStickySpilloverBinding(t *testing.T) {
	ctx := context.Background()
	const group = int64(7)
	read := func(t *testing.T, c *gatewayCache) (int64, int64) {
		t.Helper()
		primary, _ := c.GetSessionAccountID(ctx, group, "openai:s")
		secondary, _ := c.GetSessionAccountID(ctx, group, "openai:spill:s")
		return primary, secondary
	}

	t.Run("records the secondary and keeps the primary", func(t *testing.T) {
		c, mr := newSpilloverTestCache(t)
		require.NoError(t, c.SetSessionAccountID(ctx, group, "openai:s", 1, time.Hour))
		outcome, err := c.UpdateStickySpilloverBinding(ctx, group, spilloverUpdate(1, 0, 2, false))
		require.NoError(t, err)
		require.Equal(t, service.OpenAIStickySpilloverBindingRecorded, outcome)
		primary, secondary := read(t, c)
		require.Equal(t, int64(1), primary)
		require.Equal(t, int64(2), secondary)
		require.Equal(t, 10*time.Minute, mr.TTL(buildSessionKey(group, "openai:spill:s")))
	})

	t.Run("swaps both keys in one step", func(t *testing.T) {
		c, mr := newSpilloverTestCache(t)
		require.NoError(t, c.SetSessionAccountID(ctx, group, "openai:s", 1, time.Minute))
		require.NoError(t, c.SetSessionAccountID(ctx, group, "openai:spill:s", 2, time.Minute))
		outcome, err := c.UpdateStickySpilloverBinding(ctx, group, spilloverUpdate(1, 2, 2, true))
		require.NoError(t, err)
		require.Equal(t, service.OpenAIStickySpilloverBindingSwapped, outcome)
		primary, secondary := read(t, c)
		require.Equal(t, int64(2), primary)
		require.Equal(t, int64(1), secondary)
		require.Equal(t, time.Hour, mr.TTL(buildSessionKey(group, "openai:s")))
		require.Equal(t, 10*time.Minute, mr.TTL(buildSessionKey(group, "openai:spill:s")))
	})

	t.Run("a stale view never overwrites a newer binding", func(t *testing.T) {
		c, _ := newSpilloverTestCache(t)
		require.NoError(t, c.SetSessionAccountID(ctx, group, "openai:s", 2, time.Hour))
		require.NoError(t, c.SetSessionAccountID(ctx, group, "openai:spill:s", 1, time.Hour))
		// The caller still believes primary=1/secondary=2 and spilled to 3.
		for _, update := range []service.OpenAIStickySpilloverBindingUpdate{
			spilloverUpdate(1, 2, 3, false),
			spilloverUpdate(1, 2, 2, true),
			// Right primary, but the secondary it read (none) was replaced meanwhile.
			spilloverUpdate(2, 0, 3, false),
		} {
			outcome, err := c.UpdateStickySpilloverBinding(ctx, group, update)
			require.NoError(t, err)
			require.Equal(t, service.OpenAIStickySpilloverBindingConflict, outcome)
		}
		primary, secondary := read(t, c)
		require.Equal(t, int64(2), primary)
		require.Equal(t, int64(1), secondary)
	})

	t.Run("a missing primary is never created by a swap", func(t *testing.T) {
		c, _ := newSpilloverTestCache(t)
		require.NoError(t, c.SetSessionAccountID(ctx, group, "openai:spill:s", 2, time.Hour))
		outcome, err := c.UpdateStickySpilloverBinding(ctx, group, spilloverUpdate(1, 2, 2, true))
		require.NoError(t, err)
		require.Equal(t, service.OpenAIStickySpilloverBindingRecorded, outcome)
		primary, secondary := read(t, c)
		require.Zero(t, primary)
		require.Equal(t, int64(2), secondary)
	})

	t.Run("a secondary equal to the primary is an invalid hint, not a conflict", func(t *testing.T) {
		c, _ := newSpilloverTestCache(t)
		require.NoError(t, c.SetSessionAccountID(ctx, group, "openai:s", 1, time.Hour))
		require.NoError(t, c.SetSessionAccountID(ctx, group, "openai:spill:s", 1, time.Hour))
		outcome, err := c.UpdateStickySpilloverBinding(ctx, group, spilloverUpdate(1, 0, 3, false))
		require.NoError(t, err)
		require.Equal(t, service.OpenAIStickySpilloverBindingRecorded, outcome)
		_, secondary := read(t, c)
		require.Equal(t, int64(3), secondary)
	})

	t.Run("concurrent spills of one session leave a single consistent pair", func(t *testing.T) {
		c, _ := newSpilloverTestCache(t)
		require.NoError(t, c.SetSessionAccountID(ctx, group, "openai:s", 1, time.Hour))
		var wg sync.WaitGroup
		var mu sync.Mutex
		recorded := map[int64]bool{}
		for account := int64(2); account <= 9; account++ {
			wg.Add(1)
			go func(account int64) {
				defer wg.Done()
				outcome, err := c.UpdateStickySpilloverBinding(ctx, group, spilloverUpdate(1, 0, account, false))
				if err == nil && outcome == service.OpenAIStickySpilloverBindingRecorded {
					mu.Lock()
					recorded[account] = true
					mu.Unlock()
				}
			}(account)
		}
		wg.Wait()
		primary, secondary := read(t, c)
		require.Equal(t, int64(1), primary)
		require.Len(t, recorded, 1, "exactly one concurrent spill may claim the secondary")
		require.True(t, recorded[secondary])
	})
}
