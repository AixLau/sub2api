package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newCodexClientCacheTestCache(t *testing.T) (service.CodexClientConfigCache, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: server.Addr()})
	return NewCodexClientConfigCache(rdb), server
}

func TestCodexClientConfigCacheAccountState(t *testing.T) {
	cache, server := newCodexClientCacheTestCache(t)
	ctx := context.Background()

	_, found, err := cache.GetCodexClientAccountState(ctx, 42)
	require.NoError(t, err)
	require.False(t, found)

	require.NoError(t, cache.SetCodexClientAccountState(ctx, 42, service.CodexClientAccountState{
		Enabled:          true,
		AppServerAllowed: true,
	}, time.Minute))

	state, found, err := cache.GetCodexClientAccountState(ctx, 42)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, service.CodexClientAccountState{Enabled: true, AppServerAllowed: true}, state)

	server.FastForward(2 * time.Minute)
	_, found, err = cache.GetCodexClientAccountState(ctx, 42)
	require.NoError(t, err)
	require.False(t, found, "expired flag must be a miss so callers fall back to the database")

	require.NoError(t, cache.SetCodexClientAccountState(ctx, 42, service.CodexClientAccountState{Enabled: true}, time.Minute))
	require.NoError(t, cache.DeleteCodexClientAccountState(ctx, 42))
	_, found, err = cache.GetCodexClientAccountState(ctx, 42)
	require.NoError(t, err)
	require.False(t, found)
}

func TestCodexClientConfigCacheWhitelist(t *testing.T) {
	cache, server := newCodexClientCacheTestCache(t)
	ctx := context.Background()
	entries := []openai.AllowedClientEntry{{
		Originator:            "opencode",
		UAContains:            []string{"opencode/"},
		SkipEngineFingerprint: true,
	}}

	_, found, err := cache.GetCodexClientWhitelist(ctx)
	require.NoError(t, err)
	require.False(t, found)

	require.NoError(t, cache.SetCodexClientWhitelist(ctx, entries, time.Minute))
	got, found, err := cache.GetCodexClientWhitelist(ctx)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, entries, got)

	server.FastForward(2 * time.Minute)
	_, found, err = cache.GetCodexClientWhitelist(ctx)
	require.NoError(t, err)
	require.False(t, found)

	require.NoError(t, cache.SetCodexClientWhitelist(ctx, nil, time.Minute))
	got, found, err = cache.GetCodexClientWhitelist(ctx)
	require.NoError(t, err)
	require.True(t, found)
	require.Empty(t, got)

	require.NoError(t, cache.DeleteCodexClientWhitelist(ctx))
	_, found, err = cache.GetCodexClientWhitelist(ctx)
	require.NoError(t, err)
	require.False(t, found)
}

func TestAccountRepositorySyncsCodexClientState(t *testing.T) {
	cache, _ := newCodexClientCacheTestCache(t)
	repo := &accountRepository{codexClientCache: cache}
	ctx := context.Background()
	account := &service.Account{
		ID:       7,
		Platform: service.PlatformOpenAI,
		Type:     service.AccountTypeOAuth,
		Extra: map[string]any{
			"codex_cli_only":                  true,
			"codex_cli_only_allow_app_server": true,
		},
	}

	repo.syncCodexClientAccountState(ctx, account)
	state, found, err := cache.GetCodexClientAccountState(ctx, account.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, service.CodexClientAccountState{Enabled: true, AppServerAllowed: true}, state)

	repo.deleteCodexClientAccountState(ctx, account.ID)
	_, found, err = cache.GetCodexClientAccountState(ctx, account.ID)
	require.NoError(t, err)
	require.False(t, found)
}

func TestCodexClientConfigCacheUnavailable(t *testing.T) {
	cache := NewCodexClientConfigCache(nil)
	ctx := context.Background()

	_, _, err := cache.GetCodexClientAccountState(ctx, 1)
	require.Error(t, err)
	err = cache.SetCodexClientAccountState(ctx, 1, service.CodexClientAccountState{}, time.Minute)
	require.Error(t, err)

	_, _, err = cache.GetCodexClientWhitelist(ctx)
	require.Error(t, err)
	err = cache.SetCodexClientWhitelist(ctx, nil, time.Minute)
	require.Error(t, err)
}
