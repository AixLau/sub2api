package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCodexIdentityHistoryRetentionFollowsObservationsOnly(t *testing.T) {
	server := miniredis.RunT(t)
	svc := newCodexPeriodRedisService(t, server)
	svc.cfg.Gateway.CodexIdentity.HistoryRetentionDays = 180
	ctx := context.Background()
	key := codexHTTPThreadKey("thread-history", "user:1", "account", "", "raw-parent")
	physical := "openai_codex_session_identity:" + key
	observed := time.Now().Add(-time.Hour).Truncate(time.Millisecond)
	record := codexHTTPThreadHistory{SessionID: uuid.Must(uuid.NewV7()).String(), ThreadID: uuid.NewString(), ObservedAtMs: observed.UnixMilli()}
	require.NoError(t, svc.recordCodexHTTPThreadHistory(ctx, key, record))
	initialTTL := server.TTL(physical)
	require.Greater(t, initialTTL, 179*24*time.Hour)
	require.Less(t, initialTTL, 180*24*time.Hour)
	server.FastForward(time.Hour)
	for range 3 {
		thread, err := svc.lookupCodexHTTPForkSource(ctx, nil, "user:1", "account", "raw-parent")
		require.NoError(t, err)
		require.Equal(t, record.ThreadID, thread)
	}
	require.Equal(t, initialTTL-time.Hour, server.TTL(physical), "fork lookups must not refresh retention")
	require.NoError(t, svc.recordCodexHTTPThreadHistory(ctx, key, record))
	require.Equal(t, initialTTL-time.Hour, server.TTL(physical), "retries of the same observation do not refresh retention")

	// A genuinely newer request extends the retained latest record even when
	// session/thread themselves did not change. Client turn timestamps are not used.
	newer := record
	newer.ObservedAtMs = time.Now().Truncate(time.Millisecond).UnixMilli()
	require.NoError(t, svc.recordCodexHTTPThreadHistory(ctx, key, newer))
	newTTL := server.TTL(physical)
	require.Greater(t, newTTL, initialTTL)
	stored, err := server.Get(physical)
	require.NoError(t, err)
	parsed, err := decodeCodexHTTPThreadHistory(stored)
	require.NoError(t, err)
	require.Equal(t, newer, parsed)
	require.NoError(t, svc.recordCodexHTTPThreadHistory(ctx, key, record))
	require.Equal(t, newTTL, server.TTL(physical), "late old observations cannot change retention")
	storedAfter, err := server.Get(physical)
	require.NoError(t, err)
	require.Equal(t, stored, storedAfter)
	server.FastForward(newTTL + time.Second)
	_, err = svc.lookupCodexHTTPForkSource(ctx, nil, "user:1", "account", "raw-parent")
	require.ErrorIs(t, err, ErrCodexSessionIdentityNotFound)
}

func TestCodexIdentityHistoryDoesNotReviveExpiredObservation(t *testing.T) {
	server := miniredis.RunT(t)
	svc := newCodexPeriodRedisService(t, server)
	svc.cfg.Gateway.CodexIdentity.HistoryRetentionDays = 1
	record := codexHTTPThreadHistory{SessionID: uuid.Must(uuid.NewV7()).String(), ThreadID: uuid.NewString(), ObservedAtMs: time.Now().Add(-48 * time.Hour).UnixMilli()}
	require.NoError(t, svc.recordCodexHTTPThreadHistory(context.Background(), "expired-history", record))
	require.Empty(t, server.Keys())
}

func TestCodexIdentitySideLifecycleMonotonicAndReadOnly(t *testing.T) {
	server := miniredis.RunT(t)
	svc := newCodexPeriodRedisService(t, server)
	ctx := context.Background()
	session := uuid.Must(uuid.NewV7()).String()
	sideKey := codexHTTPIdentityMappingKey("side-session", "user:1", "account", "side")
	key := "openai_codex_session_identity:v4:side-lifecycle:" + sideKey[len("v3:side-session:"):]
	now := time.Now()
	require.NoError(t, svc.observeCodexHTTPSide(ctx, sideKey, session, now))
	firstRaw, err := server.Get(key)
	require.NoError(t, err)
	var first codexHTTPSideLifecycle
	require.NoError(t, json.Unmarshal([]byte(firstRaw), &first))
	require.Equal(t, now.UnixMilli(), first.LastObservedAtMs)
	parsed, err := uuid.Parse(session)
	require.NoError(t, err)
	sec, ns := parsed.Time().UnixTime()
	require.Equal(t, time.Unix(sec, ns).UnixMilli(), first.CreatedAtMs)

	later := now.Add(24 * time.Hour)
	require.NoError(t, svc.observeCodexHTTPSide(ctx, sideKey, session, later))
	lastRaw, err := server.Get(key)
	require.NoError(t, err)
	var last codexHTTPSideLifecycle
	require.NoError(t, json.Unmarshal([]byte(lastRaw), &last))
	require.Equal(t, first.CreatedAtMs, last.CreatedAtMs)
	require.Equal(t, later.UnixMilli(), last.LastObservedAtMs)
	require.NoError(t, svc.observeCodexHTTPSide(ctx, sideKey, session, now))
	for range 3 {
		raw, err := server.Get(key)
		require.NoError(t, err)
		require.Equal(t, lastRaw, raw, "inspection must not advance side activity")
	}
	require.Zero(t, server.TTL(key), "side metadata is removed with its owner, never independently timed out")
}

func TestCodexIdentityOwnershipScopeSurvivesAPIKeyRotation(t *testing.T) {
	account := newTestOAuthAccount(7660, map[string]any{codexFingerprintModeExtraKey: "session"})
	observedAt := time.Now().Truncate(time.Millisecond)
	first := newCodexSessionIdentityV2Context(t, 41, 11)
	second := newCodexSessionIdentityV2Context(t, 41, 99)
	a, ok := CodexIdentityOwnershipFromContext(codexHTTPIdentityOwnershipContext(context.Background(), first, account, observedAt, false))
	require.True(t, ok)
	b, ok := CodexIdentityOwnershipFromContext(codexHTTPIdentityOwnershipContext(context.Background(), second, account, observedAt, false))
	require.True(t, ok)
	require.Equal(t, a, b)
	require.Equal(t, "user:41", a.UserOwner)
	require.Equal(t, observedAt.UnixMilli(), a.ObservedAtMs)
	require.Equal(t, config.DefaultCodexIdentityHistoryRetentionDays, 180)
}
