package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	stickySessionPrefix       = "sticky_session:"
	userAccountCooldownPrefix = "user_account_cooldown:"
)

type gatewayCache struct {
	rdb *redis.Client
}

func NewGatewayCache(rdb *redis.Client) service.GatewayCache {
	return &gatewayCache{rdb: rdb}
}

// buildSessionKey 构建 session key，包含 groupID 实现分组隔离
// 格式: sticky_session:{groupID}:{sessionHash}
func buildSessionKey(groupID int64, sessionHash string) string {
	return fmt.Sprintf("%s%d:%s", stickySessionPrefix, groupID, sessionHash)
}

func buildUserAccountCooldownSetKey(userID int64) string {
	return fmt.Sprintf("%s%d", userAccountCooldownPrefix, userID)
}

func buildUserAccountCooldownKey(userID, accountID int64) string {
	return fmt.Sprintf("%s%d:%d", userAccountCooldownPrefix, userID, accountID)
}

func (c *gatewayCache) GetSessionAccountID(ctx context.Context, groupID int64, sessionHash string) (int64, error) {
	key := buildSessionKey(groupID, sessionHash)
	return c.rdb.Get(ctx, key).Int64()
}

func (c *gatewayCache) SetSessionAccountID(ctx context.Context, groupID int64, sessionHash string, accountID int64, ttl time.Duration) error {
	key := buildSessionKey(groupID, sessionHash)
	return c.rdb.Set(ctx, key, accountID, ttl).Err()
}

func (c *gatewayCache) RefreshSessionTTL(ctx context.Context, groupID int64, sessionHash string, ttl time.Duration) error {
	key := buildSessionKey(groupID, sessionHash)
	return c.rdb.Expire(ctx, key, ttl).Err()
}

// DeleteSessionAccountID 删除粘性会话与账号的绑定关系。
// 当检测到绑定的账号不可用（如状态错误、禁用、不可调度等）时调用，
// 以便下次请求能够重新选择可用账号。
//
// DeleteSessionAccountID removes the sticky session binding for the given session.
// Called when the bound account becomes unavailable (e.g., error status, disabled,
// or unschedulable), allowing subsequent requests to select a new available account.
func (c *gatewayCache) DeleteSessionAccountID(ctx context.Context, groupID int64, sessionHash string) error {
	key := buildSessionKey(groupID, sessionHash)
	return c.rdb.Del(ctx, key).Err()
}

func (c *gatewayCache) SetUserAccountCooldown(ctx context.Context, userID, accountID int64, ttl time.Duration) error {
	setKey := buildUserAccountCooldownSetKey(userID)
	itemKey := buildUserAccountCooldownKey(userID, accountID)
	pipe := c.rdb.Pipeline()
	pipe.SAdd(ctx, setKey, accountID)
	pipe.Expire(ctx, setKey, ttl)
	pipe.Set(ctx, itemKey, "1", ttl)
	_, err := pipe.Exec(ctx)
	return err
}

func (c *gatewayCache) GetUserAccountCooldowns(ctx context.Context, userID int64) (map[int64]struct{}, error) {
	setKey := buildUserAccountCooldownSetKey(userID)
	ids, err := c.rdb.SMembers(ctx, setKey).Result()
	if err != nil {
		return nil, err
	}
	result := make(map[int64]struct{}, len(ids))
	for _, rawID := range ids {
		var accountID int64
		if _, scanErr := fmt.Sscanf(rawID, "%d", &accountID); scanErr != nil || accountID <= 0 {
			_ = c.rdb.SRem(ctx, setKey, rawID).Err()
			continue
		}
		exists, existsErr := c.rdb.Exists(ctx, buildUserAccountCooldownKey(userID, accountID)).Result()
		if existsErr != nil {
			return nil, existsErr
		}
		if exists == 0 {
			_ = c.rdb.SRem(ctx, setKey, rawID).Err()
			continue
		}
		result[accountID] = struct{}{}
	}
	return result, nil
}

// Compile-time assertion: gatewayCache must implement CyberSessionBlockStore.
var _ service.CyberSessionBlockStore = (*gatewayCache)(nil)

const cyberSessionBlockPrefix = "cyber_session_block:"

// SetCyberSessionBlocked 把被 cyber_policy 命中的会话写入屏蔽表（TTL 自动过期）。
// 存储值 "1" 作为存在标记（IsCyberSessionBlocked 只检查 key 是否存在，不读值）。
func (c *gatewayCache) SetCyberSessionBlocked(ctx context.Context, key string, ttl time.Duration) error {
	return c.rdb.Set(ctx, cyberSessionBlockPrefix+key, "1", ttl).Err()
}

// IsCyberSessionBlocked 查询会话是否在屏蔽表中。
func (c *gatewayCache) IsCyberSessionBlocked(ctx context.Context, key string) (bool, error) {
	n, err := c.rdb.Exists(ctx, cyberSessionBlockPrefix+key).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
