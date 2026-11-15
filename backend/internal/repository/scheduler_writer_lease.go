package repository

import (
	"context"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

// 调度后台写入者（全量重建 + outbox 消费）的选举键。
// 多实例共享同一份调度快照，同一时刻只需一个实例写入；其余实例只读。
const (
	schedulerBackgroundWriterLeaseKey     = "sched:bg-writer:lease"
	schedulerBackgroundWriterPreferredKey = "sched:bg-writer:preferred"
)

var (
	// 仅在仍由 owner 持有时续期，避免续上别人的租约。
	schedulerOwnedRenewScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
	redis.call('PEXPIRE', KEYS[1], ARGV[2])
	return 1
end
return 0
`)
	// 仅在仍由 owner 持有时删除，避免误删其它实例接手后的租约。
	schedulerOwnedReleaseScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
	return redis.call('DEL', KEYS[1])
end
return 0
`)
)

var _ service.SchedulerBackgroundWriterLease = (*schedulerCache)(nil)

func (c *schedulerCache) TryAcquireBackgroundWriter(ctx context.Context, owner string, ttl time.Duration) (bool, error) {
	return c.rdb.SetNX(ctx, schedulerBackgroundWriterLeaseKey, owner, ttl).Result()
}

func (c *schedulerCache) RenewBackgroundWriter(ctx context.Context, owner string, ttl time.Duration) (bool, error) {
	n, err := schedulerOwnedRenewScript.Run(ctx, c.rdb, []string{schedulerBackgroundWriterLeaseKey}, owner, ttl.Milliseconds()).Int64()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

func (c *schedulerCache) ReleaseBackgroundWriter(ctx context.Context, owner string) error {
	return schedulerOwnedReleaseScript.Run(ctx, c.rdb, []string{schedulerBackgroundWriterLeaseKey}, owner).Err()
}

func (c *schedulerCache) HeartbeatPreferredBackgroundWriter(ctx context.Context, owner string, ttl time.Duration) error {
	return c.rdb.Set(ctx, schedulerBackgroundWriterPreferredKey, owner, ttl).Err()
}

func (c *schedulerCache) ClearPreferredBackgroundWriter(ctx context.Context, owner string) error {
	return schedulerOwnedReleaseScript.Run(ctx, c.rdb, []string{schedulerBackgroundWriterPreferredKey}, owner).Err()
}

func (c *schedulerCache) PreferredBackgroundWriter(ctx context.Context) (string, error) {
	owner, err := c.rdb.Get(ctx, schedulerBackgroundWriterPreferredKey).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	return owner, err
}
