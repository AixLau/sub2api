//go:build unit

package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// fakeWriterLeaseStore 模拟多个实例共享的 Redis 租约状态（带过期）。
type fakeWriterLeaseStore struct {
	mu             sync.Mutex
	now            time.Time
	lease          string
	leaseExp       time.Time
	preferred      string
	preferredExp   time.Time
	err            error
	acquireAttempt int
}

func (s *fakeWriterLeaseStore) advance(d time.Duration) {
	s.mu.Lock()
	s.now = s.now.Add(d)
	s.mu.Unlock()
}

func (s *fakeWriterLeaseStore) clock() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.now
}

func (s *fakeWriterLeaseStore) expireLocked() {
	if s.lease != "" && !s.now.Before(s.leaseExp) {
		s.lease = ""
	}
	if s.preferred != "" && !s.now.Before(s.preferredExp) {
		s.preferred = ""
	}
}

func (s *fakeWriterLeaseStore) TryAcquireBackgroundWriter(_ context.Context, owner string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acquireAttempt++
	if s.err != nil {
		return false, s.err
	}
	s.expireLocked()
	if s.lease != "" {
		return false, nil
	}
	s.lease, s.leaseExp = owner, s.now.Add(ttl)
	return true, nil
}

func (s *fakeWriterLeaseStore) RenewBackgroundWriter(_ context.Context, owner string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return false, s.err
	}
	s.expireLocked()
	if s.lease != owner {
		return false, nil
	}
	s.leaseExp = s.now.Add(ttl)
	return true, nil
}

func (s *fakeWriterLeaseStore) ReleaseBackgroundWriter(_ context.Context, owner string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	if s.lease == owner {
		s.lease = ""
	}
	return nil
}

func (s *fakeWriterLeaseStore) HeartbeatPreferredBackgroundWriter(_ context.Context, owner string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.preferred, s.preferredExp = owner, s.now.Add(ttl)
	return nil
}

func (s *fakeWriterLeaseStore) ClearPreferredBackgroundWriter(_ context.Context, owner string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.preferred == owner {
		s.preferred = ""
	}
	return nil
}

func (s *fakeWriterLeaseStore) PreferredBackgroundWriter(context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return "", s.err
	}
	s.expireLocked()
	return s.preferred, nil
}

func newTestWriter(store *fakeWriterLeaseStore, owner string, preferred bool, acquired *int) *schedulerBackgroundWriter {
	w := newSchedulerBackgroundWriter(store, owner, preferred, 30*time.Second, func() {
		if acquired != nil {
			*acquired++
		}
	})
	w.now = store.clock
	return w
}

func TestSchedulerBackgroundWriterSingleWriterAmongAutoInstances(t *testing.T) {
	ctx := context.Background()
	store := &fakeWriterLeaseStore{now: time.Unix(1000, 0)}
	var acquiredA, acquiredB int
	a := newTestWriter(store, "a", false, &acquiredA)
	b := newTestWriter(store, "b", false, &acquiredB)

	a.tick(ctx)
	b.tick(ctx)
	require.True(t, a.IsWriter())
	require.False(t, b.IsWriter())
	require.Equal(t, 1, acquiredA)
	require.Equal(t, 0, acquiredB)

	// 持有者持续续期，另一个实例始终拿不到。
	for i := 0; i < 5; i++ {
		store.advance(10 * time.Second)
		a.tick(ctx)
		b.tick(ctx)
	}
	require.True(t, a.IsWriter())
	require.False(t, b.IsWriter())
	require.Equal(t, 1, acquiredA, "renewals must not re-trigger the acquisition rebuild")
}

func TestSchedulerBackgroundWriterFailoverAfterHolderStops(t *testing.T) {
	ctx := context.Background()
	store := &fakeWriterLeaseStore{now: time.Unix(1000, 0)}
	var acquiredB int
	a := newTestWriter(store, "a", false, nil)
	b := newTestWriter(store, "b", false, &acquiredB)

	a.tick(ctx)
	require.True(t, a.IsWriter())

	// a 宕机不再续期：租约过期后 b 接手。
	store.advance(29 * time.Second)
	b.tick(ctx)
	require.False(t, b.IsWriter())
	store.advance(2 * time.Second)
	b.tick(ctx)
	require.True(t, b.IsWriter())
	require.Equal(t, 1, acquiredB)

	// 正常退出则立即释放。
	b.release(ctx)
	require.False(t, b.IsWriter())
	a.tick(ctx)
	require.True(t, a.IsWriter())
}

func TestSchedulerBackgroundWriterAutoYieldsToPreferred(t *testing.T) {
	ctx := context.Background()
	store := &fakeWriterLeaseStore{now: time.Unix(1000, 0)}
	var acquiredP int
	auto := newTestWriter(store, "auto", false, nil)
	pref := newTestWriter(store, "pref", true, &acquiredP)

	auto.tick(ctx)
	require.True(t, auto.IsWriter())

	// 指定写入者上线：先声明，此时租约仍被 auto 持有。
	pref.tick(ctx)
	require.False(t, pref.IsWriter())

	// auto 下一轮发现存活的指定写入者，主动让出。
	auto.tick(ctx)
	require.False(t, auto.IsWriter())

	pref.tick(ctx)
	require.True(t, pref.IsWriter())
	require.Equal(t, 1, acquiredP)

	// 指定写入者存活期间，auto 不再竞争。
	store.advance(10 * time.Second)
	attempts := store.acquireAttempt
	auto.tick(ctx)
	require.False(t, auto.IsWriter())
	require.Equal(t, attempts, store.acquireAttempt)

	// 指定写入者退出后，auto 接手。
	pref.release(ctx)
	auto.tick(ctx)
	require.True(t, auto.IsWriter())
}

func TestSchedulerBackgroundWriterPreferredCrashFallsBackToAuto(t *testing.T) {
	ctx := context.Background()
	store := &fakeWriterLeaseStore{now: time.Unix(1000, 0)}
	auto := newTestWriter(store, "auto", false, nil)
	pref := newTestWriter(store, "pref", true, nil)

	pref.tick(ctx)
	require.True(t, pref.IsWriter())

	// 指定写入者崩溃（不释放）：声明与租约都过期后 auto 接手。
	store.advance(31 * time.Second)
	auto.tick(ctx)
	require.True(t, auto.IsWriter())
}

func TestSchedulerBackgroundWriterDropsWriteRightWhenRenewKeepsFailing(t *testing.T) {
	ctx := context.Background()
	store := &fakeWriterLeaseStore{now: time.Unix(1000, 0)}
	w := newTestWriter(store, "a", false, nil)
	w.tick(ctx)
	require.True(t, w.IsWriter())

	store.err = errors.New("redis unreachable")
	store.advance(10 * time.Second)
	w.tick(ctx)
	require.True(t, w.IsWriter(), "a transient renew error within the lease keeps the write right")

	store.advance(21 * time.Second)
	require.False(t, w.IsWriter(), "the lease has expired in Redis; another instance may own it")
	w.tick(ctx)
	require.False(t, w.IsWriter())
}

type leaseOutboxCache struct {
	*outboxCleanupCache
	*fakeWriterLeaseStore
}

func TestSchedulerSnapshotServiceOutboxOnlyPolledByWriter(t *testing.T) {
	newSvc := func(mode string, store *fakeWriterLeaseStore) (*SchedulerSnapshotService, *outboxCleanupCache) {
		cache := &outboxCleanupCache{}
		var schedCache SchedulerCache = cache
		if store != nil {
			schedCache = leaseOutboxCache{outboxCleanupCache: cache, fakeWriterLeaseStore: store}
		}
		repo := &outboxCleanupRepo{events: []SchedulerOutboxEvent{{ID: 7, EventType: "noop"}}}
		cfg := &config.Config{}
		cfg.Gateway.Scheduling.BackgroundWriterMode = mode
		svc := NewSchedulerSnapshotService(schedCache, repo, nil, nil, cfg)
		svc.setupBackgroundWriter()
		return svc, cache
	}

	t.Run("disabled never polls", func(t *testing.T) {
		svc, cache := newSvc("disabled", &fakeWriterLeaseStore{now: time.Unix(1000, 0)})
		require.False(t, svc.isBackgroundWriter())
		require.Nil(t, svc.backgroundWriter)
		_ = cache
	})

	t.Run("non-holder skips, holder polls", func(t *testing.T) {
		store := &fakeWriterLeaseStore{now: time.Unix(1000, 0), lease: "someone-else", leaseExp: time.Unix(2000, 0)}
		svc, cache := newSvc("auto", store)
		require.NotNil(t, svc.backgroundWriter)
		svc.backgroundWriter.now = store.clock
		svc.backgroundWriter.tick(context.Background())
		require.False(t, svc.isBackgroundWriter())

		store.advance(1001 * time.Second)
		svc.backgroundWriter.tick(context.Background())
		require.True(t, svc.isBackgroundWriter())
		svc.pollOutbox()
		require.Equal(t, []int64{7}, cache.setWatermarks)
		select {
		case <-svc.writerAcquiredCh:
		default:
			t.Fatal("acquiring the write right must request a full rebuild")
		}
	})

	t.Run("cache without lease keeps legacy behaviour", func(t *testing.T) {
		svc, _ := newSvc("auto", nil)
		require.Nil(t, svc.backgroundWriter)
		require.True(t, svc.isBackgroundWriter())
	})
}

func TestSchedulerBackgroundWriterModeParsing(t *testing.T) {
	cfg := &config.Config{}
	require.Equal(t, SchedulerBackgroundWriterModeAuto, schedulerBackgroundWriterMode(cfg))
	cfg.Gateway.Scheduling.BackgroundWriterMode = " Preferred "
	require.Equal(t, SchedulerBackgroundWriterModePreferred, schedulerBackgroundWriterMode(cfg))
	cfg.Gateway.Scheduling.BackgroundWriterMode = "disabled"
	require.Equal(t, SchedulerBackgroundWriterModeDisabled, schedulerBackgroundWriterMode(cfg))
	require.Equal(t, defaultSchedulerBackgroundWriterLease, schedulerBackgroundWriterLeaseTTL(cfg))
	cfg.Gateway.Scheduling.InstanceID = "lax-222"
	require.Contains(t, schedulerBackgroundWriterOwner(cfg), "lax-222-")
	require.NotEqual(t, schedulerBackgroundWriterOwner(cfg), schedulerBackgroundWriterOwner(cfg))
}
