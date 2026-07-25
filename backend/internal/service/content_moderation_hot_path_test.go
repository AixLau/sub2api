package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type contentModerationConfigCountingRepo struct {
	*contentModerationTestSettingRepo
	configReads atomic.Int64
}

type contentModerationBlockingConfigRepo struct {
	*contentModerationTestSettingRepo
	configReads  atomic.Int64
	refreshStart chan struct{}
	releaseRead  chan struct{}
	configSet    chan struct{}
	setOnce      sync.Once
}

func (r *contentModerationBlockingConfigRepo) GetValue(ctx context.Context, key string) (string, error) {
	if key != SettingKeyContentModerationConfig {
		return r.contentModerationTestSettingRepo.GetValue(ctx, key)
	}
	call := r.configReads.Add(1)
	value, err := r.contentModerationTestSettingRepo.GetValue(ctx, key)
	if call == 2 {
		close(r.refreshStart)
		select {
		case <-r.releaseRead:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return value, err
}

func (r *contentModerationBlockingConfigRepo) Set(ctx context.Context, key, value string) error {
	if err := r.contentModerationTestSettingRepo.Set(ctx, key, value); err != nil {
		return err
	}
	if key == SettingKeyContentModerationConfig {
		r.setOnce.Do(func() { close(r.configSet) })
	}
	return nil
}

func (r *contentModerationConfigCountingRepo) GetValue(ctx context.Context, key string) (string, error) {
	if key == SettingKeyContentModerationConfig {
		r.configReads.Add(1)
	}
	return r.contentModerationTestSettingRepo.GetValue(ctx, key)
}

func hotPathTestConfig(t *testing.T, keyword string) string {
	t.Helper()
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.EngineMode = ContentModerationEngineModeRuleOnly
	cfg.KeywordBlockingMode = ContentModerationKeywordModeKeywordOnly
	cfg.KeywordRules = []ContentModerationKeywordRule{{
		Keyword: keyword, Category: ContentModerationKeywordCategoryCustom,
		Severity: ContentModerationKeywordSeverityHigh,
		Action:   ContentModerationKeywordActionBlock, Enabled: true,
	}}
	cfg.normalize()
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	return string(raw)
}

func TestContentModerationConfigSnapshotCachesNormalizedConfig(t *testing.T) {
	repo := &contentModerationConfigCountingRepo{contentModerationTestSettingRepo: &contentModerationTestSettingRepo{values: map[string]string{
		SettingKeyContentModerationConfig: hotPathTestConfig(t, "blocked"),
	}}}
	svc := NewContentModerationService(repo, &contentModerationTestRepo{}, nil, nil, nil, nil, nil)
	svc.configCacheTTL = time.Hour

	first, err := svc.loadConfig(context.Background())
	require.NoError(t, err)
	for range 20 {
		cfg, loadErr := svc.loadConfig(context.Background())
		require.NoError(t, loadErr)
		require.Same(t, first, cfg)
		require.Same(t, first.preparedKeywordRules, cfg.preparedKeywordRules)
	}
	require.Equal(t, int64(1), repo.configReads.Load())
}

func TestContentModerationConfigSnapshotCachesDistinctPolicyRevisions(t *testing.T) {
	cfg, err := parseContentModerationConfig(hotPathTestConfig(t, "blocked"))
	require.NoError(t, err)
	snapshot := newContentModerationConfigSnapshot(cfg, sha256.Sum256([]byte("config")), time.Now())

	disabled := snapshot.policyRevision(false)
	enabled := snapshot.policyRevision(true)
	require.NotEmpty(t, disabled)
	require.NotEmpty(t, enabled)
	require.NotEqual(t, disabled, enabled)
	require.Equal(t, disabled, snapshot.policyRevision(false))
	require.Equal(t, enabled, snapshot.policyRevision(true))
}

func TestContentModerationConfigSnapshotUnchangedRefreshKeepsPreparedState(t *testing.T) {
	raw := hotPathTestConfig(t, "blocked")
	repo := &contentModerationConfigCountingRepo{contentModerationTestSettingRepo: &contentModerationTestSettingRepo{values: map[string]string{
		SettingKeyContentModerationConfig: raw,
	}}}
	svc := NewContentModerationService(repo, &contentModerationTestRepo{}, nil, nil, nil, nil, nil)
	firstConfig, err := svc.loadConfig(context.Background())
	require.NoError(t, err)
	firstSnapshot := svc.configSnapshot.Load()
	require.NotNil(t, firstSnapshot)

	refreshedConfig, err := svc.refreshConfigSnapshot(context.Background())
	require.NoError(t, err)
	refreshedSnapshot := svc.configSnapshot.Load()
	require.Same(t, firstConfig, refreshedConfig)
	require.Same(t, firstConfig, refreshedSnapshot.config)
	require.Same(t, firstConfig.preparedKeywordRules, refreshedSnapshot.config.preparedKeywordRules)
	require.Equal(t, firstSnapshot.revisionDisabled, refreshedSnapshot.revisionDisabled)
	require.Equal(t, firstSnapshot.revisionEnabled, refreshedSnapshot.revisionEnabled)
}

func TestContentModerationConfigSnapshotRejectsInvalidResourceProtectionBeforePublish(t *testing.T) {
	base := &contentModerationTestSettingRepo{values: map[string]string{
		SettingKeyContentModerationConfig: hotPathTestConfig(t, "known-good"),
	}}
	svc := NewContentModerationService(base, &contentModerationTestRepo{}, nil, nil, nil, nil, nil)
	initial, err := svc.loadConfig(context.Background())
	require.NoError(t, err)
	initialSnapshot := svc.configSnapshot.Load()
	require.NotNil(t, initialSnapshot)
	initialProtection := svc.resourceProtection.Snapshot()

	invalid := cloneContentModerationConfig(initial)
	invalid.KeywordRules[0].Keyword = "invalid-update"
	invalid.ImageAuditMaxConcurrency = 33
	raw, err := json.Marshal(invalid)
	require.NoError(t, err)
	require.NoError(t, base.Set(context.Background(), SettingKeyContentModerationConfig, string(raw)))

	for range 2 {
		_, refreshErr := svc.refreshConfigSnapshot(context.Background())
		require.ErrorContains(t, refreshErr, "image_audit_max_concurrency")
		require.Same(t, initialSnapshot, svc.configSnapshot.Load())
		require.Equal(t, initialProtection, svc.resourceProtection.Snapshot())
	}

	require.NoError(t, base.Set(context.Background(), SettingKeyContentModerationConfig, hotPathTestConfig(t, "recovered")))
	recovered, err := svc.refreshConfigSnapshot(context.Background())
	require.NoError(t, err)
	require.Equal(t, "recovered", recovered.keywordRules()[0].Keyword)
	require.NotSame(t, initialSnapshot, svc.configSnapshot.Load())
	require.Equal(t, recovered.ResourceProtectionConfig, svc.resourceProtection.Snapshot())
}

func TestContentModerationConfigSnapshotRefreshesExternalChanges(t *testing.T) {
	repo := &contentModerationConfigCountingRepo{contentModerationTestSettingRepo: &contentModerationTestSettingRepo{values: map[string]string{
		SettingKeyContentModerationConfig: hotPathTestConfig(t, "old-keyword"),
	}}}
	svc := NewContentModerationService(repo, &contentModerationTestRepo{}, nil, nil, nil, nil, nil)
	svc.configCacheTTL = time.Nanosecond

	initial, err := svc.loadConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, "old-keyword", initial.keywordRules()[0].Keyword)
	require.NoError(t, repo.Set(context.Background(), SettingKeyContentModerationConfig, hotPathTestConfig(t, "new-keyword")))

	_, err = svc.loadConfig(context.Background())
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		current := svc.configSnapshot.Load()
		return current != nil && len(current.config.keywordRules()) == 1 && current.config.keywordRules()[0].Keyword == "new-keyword"
	}, time.Second, time.Millisecond)
}

func TestContentModerationConfigSnapshotKeepsLastKnownGoodAndBacksOff(t *testing.T) {
	base := &contentModerationTestSettingRepo{values: map[string]string{
		SettingKeyContentModerationConfig: hotPathTestConfig(t, "known-good"),
	}}
	repo := &contentModerationConfigCountingRepo{contentModerationTestSettingRepo: base}
	svc := NewContentModerationService(repo, &contentModerationTestRepo{}, nil, nil, nil, nil, nil)
	svc.configCacheTTL = time.Hour
	initial, err := svc.loadConfig(context.Background())
	require.NoError(t, err)
	snapshot := *svc.configSnapshot.Load()
	snapshot.loadedAt = time.Now().Add(-2 * time.Hour)
	svc.configSnapshot.Store(&snapshot)
	base.mu.Lock()
	base.errors = map[string]error{SettingKeyContentModerationConfig: errors.New("settings unavailable")}
	base.mu.Unlock()

	stale, err := svc.loadConfig(context.Background())
	require.NoError(t, err)
	require.Same(t, initial, stale)
	require.Eventually(t, func() bool {
		return svc.configRefreshRetryAt.Load() > time.Now().UnixNano()
	}, time.Second, time.Millisecond)
	reads := repo.configReads.Load()
	stale, err = svc.loadConfig(context.Background())
	require.NoError(t, err)
	require.Same(t, initial, stale)
	require.Equal(t, reads, repo.configReads.Load())
}

func TestContentModerationUpdateWinsAgainstInFlightStaleRefresh(t *testing.T) {
	base := &contentModerationTestSettingRepo{values: map[string]string{
		SettingKeyContentModerationConfig: hotPathTestConfig(t, "old-keyword"),
	}}
	repo := &contentModerationBlockingConfigRepo{
		contentModerationTestSettingRepo: base,
		refreshStart:                     make(chan struct{}),
		releaseRead:                      make(chan struct{}),
		configSet:                        make(chan struct{}),
	}
	svc := NewContentModerationService(repo, &contentModerationTestRepo{}, nil, nil, nil, nil, nil)
	svc.configCacheTTL = time.Hour
	_, err := svc.loadConfig(context.Background())
	require.NoError(t, err)
	snapshot := *svc.configSnapshot.Load()
	snapshot.loadedAt = time.Now().Add(-2 * time.Hour)
	svc.configSnapshot.Store(&snapshot)
	_, err = svc.loadConfig(context.Background())
	require.NoError(t, err)
	select {
	case <-repo.refreshStart:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stale config refresh")
	}

	updatedRules := []ContentModerationKeywordRule{{
		Keyword: "new-keyword", Category: ContentModerationKeywordCategoryCustom,
		Severity: ContentModerationKeywordSeverityHigh,
		Action:   ContentModerationKeywordActionBlock, Enabled: true,
	}}
	updateDone := make(chan error, 1)
	go func() {
		_, updateErr := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{KeywordRules: &updatedRules})
		updateDone <- updateErr
	}()
	select {
	case <-repo.configSet:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for config update")
	}
	close(repo.releaseRead)
	select {
	case updateErr := <-updateDone:
		require.NoError(t, updateErr)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for config update completion")
	}

	cached, err := svc.loadConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, "new-keyword", cached.keywordRules()[0].Keyword)
}

func TestContentModerationUpdatePublishesConfigAndTestOverridesDoNotPolluteIt(t *testing.T) {
	repo := &contentModerationConfigCountingRepo{contentModerationTestSettingRepo: &contentModerationTestSettingRepo{values: map[string]string{
		SettingKeyContentModerationConfig: hotPathTestConfig(t, "old-keyword"),
	}}}
	svc := NewContentModerationService(repo, &contentModerationTestRepo{}, nil, nil, nil, nil, nil)
	svc.configCacheTTL = time.Hour
	_, err := svc.loadConfig(context.Background())
	require.NoError(t, err)

	keywords := []ContentModerationKeywordRule{{
		Keyword: "new-keyword", Category: ContentModerationKeywordCategoryCustom,
		Severity: ContentModerationKeywordSeverityHigh,
		Action:   ContentModerationKeywordActionBlock, Enabled: true,
	}}
	_, err = svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{KeywordRules: &keywords})
	require.NoError(t, err)
	cached, err := svc.loadConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, "new-keyword", cached.keywordRules()[0].Keyword)
	originalBaseURL := cached.BaseURL

	_, err = svc.TestAPIKeys(context.Background(), TestContentModerationAPIKeysInput{
		BaseURL: "https://example.com", Provider: "openai", Model: "moderation-test", Prompt: "hello",
	})
	require.NoError(t, err)
	cached, err = svc.loadConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, originalBaseURL, cached.BaseURL)
}

func TestContentModerationUpdatePublishesIsolatedConfigSnapshot(t *testing.T) {
	repo := &contentModerationConfigCountingRepo{contentModerationTestSettingRepo: &contentModerationTestSettingRepo{values: map[string]string{
		SettingKeyContentModerationConfig: hotPathTestConfig(t, "old-keyword"),
	}}}
	svc := NewContentModerationService(repo, &contentModerationTestRepo{}, nil, nil, nil, nil, nil)
	oldConfig, err := svc.loadConfig(context.Background())
	require.NoError(t, err)
	oldPrepared := oldConfig.preparedKeywordRules

	updatedRules := []ContentModerationKeywordRule{{Keyword: "new-keyword", Enabled: true}}
	_, err = svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{KeywordRules: &updatedRules})
	require.NoError(t, err)
	newConfig, err := svc.loadConfig(context.Background())
	require.NoError(t, err)

	require.NotSame(t, oldConfig, newConfig)
	require.NotSame(t, oldPrepared, newConfig.preparedKeywordRules)
	require.Equal(t, "old-keyword", oldConfig.keywordRules()[0].Keyword)
	require.Equal(t, "new-keyword", newConfig.keywordRules()[0].Keyword)
	updatedRules[0].Keyword = "caller-mutated"
	require.Equal(t, "new-keyword", newConfig.keywordRules()[0].Keyword)
}

func TestContentModerationWarmPolicyRevisionAndAttemptReuseHaveNoAllocations(t *testing.T) {
	cfg, err := parseContentModerationConfig(hotPathTestConfig(t, "blocked"))
	require.NoError(t, err)
	snapshot := newContentModerationConfigSnapshot(cfg, sha256.Sum256([]byte("config")), time.Now())
	policy := &contentModerationPolicySnapshot{
		riskEnabled: true,
		config:      cfg,
		revision:    snapshot.policyRevision(true),
	}
	prior := &ContentModerationAttemptState{
		Reusable:       true,
		PolicyRevision: policy.revision,
		policySnapshot: policy,
	}
	svc := &ContentModerationService{}
	ctx := context.Background()

	require.Zero(t, testing.AllocsPerRun(1000, func() {
		_ = snapshot.policyRevision(true)
	}))
	require.Zero(t, testing.AllocsPerRun(1000, func() {
		_, _, _, loadErr := svc.loadAttemptPolicy(ctx, prior)
		if loadErr != nil {
			panic(loadErr)
		}
	}))
}

func TestContentModerationInputCacheSeparatesBodyProtocolAndScope(t *testing.T) {
	ctx := withContentModerationInputCache(context.Background())
	body := []byte(`{"input":"hello"}`)

	first := extractContentModerationInputCached(ctx, ContentModerationProtocolOpenAIResponses, body, ContentModerationAuditScopeUserOnly)
	second := extractContentModerationInputCached(ctx, ContentModerationProtocolOpenAIResponses, body, ContentModerationAuditScopeUserOnly)
	require.Equal(t, first, second)
	require.Len(t, contentModerationInputCacheFromContext(ctx).entries, 1)

	_ = extractContentModerationInputCached(ctx, ContentModerationProtocolOpenAIResponses, body, ContentModerationAuditScopeAllContext)
	_ = extractContentModerationInputCached(ctx, ContentModerationProtocolOpenAIChat, body, ContentModerationAuditScopeUserOnly)
	_ = extractContentModerationInputCached(ctx, ContentModerationProtocolOpenAIResponses, []byte(`{"input":"different"}`), ContentModerationAuditScopeUserOnly)
	require.Len(t, contentModerationInputCacheFromContext(ctx).entries, 4)
}

func TestContentModerationInputCacheConcurrentReuse(t *testing.T) {
	ctx := withContentModerationInputCache(context.Background())
	body := []byte(`{"input":[{"role":"user","content":[{"type":"input_text","text":"hello"}]}]}`)
	start := make(chan struct{})
	errs := make(chan error, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range 20 {
				input := extractContentModerationInputCached(ctx, ContentModerationProtocolOpenAIResponses, body, ContentModerationAuditScopeUserOnly)
				if input.Text != "hello" {
					errs <- fmt.Errorf("unexpected extracted text %q", input.Text)
					return
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	cache := contentModerationInputCacheFromContext(ctx)
	cache.mu.Lock()
	require.Len(t, cache.entries, 1)
	cache.mu.Unlock()
}

func TestCheckAccountAttemptReusesRequestExtraction(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.EngineMode = ContentModerationEngineModeRuleOnly
	cfg.AccountScope = ContentModerationAccountScopeAll
	svc := newAccountGateTestService(t, cfg)
	ctx := withContentModerationInputCache(context.Background())

	result, err := svc.CheckAccountAttempt(ctx, ContentModerationCheckInput{
		AccountID: 1, AccountType: AccountTypeAPIKey, Model: "gpt-5",
		Protocol: ContentModerationProtocolOpenAIResponses,
		Body:     []byte(`{"input":[{"role":"user","content":[{"type":"input_text","text":"ordinary request"}]}]}`),
	}, nil)
	require.NoError(t, err)
	require.NotNil(t, result.Decision)
	require.Len(t, contentModerationInputCacheFromContext(ctx).entries, 1)
}

func TestFindCompactKeywordComparableSpanHasNoAllocations(t *testing.T) {
	tests := []struct {
		name       string
		normalized string
		keyword    string
		wantHit    bool
	}{
		{name: "miss", normalized: "ordinary safe text", keyword: "secret", wantHit: false},
		{name: "hit", normalized: "safe s e c r e t value", keyword: "secret", wantHit: true},
		{name: "unicode hit", normalized: "这里有 敏 感 词 内容", keyword: "敏感词", wantHit: true},
		{name: "many rejected boundaries", normalized: strings.Repeat("xapikey", 1000), keyword: "apikey", wantHit: false},
		{name: "many spaced rejected boundaries", normalized: strings.Repeat("xapi key ", 1000), keyword: "apikey", wantHit: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compact := compactKeywordComparable(tt.normalized)
			_, _, hit := findCompactKeywordComparableSpanWithBoundary(tt.normalized, compact, tt.keyword)
			require.Equal(t, tt.wantHit, hit)
			allocs := testing.AllocsPerRun(1000, func() {
				_, _, _ = findCompactKeywordComparableSpanWithBoundary(tt.normalized, compact, tt.keyword)
			})
			require.Zero(t, allocs)
		})
	}
}

func TestContentModerationPreparedRuleSetMatchesAlreadyCompactText(t *testing.T) {
	rules := newContentModerationPreparedRuleSet([]ContentModerationKeywordRule{{
		Keyword: "api key", Enabled: true,
	}})

	match, hit := rules.Match("apikey")

	require.True(t, hit)
	require.Equal(t, "api key", match.Keyword)
}

func TestContentModerationPreparedRuleSetBoundaryFalseCandidatesHaveNoAllocations(t *testing.T) {
	rules := make([]ContentModerationKeywordRule, maxContentModerationBlockedKeywords)
	rules[0] = ContentModerationKeywordRule{Keyword: "apikey", Enabled: true}
	for index := 1; index < len(rules); index++ {
		rules[index] = ContentModerationKeywordRule{
			Keyword: fmt.Sprintf("unrelated%05d", index), Enabled: true,
		}
	}
	prepared := newContentModerationPreparedRuleSet(rules)
	text := strings.Repeat("xapi key ", maxModerationInputRunes/9)

	_, hit := prepared.Match(text)
	require.False(t, hit)
	allocs := testing.AllocsPerRun(100, func() {
		_, _ = prepared.Match(text)
	})
	require.Zero(t, allocs)
}

func TestContentModerationPreparedRuleSetAdversarialFalseCandidatesHaveNoAllocations(t *testing.T) {
	tests := []struct {
		name     string
		prepared *contentModerationPreparedRuleSet
		text     string
	}{
		{
			name:     "compact collision 10k",
			prepared: newContentModerationPreparedRuleSet(contentModerationCompactCollisionRules(10_000)),
			text:     strings.Repeat("xabcdefghijklmno ", maxModerationInputRunes/17),
		},
		{
			name: "nested suffix 200",
			prepared: func() *contentModerationPreparedRuleSet {
				rules := make([]ContentModerationKeywordRule, maxContentModerationBlockedKeywordRunes)
				for index := range rules {
					rules[index] = ContentModerationKeywordRule{Keyword: strings.Repeat("a", index+1), Enabled: true}
				}
				return newContentModerationPreparedRuleSet(rules)
			}(),
			text: "x" + strings.Repeat("a", maxModerationInputRunes-1),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, hit := tt.prepared.Match(tt.text)
			require.False(t, hit)
			allocs := testing.AllocsPerRun(10, func() {
				_, _ = tt.prepared.Match(tt.text)
			})
			require.Zero(t, allocs)
		})
	}
}

func TestContentModerationConfigRebuildsPreparedRulesAfterSliceReplacement(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.normalize()
	cfg.KeywordRules = []ContentModerationKeywordRule{{Keyword: "replacement", Enabled: true}}

	match, hit := cfg.keywordRuleSet().Match("replacement")

	require.True(t, hit)
	require.Equal(t, "replacement", match.Keyword)
}
