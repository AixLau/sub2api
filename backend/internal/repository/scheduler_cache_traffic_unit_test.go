//go:build unit

package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// 这组测试覆盖“内容未变就不在网络上传输账号记录”的读写路径，
// 以及多个实例（各自独立的 schedulerCache）共用同一个 Redis 时的正确性。

// newSchedulerCachePeer 在同一个 Redis 上再建一个独立实例，模拟另一台 sub2api。
func newSchedulerCachePeer(t *testing.T, addr string) (*schedulerCache, *schedulerCacheCommandRecorder) {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = rdb.Close() })
	recorder := &schedulerCacheCommandRecorder{}
	rdb.AddHook(recorder)
	cache, ok := newSchedulerCacheWithChunkSizes(rdb, defaultSchedulerSnapshotMGetChunkSize, defaultSchedulerSnapshotWriteChunkSize).(*schedulerCache)
	require.True(t, ok)
	return cache, recorder
}

func TestSchedulerCacheRebuildWritesOnlyChangedAccounts(t *testing.T) {
	ctx := context.Background()
	cache, mr, recorder := newSchedulerCacheMemoUnit(t, 2)
	accounts := []service.Account{
		memoTestAccount(301, "a"), memoTestAccount(302, "b"), memoTestAccount(303, "c"),
		memoTestAccount(304, "d"), memoTestAccount(305, "e"),
	}
	_, err := cache.writeAccountIDs(ctx, accounts)
	require.NoError(t, err)
	revision := memoRevision(t, cache, memoAnthropicRevisionKey)

	t.Run("identical rebuild sends nothing", func(t *testing.T) {
		recorder.reset()
		ids, err := cache.writeAccountIDs(ctx, accounts)
		require.NoError(t, err)
		require.Equal(t, []int64{301, 302, 303, 304, 305}, ids)
		require.Zero(t, recorder.countPrefix("set "))
		require.Empty(t, recorder.pipelineBatches())
		require.Equal(t, revision, memoRevision(t, cache, memoAnthropicRevisionKey))
	})

	t.Run("only the changed account is written", func(t *testing.T) {
		changed := append([]service.Account(nil), accounts...)
		changed[1] = memoTestAccount(302, "b2")
		recorder.reset()
		_, err := cache.writeAccountIDs(ctx, changed)
		require.NoError(t, err)
		require.Equal(t, [][]string{{
			"set " + schedulerAccountKey("302"),
			"set " + schedulerAccountMetaKey("302"),
		}, {
			"set " + memoAnthropicRevisionKey,
		}}, recorder.pipelineBatches(), "the revision still follows the last metadata write")
		require.NotEqual(t, revision, memoRevision(t, cache, memoAnthropicRevisionKey))
		accounts = changed
		revision = memoRevision(t, cache, memoAnthropicRevisionKey)
	})

	t.Run("change outside the scheduler projection rewrites only the full record", func(t *testing.T) {
		changed := append([]service.Account(nil), accounts...)
		changed[2].Credentials = map[string]any{
			"model_mapping": map[string]any{"claude-sonnet": "sonnet-c"},
			"access_token":  "rotated",
		}
		recorder.reset()
		_, err := cache.writeAccountIDs(ctx, changed)
		require.NoError(t, err)
		require.Equal(t, [][]string{{"set " + schedulerAccountKey("303")}}, recorder.pipelineBatches())
		require.Equal(t, revision, memoRevision(t, cache, memoAnthropicRevisionKey), "metadata did not change")
		accounts = changed

		got, err := cache.GetAccount(ctx, 303)
		require.NoError(t, err)
		require.Equal(t, "rotated", got.Credentials["access_token"])
	})

	t.Run("records removed or overwritten elsewhere are written again", func(t *testing.T) {
		mr.Del(schedulerAccountKey("304"))
		mr.Del(schedulerAccountMetaKey("304"))
		require.NoError(t, mr.Set(schedulerAccountMetaKey("305"), `{"ID":305,"Name":"stale","Platform":"anthropic"}`))

		recorder.reset()
		_, err := cache.writeAccountIDs(ctx, accounts)
		require.NoError(t, err)
		require.Equal(t, [][]string{{
			"set " + schedulerAccountKey("304"),
			"set " + schedulerAccountMetaKey("304"),
		}, {
			"set " + schedulerAccountMetaKey("305"),
			"set " + memoAnthropicRevisionKey,
		}}, recorder.pipelineBatches())

		bucket := memoTestBucket(130)
		memoPublish(t, cache, bucket, accounts...)
		got := memoRead(t, cache, bucket)
		require.Equal(t, "d", got[3].Name)
		require.Equal(t, "e", got[4].Name)
	})
}

func TestSchedulerCacheSnapshotReloadTransfersOnlyChangedMetadata(t *testing.T) {
	ctx := context.Background()
	cache, mr, _ := newSchedulerCacheMemoUnit(t, defaultSchedulerSnapshotWriteChunkSize)
	bucket := memoTestBucket(131)
	accounts := []service.Account{memoTestAccount(311, "a"), memoTestAccount(312, "b"), memoTestAccount(313, "c")}
	memoPublish(t, cache, bucket, accounts...)
	memoRead(t, cache, bucket)

	masterOf := func(id string) *service.Account {
		cache.memoMu.Lock()
		defer cache.memoMu.Unlock()
		entry := cache.metaMemo[id]
		require.NotNil(t, entry)
		return entry.account
	}
	a, b, c := masterOf("311"), masterOf("312"), masterOf("313")

	// 重建发布了新版本但内容没变：读侧重新加载，母本全部复用。
	memoPublish(t, cache, bucket, accounts...)
	got := memoRead(t, cache, bucket)
	require.Equal(t, []int64{311, 312, 313}, memoIDs(got))
	require.Same(t, a, masterOf("311"))
	require.Same(t, b, masterOf("312"))
	require.Same(t, c, masterOf("313"))

	// 只有一个账号变了：只有它被重新取回并解码。
	require.NoError(t, cache.SetAccount(ctx, ptrAccount(memoTestAccount(312, "b2"))))
	got = memoRead(t, cache, bucket)
	require.Equal(t, "b2", got[1].Name)
	require.Equal(t, "sonnet-b2", got[1].GetMappedModel("claude-sonnet"))
	require.Same(t, a, masterOf("311"))
	require.NotSame(t, b, masterOf("312"))
	require.Same(t, c, masterOf("313"))

	// 记录被绕过修订号直接改写（例如旧版本实例或写入出错）：最长复用时长过后仍能读到新值。
	raw, err := mr.Get(schedulerAccountMetaKey("313"))
	require.NoError(t, err)
	require.NoError(t, mr.Set(schedulerAccountMetaKey("313"), raw+" "))
	base := time.Now()
	cache.now = func() time.Time { return base.Add(schedulerSnapshotCacheMaxAge + time.Second) }
	got = memoRead(t, cache, bucket)
	require.Equal(t, "c", got[2].Name)
	require.NotSame(t, c, masterOf("313"))

	// 记录缺失按未命中处理。
	mr.Del(schedulerAccountMetaKey("311"))
	cache.now = func() time.Time { return base.Add(3 * schedulerSnapshotCacheMaxAge) }
	memoRequireMiss(t, cache, bucket)
}

func TestSchedulerCacheGetAccountReusesUnchangedPayload(t *testing.T) {
	ctx := context.Background()
	cache, mr, recorder := newSchedulerCacheMemoUnit(t, defaultSchedulerSnapshotWriteChunkSize)

	missing, err := cache.GetAccount(ctx, 321)
	require.NoError(t, err)
	require.Nil(t, missing)

	require.NoError(t, cache.SetAccount(ctx, ptrAccount(memoTestAccount(321, "a"))))
	first, err := cache.GetAccount(ctx, 321)
	require.NoError(t, err)
	require.Equal(t, "a", first.Name)
	require.Nil(t, first.LastUsedAt)
	require.Equal(t, "dropped-by-projection", first.Credentials["access_token"], "the full record keeps credentials")

	// 调用方可以随意修改拿到的对象，不影响后续读取。
	first.Name = "mutated"
	first.Credentials["access_token"] = "mutated"

	usedAt := time.Now().UTC().Truncate(time.Millisecond)
	require.NoError(t, cache.UpdateLastUsed(ctx, map[int64]time.Time{321: usedAt}))
	recorder.reset()
	second, err := cache.GetAccount(ctx, 321)
	require.NoError(t, err)
	require.Len(t, recorder.recorded(), 1, "one round trip")
	require.Equal(t, "a", second.Name)
	require.Equal(t, "dropped-by-projection", second.Credentials["access_token"])
	require.NotNil(t, second.LastUsedAt)
	require.True(t, second.LastUsedAt.Equal(usedAt))

	// 其它实例改写后读到新值。
	peer, _ := newSchedulerCachePeer(t, mr.Addr())
	require.NoError(t, peer.SetAccount(ctx, ptrAccount(memoTestAccount(321, "a2"))))
	third, err := cache.GetAccount(ctx, 321)
	require.NoError(t, err)
	require.Equal(t, "a2", third.Name)

	require.NoError(t, peer.DeleteAccount(ctx, 321))
	gone, err := cache.GetAccount(ctx, 321)
	require.NoError(t, err)
	require.Nil(t, gone)
	cache.memoMu.Lock()
	require.Empty(t, cache.payloadMemo)
	require.Zero(t, cache.payloadMemoBytes)
	cache.memoMu.Unlock()
}

func TestSchedulerCachePayloadMemoStaysWithinByteBudget(t *testing.T) {
	cache, _, _ := newSchedulerCacheMemoUnit(t, defaultSchedulerSnapshotWriteChunkSize)
	payload := make([]byte, schedulerAccountPayloadMemoMaxBytes/4)
	for i := 0; i < 16; i++ {
		cache.rememberAccountPayload(fmt.Sprint(i), payload)
	}
	cache.memoMu.Lock()
	defer cache.memoMu.Unlock()
	require.LessOrEqual(t, cache.payloadMemoBytes, schedulerAccountPayloadMemoMaxBytes)
	require.Len(t, cache.payloadMemo, 4)
	require.Contains(t, cache.payloadMemo, "15", "the newest record is kept")
}

// 两个实例处理同一批 outbox 事件：各自重建同一个 bucket。后到的实例不再重写任何账号记录，
// 两边读到的快照一致；之后任一实例的改动对另一实例立即可见。
func TestSchedulerCacheTwoInstancesShareOneRedis(t *testing.T) {
	ctx := context.Background()
	first, mr, firstRecorder := newSchedulerCacheMemoUnit(t, defaultSchedulerSnapshotWriteChunkSize)
	second, secondRecorder := newSchedulerCachePeer(t, mr.Addr())
	bucket := memoTestBucket(132)
	accounts := []service.Account{memoTestAccount(331, "a"), memoTestAccount(332, "b"), memoTestAccount(333, "c")}

	memoPublish(t, first, bucket, accounts...)
	secondRecorder.reset()
	memoPublish(t, second, bucket, accounts...)
	require.Zero(t, secondRecorder.countPrefix("set "+schedulerAccountPrefix), "the second instance resends no account record")
	require.Zero(t, secondRecorder.countPrefix("set "+schedulerAccountMetaPrefix))

	require.Equal(t, memoSummary(memoRead(t, first, bucket)), memoSummary(memoRead(t, second, bucket)))

	// 实例二把账号标记为不可调度，实例一下一次读取就能看到（修订号推进，不必等最长复用时长）。
	paused := memoTestAccount(332, "b")
	paused.Schedulable = false
	require.NoError(t, second.SetAccount(ctx, &paused))
	got := memoRead(t, first, bucket)
	require.False(t, got[1].Schedulable)
	hydrated, err := first.GetAccount(ctx, 332)
	require.NoError(t, err)
	require.False(t, hydrated.Schedulable)

	// 实例一恢复该账号：内容回到实例二曾经写过、也缓存过的样子，仍必须写入并对实例二可见。
	firstRecorder.reset()
	require.NoError(t, first.SetAccount(ctx, &accounts[1]))
	require.Equal(t, 1, firstRecorder.countPrefix("set "+schedulerAccountKey("332")))
	got = memoRead(t, second, bucket)
	require.True(t, got[1].Schedulable)

	// 并发：两个实例同时反复重建并各自改写不同账号，最终状态是各自最后一次写入。
	var wg sync.WaitGroup
	for index, cache := range []*schedulerCache{first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for round := 0; round < 20; round++ {
				account := memoTestAccount(int64(331+index), fmt.Sprintf("w%d-%d", index, round))
				if err := cache.SetAccount(ctx, &account); err != nil {
					t.Errorf("SetAccount: %v", err)
					return
				}
				token, err := cache.CaptureBucketWriteToken(ctx, bucket)
				if err != nil {
					t.Errorf("CaptureBucketWriteToken: %v", err)
					return
				}
				if _, err := cache.SetSnapshotAndReturnAccountIDs(ctx, bucket, token, []service.Account{
					account, memoTestAccount(333, "c"),
				}); err != nil {
					t.Errorf("SetSnapshot: %v", err)
					return
				}
				if _, _, err := cache.GetSnapshot(ctx, bucket); err != nil {
					t.Errorf("GetSnapshot: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	for _, cache := range []*schedulerCache{first, second} {
		a, err := cache.GetAccount(ctx, 331)
		require.NoError(t, err)
		require.Equal(t, "w0-19", a.Name)
		b, err := cache.GetAccount(ctx, 332)
		require.NoError(t, err)
		require.Equal(t, "w1-19", b.Name)
	}
	finalFirst, finalSecond := memoSummary(memoRead(t, first, bucket)), memoSummary(memoRead(t, second, bucket))
	require.Equal(t, finalFirst, finalSecond)
	require.Len(t, finalFirst, 2)
	require.Contains(t, []string{"331:w0-19:true", "332:w1-19:true"}, finalFirst[0])
	require.Equal(t, "333:c:true", finalFirst[1])
}

// memoSummary 取快照中与实例无关的可比较内容（账号对象内还带各自进程的懒加载缓存）。
func memoSummary(accounts []*service.Account) []string {
	out := make([]string, 0, len(accounts))
	for _, account := range accounts {
		out = append(out, fmt.Sprintf("%d:%s:%t", account.ID, account.Name, account.Schedulable))
	}
	return out
}
