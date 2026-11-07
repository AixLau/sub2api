//go:build unit

package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeLeaderLock 是跨“实例”共享的内存锁，模拟 Redis SETNX 语义。
type fakeLeaderLock struct {
	mu       sync.Mutex
	owners   map[string]string
	acquires int
	releases int
}

func newFakeLeaderLock() *fakeLeaderLock {
	return &fakeLeaderLock{owners: map[string]string{}}
}

func (f *fakeLeaderLock) TryAcquireLeaderLock(_ context.Context, key, owner string, _ time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, held := f.owners[key]; held {
		return false, nil
	}
	f.owners[key] = owner
	f.acquires++
	return true, nil
}

func (f *fakeLeaderLock) ReleaseLeaderLock(_ context.Context, key, owner string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.owners[key] == owner {
		delete(f.owners, key)
		f.releases++
	}
	return nil
}

func (f *fakeLeaderLock) heldBy(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.owners[key]
	return ok
}

func withFastRefreshLock(t *testing.T, maxWait time.Duration) {
	t.Helper()
	oldWait, oldPoll := oauthRefreshLockMaxWait, oauthRefreshLockPollInterval
	oauthRefreshLockMaxWait, oauthRefreshLockPollInterval = maxWait, 5*time.Millisecond
	t.Cleanup(func() { oauthRefreshLockMaxWait, oauthRefreshLockPollInterval = oldWait, oldPoll })
}

func TestRefreshIfNeeded_CrossInstanceLockAcquiredAndReleased(t *testing.T) {
	account := &Account{ID: 101, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive}
	repo := &refreshAPIAccountRepo{account: account}
	lock := newFakeLeaderLock()
	executor := &refreshAPIExecutorStub{needsRefresh: true, credentials: map[string]any{"access_token": "new"}}

	api := NewOAuthRefreshAPI(repo, nil)
	api.SetLeaderLock(lock, nil)

	result, err := api.RefreshIfNeeded(context.Background(), account, executor, time.Minute)

	require.NoError(t, err)
	require.True(t, result.Refreshed)
	require.Equal(t, 1, executor.refreshCalls)
	require.Equal(t, 1, lock.acquires)
	require.Equal(t, 1, lock.releases)
	require.False(t, lock.heldBy("oauth_refresh:account:101"), "lock must be released after refresh")
}

// 对端持锁期间本实例必须等待；对端释放后才获得锁并刷新（刷新前会重读 DB）。
func TestRefreshIfNeeded_CrossInstanceLockWaitsForPeer(t *testing.T) {
	withFastRefreshLock(t, 2*time.Second)
	account := &Account{ID: 102, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive}
	repo := &refreshAPIAccountRepo{account: account}
	lock := newFakeLeaderLock()
	_, _ = lock.TryAcquireLeaderLock(context.Background(), "oauth_refresh:account:102", "peer", time.Minute)

	var peerDone sync.WaitGroup
	peerDone.Add(1)
	go func() {
		defer peerDone.Done()
		time.Sleep(50 * time.Millisecond)
		_ = lock.ReleaseLeaderLock(context.Background(), "oauth_refresh:account:102", "peer")
	}()

	executor := &refreshAPIExecutorStub{needsRefresh: true, credentials: map[string]any{"access_token": "new"}}
	api := NewOAuthRefreshAPI(repo, nil)
	api.SetLeaderLock(lock, nil)

	start := time.Now()
	result, err := api.RefreshIfNeeded(context.Background(), account, executor, time.Minute)
	peerDone.Wait()

	require.NoError(t, err)
	require.GreaterOrEqual(t, time.Since(start), 40*time.Millisecond, "must wait while peer holds the lock")
	require.NotNil(t, result)
	require.Equal(t, 1, lock.releases-1, "own lock released exactly once (peer release not counted)")
}

// 对端一直不释放：超时后不刷新、不报错，返回 DB 中的最新账号。
func TestRefreshIfNeeded_CrossInstanceLockTimeoutSkipsRefresh(t *testing.T) {
	withFastRefreshLock(t, 60*time.Millisecond)
	account := &Account{ID: 103, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive}
	repo := &refreshAPIAccountRepo{account: account}
	lock := newFakeLeaderLock()
	_, _ = lock.TryAcquireLeaderLock(context.Background(), "oauth_refresh:account:103", "peer", time.Minute)

	executor := &refreshAPIExecutorStub{needsRefresh: true, credentials: map[string]any{"access_token": "new"}}
	api := NewOAuthRefreshAPI(repo, nil)
	api.SetLeaderLock(lock, nil)

	result, err := api.RefreshIfNeeded(context.Background(), account, executor, time.Minute)

	require.NoError(t, err)
	require.False(t, result.Refreshed)
	require.NotNil(t, result.Account)
	require.Equal(t, 0, executor.refreshCalls, "must not refresh while a peer holds the lock")
	require.Equal(t, 0, repo.updateCalls)
	require.True(t, lock.heldBy("oauth_refresh:account:103"), "peer's lock must be untouched")
}

// 不同账号互不阻塞。
func TestRefreshIfNeeded_CrossInstanceLockIsPerAccount(t *testing.T) {
	withFastRefreshLock(t, 60*time.Millisecond)
	account := &Account{ID: 104, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive}
	repo := &refreshAPIAccountRepo{account: account}
	lock := newFakeLeaderLock()
	_, _ = lock.TryAcquireLeaderLock(context.Background(), "oauth_refresh:account:999", "peer", time.Minute)

	executor := &refreshAPIExecutorStub{needsRefresh: true, credentials: map[string]any{"access_token": "new"}}
	api := NewOAuthRefreshAPI(repo, nil)
	api.SetLeaderLock(lock, nil)

	result, err := api.RefreshIfNeeded(context.Background(), account, executor, time.Minute)

	require.NoError(t, err)
	require.True(t, result.Refreshed)
	require.Equal(t, 1, executor.refreshCalls)
}
