package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type incrementalClientFactoryStub struct{ client *http.Client }

func (f incrementalClientFactoryStub) Client(string, time.Duration) (*http.Client, error) {
	return f.client, nil
}

type incrementalEpochStub struct{ value uint64 }

func (e incrementalEpochStub) GetModerationFeedbackEpoch(context.Context) (uint64, error) {
	return e.value, nil
}
func (e incrementalEpochStub) IncrementModerationFeedbackEpoch(context.Context) (uint64, error) {
	return e.value + 1, nil
}

type incrementalPassCacheStub struct {
	mu         sync.Mutex
	pass       map[string]bool
	lookupErr  error
	lookups    int
	stores     int
	metadata   *ContentModerationComparisonMetadata
	deleted    []string
	quarantine map[string]ContentModerationQuarantineEntry
}

func (c *incrementalPassCacheStub) LookupPASS(_ context.Context, _ ContentModerationPassCacheOptions, keys []string) (map[string]bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lookups++
	if c.lookupErr != nil {
		return map[string]bool{"partial-must-not-be-used": true}, c.lookupErr
	}
	out := map[string]bool{}
	for _, key := range keys {
		if c.pass[key] {
			out[key] = true
		}
	}
	return out, nil
}
func (c *incrementalPassCacheStub) StorePASS(_ context.Context, _ ContentModerationPassCacheOptions, keys []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stores++
	for _, key := range keys {
		c.pass[key] = true
	}
}

func (c *incrementalPassCacheStub) DeletePASS(_ context.Context, _ ContentModerationPassCacheOptions, keys []string) error {
	c.deleted = append(c.deleted, keys...)
	return nil
}
func (*incrementalPassCacheStub) LookupQuarantine(context.Context, ContentModerationPassCacheOptions, []string) (map[string]ContentModerationQuarantineEntry, error) {
	return map[string]ContentModerationQuarantineEntry{}, nil
}

func (c *incrementalPassCacheStub) StoreQuarantine(_ context.Context, _ ContentModerationPassCacheOptions, entries map[string]ContentModerationQuarantineEntry) error {
	if c.quarantine == nil {
		c.quarantine = map[string]ContentModerationQuarantineEntry{}
	}
	for key, entry := range entries {
		c.quarantine[key] = entry
	}
	return nil
}
func (*incrementalPassCacheStub) DeleteQuarantine(context.Context, ContentModerationPassCacheOptions, []string) error {
	return nil
}
func (c *incrementalPassCacheStub) GetComparisonMetadata(context.Context, string) (*ContentModerationComparisonMetadata, error) {
	return c.metadata, nil
}

func (c *incrementalPassCacheStub) StoreComparisonMetadata(_ context.Context, _ string, metadata ContentModerationComparisonMetadata) error {
	c.metadata = &metadata
	return nil
}
func (*incrementalPassCacheStub) DeleteComparisonMetadata(context.Context, string) error { return nil }

func TestContentModerationIncrementalRepeatUsesPASSCache(t *testing.T) {
	var calls atomic.Int64
	client := &http.Client{Transport: moderationRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"results":[{"risk_level":"PASS","risk_types":[]}]}`)), Request: req}, nil
	})}
	cache := &incrementalPassCacheStub{pass: map[string]bool{}}
	svc := NewContentModerationService(nil, nil, nil, nil, nil, nil, nil)
	svc.SetIncrementalModerationDependencies(cache, incrementalEpochStub{value: 3}, incrementalClientFactoryStub{client: client}, []byte(strings.Repeat("k", 32)), 1)
	cfg := defaultContentModerationConfig()
	cfg.Provider = "zhipu"
	cfg.BaseURL = "https://open.bigmodel.cn/api"
	cfg.Model = "moderation"
	cfg.APIKeys = []string{"key"}
	cfg.PassCacheEnabled = true
	text := strings.Repeat("界", 2000)
	content := ContentModerationInput{Extraction: ModerationExtraction{Complete: true, Sources: []ModerationTextSource{{Source: "messages[0]", Role: "user", Text: text}}}}

	first, err := svc.runIncrementalModeration(context.Background(), ContentModerationCheckInput{}, cfg, content)
	require.NoError(t, err)
	require.Equal(t, ModerationLevelPass, first.Level)
	require.Equal(t, int64(2), calls.Load())
	require.Equal(t, 1, cache.stores)

	second, err := svc.runIncrementalModeration(context.Background(), ContentModerationCheckInput{}, cfg, content)
	require.NoError(t, err)
	require.Equal(t, ModerationLevelPass, second.Level)
	require.Equal(t, int64(2), calls.Load(), "all-hit repeat must not call the provider")
	require.Equal(t, 2, cache.lookups)
}

func TestContentModerationIncrementalCacheReadErrorAuditsEveryChunk(t *testing.T) {
	var calls atomic.Int64
	client := &http.Client{Transport: moderationRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"results":[{"risk_level":"PASS","risk_types":[]}]}`)), Request: req}, nil
	})}
	cache := &incrementalPassCacheStub{pass: map[string]bool{}, lookupErr: errors.New("redis unavailable")}
	svc := NewContentModerationService(nil, nil, nil, nil, nil, nil, nil)
	svc.SetIncrementalModerationDependencies(cache, incrementalEpochStub{}, incrementalClientFactoryStub{client: client}, []byte(strings.Repeat("k", 32)), 1)
	cfg := defaultContentModerationConfig()
	cfg.Provider, cfg.BaseURL, cfg.Model = "zhipu", "https://open.bigmodel.cn/api", "moderation"
	cfg.APIKeys, cfg.PassCacheEnabled = []string{"key"}, true
	content := ContentModerationInput{Extraction: ModerationExtraction{Complete: true, Sources: []ModerationTextSource{{Source: "message", Role: "user", Text: strings.Repeat("界", 2000)}}}}

	got, err := svc.runIncrementalModeration(context.Background(), ContentModerationCheckInput{}, cfg, content)
	require.NoError(t, err)
	require.Equal(t, ModerationLevelPass, got.Level)
	require.Equal(t, int64(2), calls.Load())
}

func TestContentModerationCyberCorrelationDeletesPASSAndQuarantinesRequest(t *testing.T) {
	now := time.Now()
	cache := &incrementalPassCacheStub{metadata: &ContentModerationComparisonMetadata{
		RequestID: "req-1", DecisionID: "decision-1", RequestHMAC: "request-hmac", ChunkKeys: []string{"chunk-a", "chunk-b"},
		Provider: "zhipu", AggregateLevel: string(ModerationLevelPass), TotalChunks: 2, CachedChunks: 1, FreshChunks: 1,
		CompletePASSEvidence: true, ForwardedUpstream: "openai", ForwardedAt: now.Add(-time.Minute), CorrelationDeadline: now.Add(time.Minute),
	}}
	svc := NewContentModerationService(nil, nil, nil, nil, nil, nil, nil)
	svc.SetIncrementalModerationDependencies(cache, incrementalEpochStub{}, incrementalClientFactoryStub{}, []byte(strings.Repeat("k", 32)), 1)
	got := svc.correlateCyberPolicyMiss(context.Background(), defaultContentModerationConfig(), "req-1")
	require.NotNil(t, got)
	require.Equal(t, []string{"chunk-a", "chunk-b"}, cache.deleted)
	require.Contains(t, cache.quarantine, "request-hmac")

	cache.deleted = nil
	cache.metadata.CorrelationDeadline = now.Add(-time.Second)
	require.Nil(t, svc.correlateCyberPolicyMiss(context.Background(), defaultContentModerationConfig(), "req-1"))
	require.Empty(t, cache.deleted)
}
