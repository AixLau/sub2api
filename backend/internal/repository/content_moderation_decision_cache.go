package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	contentModerationDecisionCacheSchemaVersion = 1
	contentModerationDecisionCachePrefix        = "moderation:decision:v1:"
	contentModerationDecisionLockPrefix         = "moderation:decision-lock:v1:"
)

type contentModerationDecisionCache struct {
	rdb *redis.Client
}

var contentModerationDecisionReleaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`)

var contentModerationDecisionRenewScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("PEXPIRE", KEYS[1], ARGV[2])
end
return 0
`)

func NewContentModerationDecisionCache(rdb *redis.Client) service.ContentModerationDecisionCache {
	if rdb == nil {
		return nil
	}
	return &contentModerationDecisionCache{rdb: rdb}
}

func contentModerationDecisionCacheKey(key string) string {
	return contentModerationDecisionCachePrefix + strings.TrimSpace(key)
}

func contentModerationDecisionLockKey(key string) string {
	return contentModerationDecisionLockPrefix + strings.TrimSpace(key)
}

func (c *contentModerationDecisionCache) Get(ctx context.Context, key string) (*service.ContentModerationCachedDecision, error) {
	if c == nil || c.rdb == nil || strings.TrimSpace(key) == "" {
		return nil, nil
	}
	raw, err := c.rdb.Get(ctx, contentModerationDecisionCacheKey(key)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get moderation decision cache: %w", err)
	}
	var entry service.ContentModerationCachedDecision
	if err := decodeStrictJSON(raw, &entry); err != nil {
		return nil, fmt.Errorf("decode moderation decision cache: %w", err)
	}
	if entry.SchemaVersion != contentModerationDecisionCacheSchemaVersion || entry.ExpiresAt <= time.Now().Unix() {
		return nil, nil
	}
	return &entry, nil
}

func (c *contentModerationDecisionCache) TryAcquire(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	if c == nil || c.rdb == nil || strings.TrimSpace(key) == "" || strings.TrimSpace(owner) == "" {
		return false, nil
	}
	if ttl <= 0 {
		ttl = time.Second
	}
	ok, err := c.rdb.SetNX(ctx, contentModerationDecisionLockKey(key), owner, ttl).Result()
	if err != nil {
		return false, fmt.Errorf("acquire moderation decision lock: %w", err)
	}
	return ok, nil
}

func (c *contentModerationDecisionCache) Renew(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	if c == nil || c.rdb == nil || strings.TrimSpace(key) == "" || strings.TrimSpace(owner) == "" {
		return false, fmt.Errorf("renew moderation decision lock: cache unavailable")
	}
	if ttl <= 0 {
		ttl = time.Second
	}
	result, err := contentModerationDecisionRenewScript.Run(ctx, c.rdb, []string{contentModerationDecisionLockKey(key)}, owner, ttl.Milliseconds()).Int64()
	if err != nil {
		return false, fmt.Errorf("renew moderation decision lock: %w", err)
	}
	return result == 1, nil
}

func (c *contentModerationDecisionCache) Store(ctx context.Context, key string, entry service.ContentModerationCachedDecision, ttl time.Duration) error {
	if c == nil || c.rdb == nil || strings.TrimSpace(key) == "" {
		return nil
	}
	if ttl <= 0 {
		return nil
	}
	entry.SchemaVersion = contentModerationDecisionCacheSchemaVersion
	entry.ExpiresAt = time.Now().Add(ttl).Unix()
	raw, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal moderation decision cache: %w", err)
	}
	if err := c.rdb.Set(ctx, contentModerationDecisionCacheKey(key), raw, ttl).Err(); err != nil {
		return fmt.Errorf("store moderation decision cache: %w", err)
	}
	return nil
}

func (c *contentModerationDecisionCache) Release(ctx context.Context, key, owner string) error {
	if c == nil || c.rdb == nil || strings.TrimSpace(key) == "" || strings.TrimSpace(owner) == "" {
		return nil
	}
	if err := contentModerationDecisionReleaseScript.Run(ctx, c.rdb, []string{contentModerationDecisionLockKey(key)}, owner).Err(); err != nil {
		return fmt.Errorf("release moderation decision lock: %w", err)
	}
	return nil
}
