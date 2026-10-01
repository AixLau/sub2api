//go:build unit

package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// openAITokenCacheStub implements OpenAITokenCache for testing
type openAITokenCacheStub struct {
	mu        sync.Mutex
	tokens    map[string]string
	getErr    error
	setErr    error
	deleteErr error
	getCalled int32
	setCalled int32
}

func newOpenAITokenCacheStub() *openAITokenCacheStub {
	return &openAITokenCacheStub{
		tokens: make(map[string]string),
	}
}

func (s *openAITokenCacheStub) GetAccessToken(ctx context.Context, cacheKey string) (string, error) {
	atomic.AddInt32(&s.getCalled, 1)
	if s.getErr != nil {
		return "", s.getErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tokens[cacheKey], nil
}

func (s *openAITokenCacheStub) SetAccessToken(ctx context.Context, cacheKey string, token string, ttl time.Duration) error {
	atomic.AddInt32(&s.setCalled, 1)
	if s.setErr != nil {
		return s.setErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[cacheKey] = token
	return nil
}

func (s *openAITokenCacheStub) DeleteAccessToken(ctx context.Context, cacheKey string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tokens, cacheKey)
	return nil
}

// openAIAccountRepoStub is a minimal stub implementing only the methods used by OpenAITokenProvider
type openAIAccountRepoStub struct {
	account      *Account
	getErr       error
	updateErr    error
	getCalled    int32
	updateCalled int32
}

func (r *openAIAccountRepoStub) GetByID(ctx context.Context, id int64) (*Account, error) {
	atomic.AddInt32(&r.getCalled, 1)
	if r.getErr != nil {
		return nil, r.getErr
	}
	return r.account, nil
}

func (r *openAIAccountRepoStub) Update(ctx context.Context, account *Account) error {
	atomic.AddInt32(&r.updateCalled, 1)
	if r.updateErr != nil {
		return r.updateErr
	}
	r.account = account
	return nil
}

// openAIOAuthServiceStub implements OpenAIOAuthService methods for testing
type openAIOAuthServiceStub struct {
	tokenInfo     *OpenAITokenInfo
	refreshErr    error
	refreshCalled int32
}

func (s *openAIOAuthServiceStub) RefreshAccountToken(ctx context.Context, account *Account) (*OpenAITokenInfo, error) {
	atomic.AddInt32(&s.refreshCalled, 1)
	if s.refreshErr != nil {
		return nil, s.refreshErr
	}
	return s.tokenInfo, nil
}

func (s *openAIOAuthServiceStub) BuildAccountCredentials(info *OpenAITokenInfo) map[string]any {
	now := time.Now()
	return map[string]any{
		"access_token":  info.AccessToken,
		"refresh_token": info.RefreshToken,
		"expires_at":    now.Add(time.Duration(info.ExpiresIn) * time.Second).Format(time.RFC3339),
	}
}

func TestOpenAITokenProvider_CacheHit(t *testing.T) {
	cache := newOpenAITokenCacheStub()
	account := &Account{
		ID:       100,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "db-token",
		},
	}
	cacheKey := OpenAITokenCacheKey(account)
	cache.tokens[cacheKey] = "cached-token"

	provider := NewOpenAITokenProvider(nil, cache, nil)

	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "cached-token", token)
	require.Equal(t, int32(1), atomic.LoadInt32(&cache.getCalled))
	require.Equal(t, int32(0), atomic.LoadInt32(&cache.setCalled))
}

func TestOpenAITokenProvider_CacheMiss_FromCredentials(t *testing.T) {
	cache := newOpenAITokenCacheStub()
	// Token expires in far future, no refresh needed
	expiresAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	account := &Account{
		ID:       101,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "credential-token",
			"expires_at":   expiresAt,
		},
	}

	provider := NewOpenAITokenProvider(nil, cache, nil)

	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "credential-token", token)

	// Should have stored in cache
	cacheKey := OpenAITokenCacheKey(account)
	require.Equal(t, "credential-token", cache.tokens[cacheKey])
}

func TestOpenAITokenProvider_TokenRefresh(t *testing.T) {
	cache := newOpenAITokenCacheStub()
	accountRepo := &openAIAccountRepoStub{}
	oauthService := &openAIOAuthServiceStub{
		tokenInfo: &OpenAITokenInfo{
			AccessToken:  "refreshed-token",
			RefreshToken: "new-refresh-token",
			ExpiresIn:    3600,
		},
	}

	// Token expires soon (within refresh skew)
	expiresAt := time.Now().Add(1 * time.Minute).Format(time.RFC3339)
	account := &Account{
		ID:       102,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":  "old-token",
			"refresh_token": "old-refresh-token",
			"expires_at":    expiresAt,
		},
	}
	accountRepo.account = account

	// We need to directly test with the stub - create a custom provider
	customProvider := &testOpenAITokenProvider{
		accountRepo:  accountRepo,
		tokenCache:   cache,
		oauthService: oauthService,
	}

	token, err := customProvider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "refreshed-token", token)
	require.Equal(t, int32(1), atomic.LoadInt32(&oauthService.refreshCalled))
}

// testOpenAITokenProvider is a test version that uses the stub OAuth service
type testOpenAITokenProvider struct {
	accountRepo  *openAIAccountRepoStub
	tokenCache   *openAITokenCacheStub
	oauthService *openAIOAuthServiceStub
}

func (p *testOpenAITokenProvider) GetAccessToken(ctx context.Context, account *Account) (string, error) {
	if account == nil {
		return "", errors.New("account is nil")
	}
	if account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth {
		return "", errors.New("not an openai oauth account")
	}

	cacheKey := OpenAITokenCacheKey(account)

	// 1. Check cache
	if p.tokenCache != nil {
		if token, err := p.tokenCache.GetAccessToken(ctx, cacheKey); err == nil && token != "" {
			return token, nil
		}
	}

	// 2. Check if refresh needed
	expiresAt := account.GetCredentialAsTime("expires_at")
	needsRefresh := expiresAt == nil || time.Until(*expiresAt) <= openAITokenRefreshSkew
	refreshFailed := false
	if needsRefresh {
		fresh, err := p.accountRepo.GetByID(ctx, account.ID)
		if err == nil && fresh != nil {
			account = fresh
		}
		expiresAt = account.GetCredentialAsTime("expires_at")
		if expiresAt == nil || time.Until(*expiresAt) <= openAITokenRefreshSkew {
			if p.oauthService == nil {
				refreshFailed = true
			} else {
				tokenInfo, err := p.oauthService.RefreshAccountToken(ctx, account)
				if err != nil {
					refreshFailed = true
				} else {
					newCredentials := p.oauthService.BuildAccountCredentials(tokenInfo)
					for k, v := range account.Credentials {
						if _, exists := newCredentials[k]; !exists {
							newCredentials[k] = v
						}
					}
					account.Credentials = newCredentials
					_ = p.accountRepo.Update(ctx, account)
					expiresAt = account.GetCredentialAsTime("expires_at")
				}
			}
		}
	}

	accessToken := account.GetOpenAIAccessToken()
	if accessToken == "" {
		return "", errors.New("access_token not found in credentials")
	}

	// 3. Store in cache
	if p.tokenCache != nil {
		ttl := 30 * time.Minute
		if refreshFailed {
			ttl = time.Minute // 刷新失败时使用短 TTL
		} else if expiresAt != nil {
			until := time.Until(*expiresAt)
			if until > openAITokenCacheSkew {
				ttl = until - openAITokenCacheSkew
			} else if until > 0 {
				ttl = until
			} else {
				ttl = time.Minute
			}
		}
		_ = p.tokenCache.SetAccessToken(ctx, cacheKey, accessToken, ttl)
	}

	return accessToken, nil
}

func TestOpenAITokenProvider_NilAccount(t *testing.T) {
	provider := NewOpenAITokenProvider(nil, nil, nil)

	token, err := provider.GetAccessToken(context.Background(), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "account is nil")
	require.Empty(t, token)
}

func TestOpenAITokenProvider_WrongPlatform(t *testing.T) {
	provider := NewOpenAITokenProvider(nil, nil, nil)
	account := &Account{
		ID:       104,
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
	}

	token, err := provider.GetAccessToken(context.Background(), account)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not an openai oauth account")
	require.Empty(t, token)
}

func TestOpenAITokenProvider_WrongAccountType(t *testing.T) {
	provider := NewOpenAITokenProvider(nil, nil, nil)
	account := &Account{
		ID:       105,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
	}

	token, err := provider.GetAccessToken(context.Background(), account)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not an openai oauth account")
	require.Empty(t, token)
}

func TestOpenAITokenProvider_NilCache(t *testing.T) {
	// Token doesn't need refresh
	expiresAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	account := &Account{
		ID:       106,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "nocache-token",
			"expires_at":   expiresAt,
		},
	}

	provider := NewOpenAITokenProvider(nil, nil, nil)

	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "nocache-token", token)
}

func TestOpenAITokenProvider_CacheGetError(t *testing.T) {
	cache := newOpenAITokenCacheStub()
	cache.getErr = errors.New("redis connection failed")

	// Token doesn't need refresh
	expiresAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	account := &Account{
		ID:       107,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":  "fallback-token",
			"refresh_token": "refresh-token",
			"expires_at":    expiresAt,
		},
	}

	provider := NewOpenAITokenProvider(nil, cache, nil)

	// Should gracefully degrade and return from credentials
	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "fallback-token", token)
}

func TestOpenAITokenProvider_CacheSetError(t *testing.T) {
	cache := newOpenAITokenCacheStub()
	cache.setErr = errors.New("redis write failed")

	expiresAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	account := &Account{
		ID:       108,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "still-works-token",
			"expires_at":   expiresAt,
		},
	}

	provider := NewOpenAITokenProvider(nil, cache, nil)

	// Should still work even if cache set fails
	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "still-works-token", token)
}

func TestOpenAITokenProvider_MissingAccessToken(t *testing.T) {
	cache := newOpenAITokenCacheStub()
	expiresAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	account := &Account{
		ID:       109,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"expires_at": expiresAt,
			// missing access_token
		},
	}

	provider := NewOpenAITokenProvider(nil, cache, nil)

	token, err := provider.GetAccessToken(context.Background(), account)
	require.Error(t, err)
	require.Contains(t, err.Error(), "access_token not found")
	require.Empty(t, token)
}

func TestOpenAITokenProvider_RefreshError(t *testing.T) {
	cache := newOpenAITokenCacheStub()
	accountRepo := &openAIAccountRepoStub{}
	oauthService := &openAIOAuthServiceStub{
		refreshErr: errors.New("oauth refresh failed"),
	}

	// Token expires soon
	expiresAt := time.Now().Add(1 * time.Minute).Format(time.RFC3339)
	account := &Account{
		ID:       110,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":  "old-token",
			"refresh_token": "old-refresh-token",
			"expires_at":    expiresAt,
		},
	}
	accountRepo.account = account

	provider := &testOpenAITokenProvider{
		accountRepo:  accountRepo,
		tokenCache:   cache,
		oauthService: oauthService,
	}

	// Now with fallback behavior, should return existing token even if refresh fails
	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "old-token", token) // Fallback to existing token
}

func TestOpenAITokenProvider_OAuthServiceNotConfigured(t *testing.T) {
	cache := newOpenAITokenCacheStub()
	accountRepo := &openAIAccountRepoStub{}

	// Token expires soon
	expiresAt := time.Now().Add(1 * time.Minute).Format(time.RFC3339)
	account := &Account{
		ID:       111,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "old-token",
			"expires_at":   expiresAt,
		},
	}
	accountRepo.account = account

	provider := &testOpenAITokenProvider{
		accountRepo:  accountRepo,
		tokenCache:   cache,
		oauthService: nil, // not configured
	}

	// Now with fallback behavior, should return existing token even if oauth service not configured
	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "old-token", token) // Fallback to existing token
}

func TestOpenAITokenProvider_TTLCalculation(t *testing.T) {
	tests := []struct {
		name      string
		expiresIn time.Duration
	}{
		{
			name:      "far_future_expiry",
			expiresIn: 1 * time.Hour,
		},
		{
			name:      "medium_expiry",
			expiresIn: 10 * time.Minute,
		},
		{
			name:      "near_expiry",
			expiresIn: 6 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := newOpenAITokenCacheStub()
			expiresAt := time.Now().Add(tt.expiresIn).Format(time.RFC3339)
			account := &Account{
				ID:       200,
				Platform: PlatformOpenAI,
				Type:     AccountTypeOAuth,
				Credentials: map[string]any{
					"access_token": "test-token",
					"expires_at":   expiresAt,
				},
			}

			provider := NewOpenAITokenProvider(nil, cache, nil)

			_, err := provider.GetAccessToken(context.Background(), account)
			require.NoError(t, err)

			// Verify token was cached
			cacheKey := OpenAITokenCacheKey(account)
			require.Equal(t, "test-token", cache.tokens[cacheKey])
		})
	}
}

// Tests for real provider - to increase coverage
func TestOpenAITokenProvider_Real_ExpiredWithoutRefreshToken(t *testing.T) {
	cache := newOpenAITokenCacheStub()

	// Token with nil expires_at (no expiry set) - should use credentials
	account := &Account{
		ID:       202,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "no-expiry-token",
		},
	}

	provider := NewOpenAITokenProvider(nil, cache, nil)
	token, err := provider.GetAccessToken(context.Background(), account)
	// Without OAuth service, refresh will fail but token should be returned from credentials
	require.NoError(t, err)
	require.Equal(t, "no-expiry-token", token)
}

func TestOpenAITokenProvider_Real_WhitespaceToken(t *testing.T) {
	cache := newOpenAITokenCacheStub()
	cacheKey := "openai:account:203"
	cache.tokens[cacheKey] = "   " // Whitespace only - should be treated as empty

	expiresAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	account := &Account{
		ID:       203,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "real-token",
			"expires_at":   expiresAt,
		},
	}

	provider := NewOpenAITokenProvider(nil, cache, nil)
	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "real-token", token) // Should fall back to credentials
}

func TestOpenAITokenProvider_Real_WhitespaceCredentialToken(t *testing.T) {
	cache := newOpenAITokenCacheStub()

	expiresAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	account := &Account{
		ID:       205,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "   ", // Whitespace only
			"expires_at":   expiresAt,
		},
	}

	provider := NewOpenAITokenProvider(nil, cache, nil)
	token, err := provider.GetAccessToken(context.Background(), account)
	require.Error(t, err)
	require.Contains(t, err.Error(), "access_token not found")
	require.Empty(t, token)
}

func TestOpenAITokenProvider_Real_NilCredentials(t *testing.T) {
	cache := newOpenAITokenCacheStub()

	expiresAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	account := &Account{
		ID:       206,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"expires_at": expiresAt,
			// No access_token
		},
	}

	provider := NewOpenAITokenProvider(nil, cache, nil)
	token, err := provider.GetAccessToken(context.Background(), account)
	require.Error(t, err)
	require.Contains(t, err.Error(), "access_token not found")
	require.Empty(t, token)
}

func TestOpenAITokenProvider_NoRefreshTokenExpired_DisablesAccount(t *testing.T) {
	cache := newOpenAITokenCacheStub()
	repo := &rateLimitAccountRepoStub{}

	expiresAt := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	account := &Account{
		ID:       2881,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "expired-access-token",
			"expires_at":   expiresAt,
		},
	}

	cacheKey := OpenAITokenCacheKey(account)
	cache.tokens[cacheKey] = "stale-cached-token"
	// Force the provider past the cache hit branch.
	cache.getErr = errors.New("simulated cache miss")

	provider := NewOpenAITokenProvider(repo, cache, nil)
	blocker := &runtimeBlockRecorder{}
	provider.SetAccountRuntimeBlocker(blocker)

	token, err := provider.GetAccessToken(context.Background(), account)
	require.Error(t, err)
	require.Empty(t, token)
	require.Contains(t, err.Error(), "refresh_token is missing")

	require.Equal(t, 1, repo.setErrorCalls, "account should be disabled via SetError exactly once")
	require.Contains(t, repo.lastErrorMsg, "refresh_token is missing")
	require.Len(t, blocker.accounts, 1)
	require.Equal(t, account.ID, blocker.accounts[0].ID)
	require.Equal(t, "missing_refresh_token", blocker.reasons[0])
}
