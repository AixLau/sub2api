package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type codexRoutingCacheFake struct {
	states    map[int64]CodexClientAccountState
	whitelist []openai.AllowedClientEntry

	getStateErr        error
	setStateErr        error
	deleteStateErr     error
	getWhitelistErr    error
	setWhitelistErr    error
	deleteWhitelistErr error

	stateSetCalls     int
	whitelistSetCalls int
}

func newCodexRoutingCacheFake() *codexRoutingCacheFake {
	return &codexRoutingCacheFake{states: make(map[int64]CodexClientAccountState)}
}

func (c *codexRoutingCacheFake) GetCodexClientAccountState(_ context.Context, accountID int64) (CodexClientAccountState, bool, error) {
	if c.getStateErr != nil {
		return CodexClientAccountState{}, false, c.getStateErr
	}
	state, ok := c.states[accountID]
	return state, ok, nil
}

func (c *codexRoutingCacheFake) SetCodexClientAccountState(_ context.Context, accountID int64, state CodexClientAccountState, _ time.Duration) error {
	c.stateSetCalls++
	if c.setStateErr != nil {
		return c.setStateErr
	}
	c.states[accountID] = state
	return nil
}

func (c *codexRoutingCacheFake) DeleteCodexClientAccountState(_ context.Context, accountID int64) error {
	if c.deleteStateErr != nil {
		return c.deleteStateErr
	}
	delete(c.states, accountID)
	return nil
}

func (c *codexRoutingCacheFake) GetCodexClientWhitelist(context.Context) ([]openai.AllowedClientEntry, bool, error) {
	if c.getWhitelistErr != nil {
		return nil, false, c.getWhitelistErr
	}
	if c.whitelist == nil {
		return nil, false, nil
	}
	return append([]openai.AllowedClientEntry(nil), c.whitelist...), true, nil
}

func (c *codexRoutingCacheFake) SetCodexClientWhitelist(_ context.Context, entries []openai.AllowedClientEntry, _ time.Duration) error {
	c.whitelistSetCalls++
	if c.setWhitelistErr != nil {
		return c.setWhitelistErr
	}
	c.whitelist = append([]openai.AllowedClientEntry(nil), entries...)
	return nil
}

func (c *codexRoutingCacheFake) DeleteCodexClientWhitelist(context.Context) error {
	if c.deleteWhitelistErr != nil {
		return c.deleteWhitelistErr
	}
	c.whitelist = nil
	return nil
}

type codexRoutingSettingRepo struct {
	values map[string]string
	err    error
}

func (r *codexRoutingSettingRepo) Get(context.Context, string) (*Setting, error) {
	return nil, ErrSettingNotFound
}

func (r *codexRoutingSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	value, ok := r.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}

func (r *codexRoutingSettingRepo) Set(_ context.Context, key, value string) error {
	r.values[key] = value
	return nil
}

func (r *codexRoutingSettingRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	if r.err != nil {
		return nil, r.err
	}
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

func (r *codexRoutingSettingRepo) SetMultiple(_ context.Context, values map[string]string) error {
	for key, value := range values {
		r.values[key] = value
	}
	return nil
}

func (r *codexRoutingSettingRepo) GetAll(context.Context) (map[string]string, error) {
	out := make(map[string]string, len(r.values))
	for key, value := range r.values {
		out[key] = value
	}
	return out, nil
}

func (r *codexRoutingSettingRepo) Delete(_ context.Context, key string) error {
	delete(r.values, key)
	return nil
}

type codexRoutingAccountRepo struct {
	AccountRepository
	accounts []Account
	err      error
}

func (r *codexRoutingAccountRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	if r.err != nil {
		return nil, r.err
	}
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			return &r.accounts[i], nil
		}
	}
	return nil, errors.New("account not found")
}

func (r *codexRoutingAccountRepo) ListSchedulableByGroupIDAndPlatform(context.Context, int64, string) ([]Account, error) {
	if r.err != nil {
		return nil, r.err
	}
	return append([]Account(nil), r.accounts...), nil
}

func newCodexRoutingSettings(values map[string]string) *SettingService {
	if values == nil {
		values = map[string]string{}
	}
	if _, ok := values[SettingKeyCodexCLIOnlyEngineFingerprintSignals]; !ok {
		values[SettingKeyCodexCLIOnlyEngineFingerprintSignals] = "[]"
	}
	return NewSettingService(&codexRoutingSettingRepo{values: values}, nil)
}

func newCodexRoutingAccount(id int64, priority int, restricted bool) Account {
	account := Account{
		ID:          id,
		Name:        "codex-routing-test",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Priority:    priority,
		Concurrency: 1,
	}
	if restricted {
		account.Extra = map[string]any{"codex_cli_only": true}
	}
	return account
}

func newCodexRoutingRequestContext(t *testing.T, userAgent, originator string, headers map[string]string) context.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("User-Agent", userAgent)
	if originator != "" {
		c.Request.Header.Set("originator", originator)
	}
	for key, value := range headers {
		c.Request.Header.Set(key, value)
	}
	return WithCodexRestrictionRequest(context.Background(), c, []byte(`{"model":"gpt-5.6-sol"}`))
}

func TestCodexClientRouting_CodexClientsCanUseRestrictedAccounts(t *testing.T) {
	settings := newCodexRoutingSettings(map[string]string{
		SettingKeyCodexCLIOnlyWhitelist: `[{"originator":"opencode","ua_contains":["opencode/"],"skip_engine_fingerprint":true}]`,
	})
	cache := newCodexRoutingCacheFake()
	cache.states[1] = CodexClientAccountState{Enabled: true}
	restricted := newCodexRoutingAccount(1, 1, true)
	detector := NewOpenAICodexClientRestrictionDetector(nil)

	cases := []struct {
		name       string
		userAgent  string
		originator string
		headers    map[string]string
	}{
		{
			name:      "official Codex UA",
			userAgent: "codex_cli_rs/0.146.0 (Mac OS X 14.0; arm64) terminal",
			headers:   map[string]string{"x-codex-window-id": "window-1"},
		},
		{
			name:       "whitelisted Codex client",
			userAgent:  "opencode/1.2.3",
			originator: "opencode",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := newCodexRoutingRequestContext(t, tc.userAgent, tc.originator, tc.headers)
			allowed, reason := codexAccountAllowedForSchedulingWithCache(ctx, &restricted, nil, settings, cache, detector)
			require.True(t, allowed, "reason=%s", reason)
		})
	}
}

func TestCodexClientRouting_NonCodexClientsSkipRestrictedAccounts(t *testing.T) {
	settings := newCodexRoutingSettings(nil)
	cache := newCodexRoutingCacheFake()
	cache.states[1] = CodexClientAccountState{Enabled: true}
	cache.states[2] = CodexClientAccountState{Enabled: false}
	restricted := newCodexRoutingAccount(1, 1, true)
	open := newCodexRoutingAccount(2, 2, false)
	detector := NewOpenAICodexClientRestrictionDetector(nil)
	ctx := newCodexRoutingRequestContext(t, "curl/8.7.1", "", nil)

	allowed, reason := codexAccountAllowedForSchedulingWithCache(ctx, &restricted, nil, settings, cache, detector)
	require.False(t, allowed)
	require.Equal(t, CodexClientRestrictionReasonNonCodexClient, reason)

	allowed, reason = codexAccountAllowedForSchedulingWithCache(ctx, &open, nil, settings, cache, detector)
	require.True(t, allowed, "reason=%s", reason)
}

func TestCodexClientRouting_ForceCodexCLIBypassesClientTypeFilter(t *testing.T) {
	settings := newCodexRoutingSettings(nil)
	cache := newCodexRoutingCacheFake()
	cache.states[1] = CodexClientAccountState{Enabled: true}
	restricted := newCodexRoutingAccount(1, 1, true)
	detector := NewOpenAICodexClientRestrictionDetector(&config.Config{
		Gateway: config.GatewayConfig{ForceCodexCLI: true},
	})
	ctx := newCodexRoutingRequestContext(t, "curl/8.7.1", "", nil)

	allowed, reason := codexAccountAllowedForSchedulingWithCache(ctx, &restricted, nil, settings, cache, detector)
	require.True(t, allowed, "reason=%s", reason)
}

func TestCodexClientRouting_SelectionUsesOnlyUnrestrictedAccountsForNonCodex(t *testing.T) {
	groupID := int64(7)
	accounts := []Account{
		newCodexRoutingAccount(1, 1, true),
		newCodexRoutingAccount(2, 2, false),
	}
	repo := &codexRoutingAccountRepo{accounts: accounts}
	cache := newCodexRoutingCacheFake()
	cache.states[1] = CodexClientAccountState{Enabled: true}
	cache.states[2] = CodexClientAccountState{Enabled: false}
	svc := &OpenAIGatewayService{
		accountRepo:      repo,
		codexClientCache: cache,
		codexDetector:    NewOpenAICodexClientRestrictionDetector(nil),
	}
	ctx := newCodexRoutingRequestContext(t, "curl/8.7.1", "", nil)

	selected, err := svc.selectAccountForModelWithExclusions(ctx, &groupID, PlatformOpenAI, "", "gpt-5.6-sol", nil, false, 0, "", false)
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, int64(2), selected.ID)
}

func TestCodexClientRouting_AllRestrictedReturnsNoAvailableAccountsForNonCodex(t *testing.T) {
	groupID := int64(7)
	accounts := []Account{
		newCodexRoutingAccount(1, 1, true),
		newCodexRoutingAccount(2, 2, true),
	}
	repo := &codexRoutingAccountRepo{accounts: accounts}
	cache := newCodexRoutingCacheFake()
	cache.states[1] = CodexClientAccountState{Enabled: true}
	cache.states[2] = CodexClientAccountState{Enabled: true}
	svc := &OpenAIGatewayService{
		accountRepo:      repo,
		codexClientCache: cache,
		codexDetector:    NewOpenAICodexClientRestrictionDetector(nil),
	}
	ctx := newCodexRoutingRequestContext(t, "curl/8.7.1", "", nil)

	selected, err := svc.selectAccountForModelWithExclusions(ctx, &groupID, PlatformOpenAI, "", "gpt-5.6-sol", nil, false, 0, "", false)
	require.Nil(t, selected)
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	require.NotErrorIs(t, err, ErrNoAllowedCodexAccounts)
}

func TestCodexClientRouting_WhitelistedClientCanSelectRestrictedAccount(t *testing.T) {
	groupID := int64(7)
	accounts := []Account{
		newCodexRoutingAccount(1, 1, true),
		newCodexRoutingAccount(2, 2, false),
	}
	repo := &codexRoutingAccountRepo{accounts: accounts}
	cache := newCodexRoutingCacheFake()
	cache.states[1] = CodexClientAccountState{Enabled: true}
	cache.states[2] = CodexClientAccountState{Enabled: false}
	settings := newCodexRoutingSettings(map[string]string{
		SettingKeyCodexCLIOnlyWhitelist: `[{"originator":"opencode","ua_contains":["opencode/"],"skip_engine_fingerprint":true}]`,
	})
	svc := &OpenAIGatewayService{
		accountRepo:      repo,
		settingService:   settings,
		codexClientCache: cache,
		codexDetector:    NewOpenAICodexClientRestrictionDetector(nil),
	}
	ctx := newCodexRoutingRequestContext(t, "opencode/1.2.3", "opencode", nil)

	selected, err := svc.selectAccountForModelWithExclusions(ctx, &groupID, PlatformOpenAI, "", "gpt-5.6-sol", nil, false, 0, "", false)
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, int64(1), selected.ID)
}

func TestCodexClientRouting_CacheUpdatesChangeRoutingImmediately(t *testing.T) {
	settings := newCodexRoutingSettings(nil)
	cache := newCodexRoutingCacheFake()
	settings.SetCodexClientConfigCache(cache)
	cache.states[1] = CodexClientAccountState{Enabled: true}
	restricted := newCodexRoutingAccount(1, 1, true)
	detector := NewOpenAICodexClientRestrictionDetector(nil)
	nonCodex := newCodexRoutingRequestContext(t, "curl/8.7.1", "", nil)
	whitelisted := newCodexRoutingRequestContext(t, "opencode/1.2.3", "opencode", nil)

	allowed, reason := codexAccountAllowedForSchedulingWithCache(nonCodex, &restricted, nil, settings, cache, detector)
	require.False(t, allowed)
	require.Equal(t, CodexClientRestrictionReasonNonCodexClient, reason)

	// Admin turns codex_cli_only off: the repository writes/deletes this cache
	// entry and refreshes the scheduler snapshot in the same operation.
	cache.states[1] = CodexClientAccountState{Enabled: false}
	restricted = newCodexRoutingAccount(1, 1, false)
	allowed, reason = codexAccountAllowedForSchedulingWithCache(nonCodex, &restricted, nil, settings, cache, detector)
	require.True(t, allowed, "reason=%s", reason)

	// Admin turns it back on and adds the client to the global whitelist.
	cache.states[1] = CodexClientAccountState{Enabled: true}
	restricted = newCodexRoutingAccount(1, 1, true)
	settings.invalidateCodexRestrictionPolicy(context.Background())
	settings.SetCodexClientWhitelistCache(context.Background(), `[{"originator":"opencode","ua_contains":["opencode/"],"skip_engine_fingerprint":true}]`)

	allowed, reason = codexAccountAllowedForSchedulingWithCache(whitelisted, &restricted, nil, settings, cache, detector)
	require.True(t, allowed, "reason=%s", reason)

	// Removing the whitelist entry must take effect immediately as well.
	settings.invalidateCodexRestrictionPolicy(context.Background())
	settings.SetCodexClientWhitelistCache(context.Background(), "")
	allowed, reason = codexAccountAllowedForSchedulingWithCache(whitelisted, &restricted, nil, settings, cache, detector)
	require.False(t, allowed)
	require.Equal(t, CodexClientRestrictionReasonNonCodexClient, reason)
}

func TestCodexClientRouting_SettingUpdateWritesWhitelistCache(t *testing.T) {
	repo := &codexRoutingSettingRepo{values: map[string]string{}}
	cache := newCodexRoutingCacheFake()
	settings := NewSettingService(repo, &config.Config{})
	settings.SetCodexClientConfigCache(cache)

	err := settings.UpdateSettings(context.Background(), &SystemSettings{
		CodexCLIOnlyWhitelist:                `[{"originator":"opencode","ua_contains":["opencode/"],"skip_engine_fingerprint":true}]`,
		CodexCLIOnlyEngineFingerprintSignals: "[]",
	})
	require.NoError(t, err)

	entries, found, err := cache.GetCodexClientWhitelist(context.Background())
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, entries, 1)
	require.Equal(t, "opencode", entries[0].Originator)

	policy := settings.GetCodexRestrictionPolicy(context.Background())
	require.Len(t, policy.Whitelist, 1)
	require.Equal(t, "opencode", policy.Whitelist[0].Originator)
}

func TestCodexClientRouting_WhitelistCacheWriteFailureFallsBackToDatabase(t *testing.T) {
	repo := &codexRoutingSettingRepo{values: map[string]string{
		SettingKeyCodexCLIOnlyWhitelist: `[{"originator":"old","ua_contains":["old/"]}]`,
	}}
	cache := newCodexRoutingCacheFake()
	cache.whitelist = []openai.AllowedClientEntry{{Originator: "old", UAContains: []string{"old/"}}}
	cache.setWhitelistErr = errors.New("redis write failed")
	cache.deleteWhitelistErr = errors.New("redis delete failed")
	settings := NewSettingService(repo, &config.Config{})
	settings.SetCodexClientConfigCache(cache)

	err := settings.UpdateSettings(context.Background(), &SystemSettings{
		CodexCLIOnlyWhitelist:                `[{"originator":"opencode","ua_contains":["opencode/"],"skip_engine_fingerprint":true}]`,
		CodexCLIOnlyEngineFingerprintSignals: "[]",
	})
	require.NoError(t, err)

	// The cache still holds the old entry and both cache mutations failed, so
	// the policy load must bypass the unreliable cache and read the saved DB
	// value instead.
	policy := settings.GetCodexRestrictionPolicy(context.Background())
	require.Len(t, policy.Whitelist, 1)
	require.Equal(t, "opencode", policy.Whitelist[0].Originator)
}

func TestCodexClientRouting_CacheErrorsFallBackToDatabase(t *testing.T) {
	// A stale cache value must not win when Redis is unavailable: the account
	// repository says the account is restricted, so non-Codex traffic is denied.
	settings := newCodexRoutingSettings(nil)
	cache := newCodexRoutingCacheFake()
	cache.states[1] = CodexClientAccountState{Enabled: false}
	cache.getStateErr = errors.New("redis unavailable")
	restricted := newCodexRoutingAccount(1, 1, true)
	staleSnapshot := newCodexRoutingAccount(1, 1, false)
	repo := &codexRoutingAccountRepo{accounts: []Account{restricted}}
	detector := NewOpenAICodexClientRestrictionDetector(nil)
	ctx := newCodexRoutingRequestContext(t, "curl/8.7.1", "", nil)

	allowed, reason := codexAccountAllowedForSchedulingWithCache(ctx, &staleSnapshot, repo, settings, cache, detector)
	require.False(t, allowed)
	require.Equal(t, CodexClientRestrictionReasonNonCodexClient, reason)

	// Whitelist cache failure also falls back to the database instead of
	// treating the client as unrestricted.
	whitelistCache := newCodexRoutingCacheFake()
	whitelistCache.getWhitelistErr = errors.New("redis unavailable")
	whitelistSettings := newCodexRoutingSettings(map[string]string{
		SettingKeyCodexCLIOnlyWhitelist: `[{"originator":"opencode","ua_contains":["opencode/"],"skip_engine_fingerprint":true}]`,
	})
	whitelistSettings.SetCodexClientConfigCache(whitelistCache)
	allowed, reason = codexAccountAllowedForSchedulingWithCache(
		newCodexRoutingRequestContext(t, "opencode/1.2.3", "opencode", nil),
		&restricted, repo, whitelistSettings, whitelistCache, detector,
	)
	require.True(t, allowed, "reason=%s", reason)
}

func TestCodexClientRouting_CacheMissAndWriteFailureUseDatabase(t *testing.T) {
	settings := newCodexRoutingSettings(nil)
	restricted := newCodexRoutingAccount(1, 1, true)
	repo := &codexRoutingAccountRepo{accounts: []Account{restricted}}
	detector := NewOpenAICodexClientRestrictionDetector(nil)
	ctx := newCodexRoutingRequestContext(t, "curl/8.7.1", "", nil)

	// Miss: the repository value decides the route and fills the cache.
	cache := newCodexRoutingCacheFake()
	allowed, reason := codexAccountAllowedForSchedulingWithCache(ctx, &restricted, repo, settings, cache, detector)
	require.False(t, allowed)
	require.Equal(t, CodexClientRestrictionReasonNonCodexClient, reason)
	require.Equal(t, CodexClientAccountState{Enabled: true}, cache.states[1])

	// A failed cache fill must not change the DB-derived decision.
	cache = newCodexRoutingCacheFake()
	cache.setStateErr = errors.New("redis unavailable")
	allowed, reason = codexAccountAllowedForSchedulingWithCache(ctx, &restricted, repo, settings, cache, detector)
	require.False(t, allowed)
	require.Equal(t, CodexClientRestrictionReasonNonCodexClient, reason)

	// A cache entry that disagrees with the scheduler snapshot is treated as
	// inconsistent and resolved from the database.
	cache = newCodexRoutingCacheFake()
	cache.states[1] = CodexClientAccountState{Enabled: false}
	explicitSnapshot := newCodexRoutingAccount(1, 1, true)
	allowed, reason = codexAccountAllowedForSchedulingWithCache(ctx, &explicitSnapshot, repo, settings, cache, detector)
	require.False(t, allowed)
	require.Equal(t, CodexClientRestrictionReasonNonCodexClient, reason)
}

func TestCodexClientRouting_UnknownAccountStateFailsClosed(t *testing.T) {
	settings := newCodexRoutingSettings(nil)
	cache := newCodexRoutingCacheFake()
	cache.getStateErr = errors.New("redis unavailable")
	repo := &codexRoutingAccountRepo{err: errors.New("database unavailable")}
	snapshot := newCodexRoutingAccount(1, 1, false)
	detector := NewOpenAICodexClientRestrictionDetector(nil)

	allowed, reason := codexAccountAllowedForSchedulingWithCache(
		newCodexRoutingRequestContext(t, "curl/8.7.1", "", nil),
		&snapshot, repo, settings, cache, detector,
	)
	require.False(t, allowed)
	require.Equal(t, CodexClientRestrictionReasonNonCodexClient, reason)

	// Codex clients are allowed on restricted accounts, so an unknown flag can
	// still pass once the client identity and policy gates succeed.
	allowed, reason = codexAccountAllowedForSchedulingWithCache(
		newCodexRoutingRequestContext(t, "codex_cli_rs/0.146.0 (Mac OS X 14.0; arm64) terminal", "", map[string]string{"x-codex-window-id": "window-1"}),
		&snapshot, repo, settings, cache, detector,
	)
	require.True(t, allowed, "reason=%s", reason)
}
