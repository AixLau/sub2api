package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

func httpAccountStateKey(groupID, userID int64, digest string) string {
	return fmt.Sprintf("openai:http-state:%d:%d:%s", groupID, userID, digest)
}

func (c *gatewayCache) GetHTTPAccountStateOwner(ctx context.Context, groupID, userID int64, digest string) (int64, error) {
	id, err := c.rdb.Get(ctx, httpAccountStateKey(groupID, userID, digest)).Int64()
	if errors.Is(err, redis.Nil) {
		return 0, service.ErrStickySessionNotFound
	}
	return id, err
}

var bindHTTPAccountStateOwner = redis.NewScript(`
local owner = redis.call('GET', KEYS[1])
if owner and owner ~= ARGV[1] then return 0 end
redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2])
return 1
`)

func (c *gatewayCache) BindHTTPAccountStateOwner(ctx context.Context, groupID, userID int64, digest string, accountID int64, ttl time.Duration) error {
	ok, err := bindHTTPAccountStateOwner.Run(ctx, c.rdb, []string{httpAccountStateKey(groupID, userID, digest)}, accountID, ttl.Milliseconds()).Int()
	if err != nil {
		return err
	}
	if ok != 1 {
		return service.ErrOpenAIHTTPStateOwnerConflict
	}
	return nil
}

var _ service.OpenAIHTTPAccountStateCache = (*gatewayCache)(nil)
