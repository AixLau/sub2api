package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	codexClientWhitelistCacheKey     = "codex:client:whitelist:v1"
	codexClientAccountStateKeyPrefix = "codex:client:account:"
)

type codexClientConfigCache struct {
	rdb *redis.Client
}

// NewCodexClientConfigCache creates the Redis-backed cache used for the
// per-account Codex-client support flag and the global client UA whitelist.
func NewCodexClientConfigCache(rdb *redis.Client) service.CodexClientConfigCache {
	return &codexClientConfigCache{rdb: rdb}
}

func codexClientAccountStateKey(accountID int64) string {
	return codexClientAccountStateKeyPrefix + strconv.FormatInt(accountID, 10)
}

func codexClientCacheTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return service.DefaultCodexClientConfigCacheTTL
	}
	return ttl
}

func (c *codexClientConfigCache) available() error {
	if c == nil || c.rdb == nil {
		return errors.New("codex client config cache unavailable")
	}
	return nil
}

func (c *codexClientConfigCache) GetCodexClientAccountState(ctx context.Context, accountID int64) (service.CodexClientAccountState, bool, error) {
	if err := c.available(); err != nil {
		return service.CodexClientAccountState{}, false, err
	}
	if accountID <= 0 {
		return service.CodexClientAccountState{}, false, errors.New("invalid account id")
	}
	raw, err := c.rdb.Get(ctx, codexClientAccountStateKey(accountID)).Bytes()
	if errors.Is(err, redis.Nil) {
		return service.CodexClientAccountState{}, false, nil
	}
	if err != nil {
		return service.CodexClientAccountState{}, false, err
	}
	var state service.CodexClientAccountState
	if err := json.Unmarshal(raw, &state); err != nil {
		return service.CodexClientAccountState{}, false, fmt.Errorf("decode codex client account state: %w", err)
	}
	return state, true, nil
}

func (c *codexClientConfigCache) SetCodexClientAccountState(ctx context.Context, accountID int64, state service.CodexClientAccountState, ttl time.Duration) error {
	if err := c.available(); err != nil {
		return err
	}
	if accountID <= 0 {
		return errors.New("invalid account id")
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return c.rdb.Set(ctx, codexClientAccountStateKey(accountID), payload, codexClientCacheTTL(ttl)).Err()
}

func (c *codexClientConfigCache) DeleteCodexClientAccountState(ctx context.Context, accountID int64) error {
	if err := c.available(); err != nil {
		return err
	}
	if accountID <= 0 {
		return nil
	}
	return c.rdb.Del(ctx, codexClientAccountStateKey(accountID)).Err()
}

func (c *codexClientConfigCache) GetCodexClientWhitelist(ctx context.Context) ([]openai.AllowedClientEntry, bool, error) {
	if err := c.available(); err != nil {
		return nil, false, err
	}
	raw, err := c.rdb.Get(ctx, codexClientWhitelistCacheKey).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var entries []openai.AllowedClientEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, false, fmt.Errorf("decode codex client whitelist: %w", err)
	}
	return entries, true, nil
}

func (c *codexClientConfigCache) SetCodexClientWhitelist(ctx context.Context, entries []openai.AllowedClientEntry, ttl time.Duration) error {
	if err := c.available(); err != nil {
		return err
	}
	if entries == nil {
		entries = []openai.AllowedClientEntry{}
	}
	payload, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	return c.rdb.Set(ctx, codexClientWhitelistCacheKey, payload, codexClientCacheTTL(ttl)).Err()
}

func (c *codexClientConfigCache) DeleteCodexClientWhitelist(ctx context.Context) error {
	if err := c.available(); err != nil {
		return err
	}
	return c.rdb.Del(ctx, codexClientWhitelistCacheKey).Err()
}
