package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	codexIdentityOwnerPrefix   = "openai_codex_identity_owner:"
	codexIdentityFencePrefix   = "openai_codex_identity_fence:"
	codexIdentityKindPrefix    = "openai_codex_identity_kind:"
	codexIdentityReverseSuffix = ":ownership"
)

var codexIdentityKinds = []string{
	"period_session", "thread_current", "thread_history", "side_session", "side_fork",
	"side_thread_current", "side_lifecycle", "legacy_v2", "legacy_v3",
}

// All index scores are the data key's absolute expiry, not the last GET time.
// A durable member uses an exactly representable sentinel beyond Unix times.
// Members carry their owner links so expiry cleanup does not depend on reverse
// metadata that has already expired. Index containers survive until their last
// member is pruned; expiring one container in isolation would strand members in
// another owner that also has durable side identities.
const codexIdentityIndexLua = `
local persistent = 9007199254740991
local clock = redis.call('TIME')
local now = tonumber(clock[1]) * 1000 + math.floor(tonumber(clock[2]) / 1000)
local function retain_index(key)
  redis.call('PERSIST', key)
end
local function remove_member(member)
  local ownership = cjson.decode(member)
  for i = 2, 4 do redis.call('ZREM', ownership[i], member) end
  return ownership[1]
end
local function prune_index(key)
  local expired = redis.call('ZRANGEBYSCORE', key, '-inf', now, 'LIMIT', 0, 256)
  for _, member in ipairs(expired) do remove_member(member) end
end
`

var codexOwnedIdentityScript = redis.NewScript(codexIdentityIndexLua + `
local function retired(key)
  local fence = redis.call('HMGET', key, 'cutoff', 'cleaning', 'retired')
  return fence[2] == '1' or fence[3] == '1' or (fence[1] ~= false and tonumber(ARGV[2]) <= tonumber(fence[1]))
end
if retired(KEYS[4]) or retired(KEYS[5]) then return {-1, '', 0} end
local current = redis.call('GET', KEYS[1])
if ARGV[1] == 'get' and current ~= false and ARGV[6] == '` + codexIdentityKindPrefix + `thread_history' and tonumber(ARGV[8]) > 0 and redis.call('PTTL', KEYS[1]) == -1 then
  local valid, record = pcall(cjson.decode, current)
  local observed = nil
  if valid and type(record) == 'table' then observed = tonumber(record.observed_at_ms) end
  if observed ~= nil then
    local deadline = observed + tonumber(ARGV[8])
    if deadline <= now then
      local previous_member = redis.call('HGET', KEYS[6], 'member')
      if previous_member ~= false then remove_member(previous_member) end
      remove_member(cjson.encode({KEYS[1], KEYS[2], KEYS[3], ARGV[6]}))
      redis.call('UNLINK', KEYS[1], KEYS[6])
      return {-2, '', 0}
    end
    redis.call('PEXPIREAT', KEYS[1], string.format('%.0f', deadline))
  end
end
local changed = 0
local write = false
if ARGV[1] == 'setnx' then
  write = current == false
elseif ARGV[1] == 'cas' then
  write = (ARGV[3] == '' and current == false) or (current ~= false and ARGV[3] ~= '' and current == ARGV[3])
end
if write then
  local absolute_expiry = tonumber(ARGV[7])
  if absolute_expiry > 0 and absolute_expiry <= now then return {-3, '', 0} end
  redis.call('SET', KEYS[1], ARGV[4])
  if absolute_expiry > 0 then
    redis.call('PEXPIREAT', KEYS[1], ARGV[7])
  elseif tonumber(ARGV[5]) > 0 then
    redis.call('PEXPIRE', KEYS[1], ARGV[5])
  end
  current = ARGV[4]
  changed = 1
end
if current == false then
  if ARGV[1] == 'get' then return {-2, '', 0} end
  return {0, '', 0}
end
local ttl = redis.call('PTTL', KEYS[1])
local deadline = persistent
if ttl >= 0 then deadline = now + ttl end
local kind_key = ARGV[6]
if kind_key == '` + codexIdentityKindPrefix + `thread_current' and ttl == -1 then
  kind_key = '` + codexIdentityKindPrefix + `side_thread_current'
end
local registered = 0
if redis.call('EXISTS', KEYS[6]) == 0 then registered = 1 end
local member = cjson.encode({KEYS[1], KEYS[2], KEYS[3], kind_key})
local previous_member = redis.call('HGET', KEYS[6], 'member')
if previous_member ~= false and previous_member ~= member then
  remove_member(previous_member)
end
redis.call('HSET', KEYS[6], 'member', member)
if ttl >= 0 then
  redis.call('PEXPIRE', KEYS[6], math.max(ttl, 1))
else
  redis.call('PERSIST', KEYS[6])
end
for _, key in ipairs({KEYS[2], KEYS[3], kind_key}) do
  prune_index(key)
  redis.call('ZADD', key, deadline, member)
  retain_index(key)
end
return {changed, current, registered}
`)

func codexIdentityKeyKind(key string, legacy bool) string {
	for _, kind := range []string{"thread-current", "thread-history", "side-fork", "side-lifecycle"} {
		if strings.HasPrefix(key, "v4:"+kind+":") {
			return strings.ReplaceAll(kind, "-", "_")
		}
	}
	if strings.HasPrefix(key, "v3:side-session:") {
		return "side_session"
	}
	if strings.HasPrefix(key, "v3:") {
		return "legacy_v3"
	}
	if legacy {
		return "legacy_v2"
	}
	return "period_session"
}

func (c *gatewayCache) codexOwnedIdentityOperation(ctx context.Context, owner service.CodexIdentityOwnership, operation, key, expected, value string, ttl time.Duration) (bool, string, error) {
	physical := openAICodexSessionIdentityPrefix + key
	ttlMilliseconds := ttl.Milliseconds()
	if ttl > 0 && ttlMilliseconds == 0 {
		ttlMilliseconds = 1
	}
	result, err := codexOwnedIdentityScript.Run(ctx, c.rdb, []string{
		physical, codexIdentityOwnerPrefix + owner.AccountOwner, codexIdentityOwnerPrefix + owner.UserOwner,
		codexIdentityFencePrefix + owner.AccountOwner, codexIdentityFencePrefix + owner.UserOwner,
		physical + codexIdentityReverseSuffix,
	}, operation, owner.ObservedAtMs, expected, value, ttlMilliseconds,
		codexIdentityKindPrefix+codexIdentityKeyKind(key, owner.Legacy), owner.ExpiresAtMs, owner.HistoryRetentionMs).Slice()
	if err != nil {
		return false, "", err
	}
	if len(result) != 3 {
		return false, "", errors.New("invalid Codex identity storage response")
	}
	code, ok := result[0].(int64)
	if !ok {
		return false, "", errors.New("invalid Codex identity storage status")
	}
	switch code {
	case -1:
		return false, "", service.ErrCodexIdentityOwnerRetired
	case -2:
		return false, "", service.ErrCodexSessionIdentityNotFound
	case -3:
		return false, "", errors.New("Codex identity expiry elapsed before storage")
	}
	if result[2] == int64(1) {
		service.RecordCodexIdentityEvent("ownership", "registered")
	}
	stored, ok := result[1].(string)
	if !ok {
		return false, "", errors.New("invalid Codex identity storage value")
	}
	return code == 1, stored, nil
}

var beginCodexIdentityCleanupScript = redis.NewScript(codexIdentityIndexLua + `
local cutoff = redis.call('HGET', KEYS[1], 'cutoff')
if cutoff ~= false then now = math.max(now, tonumber(cutoff)) end
redis.call('HSET', KEYS[1], 'cutoff', string.format('%.0f', now), 'cleaning', '1', 'retired', '1')
return 1
`)

var deleteCodexIdentityOwnerBatchScript = redis.NewScript(codexIdentityIndexLua + `
if redis.call('HGET', KEYS[2], 'cleaning') ~= '1' then return {0, 1} end
local members = redis.call('ZRANGE', KEYS[1], 0, 255)
local deleted = 0
for _, member in ipairs(members) do
  local ownership = cjson.decode(member)
  local physical = ownership[1]
  local reverse = physical .. '` + codexIdentityReverseSuffix + `'
  deleted = deleted + redis.call('UNLINK', physical)
  redis.call('UNLINK', reverse)
  remove_member(member)
end
if redis.call('ZCARD', KEYS[1]) == 0 then
  redis.call('DEL', KEYS[1])
  redis.call('HSET', KEYS[2], 'cleaning', '0')
  return {deleted, 1}
end
return {deleted, 0}
`)

// DeleteCodexIdentityOwner deletes only registered identities belonging to one
// account credential namespace or authenticated user. Each Lua batch is bounded;
// failures leave its fence and remaining index available for the deletion outbox
// to retry. No keyspace scan is needed. The small retirement fence remains durable
// to prevent stale auth/scheduler snapshots from recreating orphan identities.
// Only a database-confirmed live owner can explicitly reactivate the namespace.
func (c *gatewayCache) DeleteCodexIdentityOwner(ctx context.Context, owner string) (int64, error) {
	if c == nil || c.rdb == nil {
		return 0, errors.New("gateway cache unavailable")
	}
	owner = strings.TrimSpace(owner)
	if owner == "" || (!strings.HasPrefix(owner, "account:") && !strings.HasPrefix(owner, "user:") && !strings.HasPrefix(owner, "api-key:")) {
		return 0, errors.New("invalid Codex identity owner")
	}
	index := codexIdentityOwnerPrefix + owner
	fence := codexIdentityFencePrefix + owner
	if err := beginCodexIdentityCleanupScript.Run(ctx, c.rdb, []string{fence}).Err(); err != nil {
		service.RecordCodexIdentityEvent("cleanup", "error")
		return 0, err
	}
	return c.finishCodexIdentityOwnerCleanup(ctx, index, fence)
}

// ResumeCodexIdentityOwnerCleanup finishes an interrupted retirement without
// starting a new one. The caller can use it when a credential namespace has
// been reimported after cleanup began: complete the old fenced graph before
// explicit activation, while leaving any already healthy graph alone.
func (c *gatewayCache) ResumeCodexIdentityOwnerCleanup(ctx context.Context, owner string) (int64, error) {
	if c == nil || c.rdb == nil {
		return 0, errors.New("gateway cache unavailable")
	}
	owner = strings.TrimSpace(owner)
	if owner == "" || (!strings.HasPrefix(owner, "account:") && !strings.HasPrefix(owner, "user:") && !strings.HasPrefix(owner, "api-key:")) {
		return 0, errors.New("invalid Codex identity owner")
	}
	return c.finishCodexIdentityOwnerCleanup(ctx, codexIdentityOwnerPrefix+owner, codexIdentityFencePrefix+owner)
}

func (c *gatewayCache) finishCodexIdentityOwnerCleanup(ctx context.Context, index, fence string) (int64, error) {
	var deleted int64
	for {
		result, err := deleteCodexIdentityOwnerBatchScript.Run(ctx, c.rdb, []string{index, fence}).Int64Slice()
		if err != nil {
			service.RecordCodexIdentityEvent("cleanup", "error")
			return deleted, err
		}
		if len(result) != 2 {
			return deleted, errors.New("invalid Codex identity cleanup response")
		}
		deleted += result[0]
		if result[1] == 1 {
			if deleted > 0 {
				service.RecordCodexIdentityEvent("cleanup", "deleted")
			}
			return deleted, nil
		}
	}
}

var activateCodexIdentityOwnerScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then return 0 end
if redis.call('HGET', KEYS[1], 'cleaning') == '1' then return -1 end
if redis.call('HGET', KEYS[1], 'retired') == '0' then return 0 end
redis.call('HSET', KEYS[1], 'retired', '0')
return 1
`)

// ActivateCodexIdentityOwner is called only after the deletion outbox's database
// guard confirms the owner exists again. It never bypasses partial cleanup or
// forgets the old cutoff: pre-retirement in-flight requests remain rejected.
func (c *gatewayCache) ActivateCodexIdentityOwner(ctx context.Context, owner string) error {
	if c == nil || c.rdb == nil {
		return errors.New("gateway cache unavailable")
	}
	owner = strings.TrimSpace(owner)
	if owner == "" || (!strings.HasPrefix(owner, "account:") && !strings.HasPrefix(owner, "user:") && !strings.HasPrefix(owner, "api-key:")) {
		return errors.New("invalid Codex identity owner")
	}
	result, err := activateCodexIdentityOwnerScript.Run(ctx, c.rdb, []string{codexIdentityFencePrefix + owner}).Int()
	if err != nil {
		return err
	}
	if result == -1 {
		return service.ErrCodexIdentityOwnerRetired
	}
	if result == 1 {
		service.RecordCodexIdentityEvent("ownership", "advanced")
	}
	return nil
}

var countCodexIdentityKeysScript = redis.NewScript(codexIdentityIndexLua + `
local result = {}
for _, key in ipairs(KEYS) do
  prune_index(key)
  retain_index(key)
  table.insert(result, redis.call('ZCOUNT', key, '(' .. string.format('%.0f', now), '+inf'))
end
return result
`)

// CountCodexIdentityKeys reports live indexed keys, excluding unadopted legacy
// identities. Counting uses fixed per-kind indexes, never SCAN/KEYS, and never
// refreshes data TTL or observations. Redis expiry scores avoid per-key probes.
func (c *gatewayCache) CountCodexIdentityKeys(ctx context.Context) (map[string]int64, error) {
	if c == nil || c.rdb == nil {
		return nil, errors.New("gateway cache unavailable")
	}
	keys := make([]string, len(codexIdentityKinds))
	for i, kind := range codexIdentityKinds {
		keys[i] = codexIdentityKindPrefix + kind
	}
	counts, err := countCodexIdentityKeysScript.Run(ctx, c.rdb, keys).Int64Slice()
	if err != nil {
		return nil, err
	}
	if len(counts) != len(keys) {
		return nil, fmt.Errorf("invalid Codex identity key count response")
	}
	result := make(map[string]int64, len(keys))
	for i, kind := range codexIdentityKinds {
		result[kind] = counts[i]
	}
	return result, nil
}
