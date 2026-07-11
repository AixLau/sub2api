package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	moderationCacheSchemaVersion = 1
	defaultModerationCacheTTL    = 24 * time.Hour
	minModerationCacheTTL        = time.Minute
	maxModerationCacheTTL        = 30 * 24 * time.Hour
	comparisonMetadataTTL        = 30 * 24 * time.Hour
	maxComparisonChunkKeys       = 64
	maxComparisonRiskTypes       = 32
	maxComparisonMetadataBytes   = 32 * 1024
)

type contentModerationPassValue struct {
	SchemaVersion int   `json:"schema_version"`
	ExpiresAt     int64 `json:"expires_at"`
}

type contentModerationPassCache struct{ rdb *redis.Client }

func decodeStrictJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return fmt.Errorf("unexpected trailing moderation cache data")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func NewContentModerationPassCache(rdb *redis.Client) service.ContentModerationPassCache {
	return &contentModerationPassCache{rdb: rdb}
}

func moderationTTL(ttl time.Duration) time.Duration {
	if ttl == 0 {
		return defaultModerationCacheTTL
	}
	if ttl < minModerationCacheTTL {
		return minModerationCacheTTL
	}
	if ttl > maxModerationCacheTTL {
		return maxModerationCacheTTL
	}
	return ttl
}

func moderationCacheKey(kind string, version uint64, digest string) string {
	return fmt.Sprintf("moderation:%s:v1:%d:%s", kind, version, strings.TrimSpace(digest))
}

func (c *contentModerationPassCache) LookupPASS(ctx context.Context, opts service.ContentModerationPassCacheOptions, keys []string) (map[string]bool, error) {
	if !opts.Enabled || c == nil || c.rdb == nil || len(keys) == 0 {
		return map[string]bool{}, nil
	}
	cmds := make([]*redis.StringCmd, 0, len(keys))
	pipe := c.rdb.Pipeline()
	for _, key := range keys {
		cmds = append(cmds, pipe.Get(ctx, moderationCacheKey("pass", opts.KeyVersion, key)))
	}
	execCmds, execErr := pipe.Exec(ctx)
	if execErr != nil && !errors.Is(execErr, redis.Nil) {
		return map[string]bool{}, execErr
	}
	if len(execCmds) != len(keys) || len(cmds) != len(keys) {
		return map[string]bool{}, fmt.Errorf("moderation pass pipeline reply count: got %d want %d", len(execCmds), len(keys))
	}
	now := time.Now().Unix()
	hits := make(map[string]bool, len(keys))
	for i, cmd := range cmds {
		value, err := cmd.Result()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			return map[string]bool{}, err
		}
		var decoded contentModerationPassValue
		if err := decodeStrictJSON([]byte(value), &decoded); err != nil {
			return map[string]bool{}, err
		}
		if decoded.SchemaVersion != moderationCacheSchemaVersion || decoded.ExpiresAt <= now {
			return map[string]bool{}, fmt.Errorf("invalid moderation pass cache value")
		}
		hits[keys[i]] = true
	}
	return hits, nil
}

func (c *contentModerationPassCache) StorePASS(ctx context.Context, opts service.ContentModerationPassCacheOptions, keys []string) {
	if !opts.Enabled || c == nil || c.rdb == nil || len(keys) == 0 {
		return
	}
	ttl := moderationTTL(opts.TTL)
	data, err := json.Marshal(contentModerationPassValue{SchemaVersion: moderationCacheSchemaVersion, ExpiresAt: time.Now().Add(ttl).Unix()})
	if err != nil {
		return
	}
	pipe := c.rdb.Pipeline()
	for _, key := range keys {
		pipe.Set(ctx, moderationCacheKey("pass", opts.KeyVersion, key), data, ttl)
	}
	_, _ = pipe.Exec(ctx)
}

func (c *contentModerationPassCache) DeletePASS(ctx context.Context, opts service.ContentModerationPassCacheOptions, keys []string) {
	if !opts.Enabled || c == nil || c.rdb == nil || len(keys) == 0 {
		return
	}
	redisKeys := make([]string, 0, len(keys))
	for _, key := range keys {
		redisKeys = append(redisKeys, moderationCacheKey("pass", opts.KeyVersion, key))
	}
	_ = c.rdb.Del(ctx, redisKeys...).Err()
}

func (c *contentModerationPassCache) LookupQuarantine(ctx context.Context, opts service.ContentModerationPassCacheOptions, keys []string) (map[string]service.ContentModerationQuarantineEntry, error) {
	if !opts.Enabled || c == nil || c.rdb == nil || len(keys) == 0 {
		return map[string]service.ContentModerationQuarantineEntry{}, nil
	}
	cmds := make([]*redis.StringCmd, 0, len(keys))
	pipe := c.rdb.Pipeline()
	for _, key := range keys {
		cmds = append(cmds, pipe.Get(ctx, moderationCacheKey("quarantine", opts.KeyVersion, key)))
	}
	execCmds, execErr := pipe.Exec(ctx)
	if execErr != nil && !errors.Is(execErr, redis.Nil) {
		return map[string]service.ContentModerationQuarantineEntry{}, execErr
	}
	if len(execCmds) != len(keys) || len(cmds) != len(keys) {
		return map[string]service.ContentModerationQuarantineEntry{}, fmt.Errorf("moderation quarantine pipeline reply count: got %d want %d", len(execCmds), len(keys))
	}
	now := time.Now().Unix()
	result := make(map[string]service.ContentModerationQuarantineEntry, len(keys))
	for i, cmd := range cmds {
		value, err := cmd.Result()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			return map[string]service.ContentModerationQuarantineEntry{}, err
		}
		var entry service.ContentModerationQuarantineEntry
		if err := decodeStrictJSON([]byte(value), &entry); err != nil {
			return map[string]service.ContentModerationQuarantineEntry{}, err
		}
		if entry.SchemaVersion != moderationCacheSchemaVersion || entry.ExpiresAt <= now {
			return map[string]service.ContentModerationQuarantineEntry{}, fmt.Errorf("invalid moderation quarantine value")
		}
		result[keys[i]] = entry
	}
	return result, nil
}

func (c *contentModerationPassCache) StoreQuarantine(ctx context.Context, opts service.ContentModerationPassCacheOptions, entries map[string]service.ContentModerationQuarantineEntry) error {
	if !opts.Enabled || c == nil || c.rdb == nil || len(entries) == 0 {
		return nil
	}
	ttl := moderationTTL(opts.TTL)
	pipe := c.rdb.Pipeline()
	for key, entry := range entries {
		entry.SchemaVersion = moderationCacheSchemaVersion
		entry.ExpiresAt = time.Now().Add(ttl).Unix()
		data, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		pipe.Set(ctx, moderationCacheKey("quarantine", opts.KeyVersion, key), data, ttl)
	}
	cmds, err := pipe.Exec(ctx)
	if err != nil {
		return err
	}
	if len(cmds) != len(entries) {
		return fmt.Errorf("moderation quarantine write count: got %d want %d", len(cmds), len(entries))
	}
	return nil
}

func (c *contentModerationPassCache) DeleteQuarantine(ctx context.Context, opts service.ContentModerationPassCacheOptions, keys []string) error {
	if !opts.Enabled || c == nil || c.rdb == nil || len(keys) == 0 {
		return nil
	}
	redisKeys := make([]string, 0, len(keys))
	for _, key := range keys {
		redisKeys = append(redisKeys, moderationCacheKey("quarantine", opts.KeyVersion, key))
	}
	return c.rdb.Del(ctx, redisKeys...).Err()
}

func comparisonKey(correlationID string) string {
	return "moderation:comparison:v1:" + strings.TrimSpace(correlationID)
}

func (c *contentModerationPassCache) GetComparisonMetadata(ctx context.Context, correlationID string) (*service.ContentModerationComparisonMetadata, error) {
	if c == nil || c.rdb == nil || strings.TrimSpace(correlationID) == "" {
		return nil, nil
	}
	data, err := c.rdb.Get(ctx, comparisonKey(correlationID)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var metadata service.ContentModerationComparisonMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, err
	}
	if err := validateComparisonMetadata(metadata, len(data)); err != nil {
		return nil, err
	}
	return &metadata, nil
}

func validateComparisonMetadata(metadata service.ContentModerationComparisonMetadata, encodedBytes int) error {
	if len(metadata.ChunkKeys) > maxComparisonChunkKeys || len(metadata.RiskTypes) > maxComparisonRiskTypes || encodedBytes > maxComparisonMetadataBytes {
		return fmt.Errorf("moderation comparison metadata exceeds bounds")
	}
	return nil
}

func (c *contentModerationPassCache) StoreComparisonMetadata(ctx context.Context, correlationID string, metadata service.ContentModerationComparisonMetadata) error {
	if c == nil || c.rdb == nil || strings.TrimSpace(correlationID) == "" {
		return nil
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	if err := validateComparisonMetadata(metadata, len(data)); err != nil {
		return err
	}
	return c.rdb.Set(ctx, comparisonKey(correlationID), data, comparisonMetadataTTL).Err()
}

func (c *contentModerationPassCache) DeleteComparisonMetadata(ctx context.Context, correlationID string) error {
	if c == nil || c.rdb == nil || strings.TrimSpace(correlationID) == "" {
		return nil
	}
	return c.rdb.Del(ctx, comparisonKey(correlationID)).Err()
}
