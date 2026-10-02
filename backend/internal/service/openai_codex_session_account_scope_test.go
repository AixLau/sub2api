package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCodexSessionPeriodAccountSwitchDoesNotReuseSideAsMain(t *testing.T) {
	server := miniredis.RunT(t)
	svc := newCodexPeriodRedisService(t, server)
	accountA := newTestOAuthAccount(7701, map[string]any{codexFingerprintModeExtraKey: "session"})
	accountA.Credentials = map[string]any{"access_token": "a", "chatgpt_account_id": "account-a"}
	accountB := newTestOAuthAccount(7702, map[string]any{codexFingerprintModeExtraKey: "session"})
	accountB.Credentials = map[string]any{"access_token": "b", "chatgpt_account_id": "account-b"}
	now := time.Now()
	rootA := buildCodexTopologyAt(t, svc, accountA, codexRootTopology, now, 1, 11, false)
	sideA := buildCodexTopologyAt(t, svc, accountA, codexSideTopology, now, 1, 11, false)
	rootB := buildCodexTopologyAt(t, svc, accountB, codexRootTopology, now, 1, 11, false)
	sideB := buildCodexTopologyAt(t, svc, accountB, codexSideTopology, now, 1, 11, false)

	require.NotEqual(t, rootA.session(), rootB.session())
	require.NotEqual(t, sideA.session(), sideB.session())
	require.NotEqual(t, rootB.session(), sideB.session())
	require.NotEqual(t, sideA.session(), rootB.session())
	require.Equal(t, rootB.thread(), sideB.body.Get("client_metadata.forked_from_thread_id").String())
}

func TestCodexSessionPeriodAccountSwitchSideRootDoesNotReuseOldAccount(t *testing.T) {
	server := miniredis.RunT(t)
	svc := newCodexPeriodRedisService(t, server)
	accountA := newTestOAuthAccount(7704, map[string]any{codexFingerprintModeExtraKey: "session"})
	accountA.Credentials = map[string]any{"access_token": "a", "chatgpt_account_id": "account-a"}
	accountB := newTestOAuthAccount(7705, map[string]any{codexFingerprintModeExtraKey: "session"})
	accountB.Credentials = map[string]any{"access_token": "b", "chatgpt_account_id": "account-b"}
	now := time.Now()
	buildCodexTopologyAt(t, svc, accountA, codexRootTopology, now, 1, 11, false)
	sideA := buildCodexTopologyAt(t, svc, accountA, codexSideTopology, now, 1, 11, false)
	// Account B has no root or side records yet. A side root must not inherit
	// account A's side state when the selected upstream account changes. Without
	// a B-scoped fork source the safe result is an explicit unresolved-source
	// error; inventing a B session from A's fork would violate account isolation.
	c := newCodexSessionIdentityV2Context(t, 1, 11)
	stageCodexSessionIdentityInputMap(c, map[string]any{
		"client_metadata": map[string]any{openAIWSTurnMetadataHeader: codexSideTopology},
	})
	_, err := svc.resolveCodexHTTPFingerprintIDs(context.Background(), c, accountB, now)
	require.ErrorIs(t, err, ErrCodexSessionIdentityNotFound)
	require.True(t, server.Exists("openai_codex_session_identity:"+codexHTTPIdentityMappingKey(
		"side-session", "user:1", codexSessionIdentityUpstreamScope(accountA),
		gjson.Get(codexSideTopology, "session_id").String())))
	require.False(t, server.Exists("openai_codex_session_identity:"+codexHTTPIdentityMappingKey(
		"side-session", "user:1", codexSessionIdentityUpstreamScope(accountB),
		gjson.Get(codexSideTopology, "session_id").String())))
	require.NotEmpty(t, sideA.session())
}

func TestCodexBindingPeriodSessionRotatesAtEpochBoundary(t *testing.T) {
	server := miniredis.RunT(t)
	svc := newCodexPeriodRedisService(t, server)
	svc.cfg.Gateway.CodexIdentity.SessionBinding = config.CodexSessionBindingBinding
	account := newTestOAuthAccount(7703, map[string]any{codexFingerprintModeExtraKey: "session"})
	seed, ok := codexFingerprintSeed(account.Extra)
	require.True(t, ok)
	accountScope := codexSessionIdentityUpstreamScope(account)
	now := time.Now()
	firstPeriod := resolveCodexSessionPeriod(seed, "user:1", accountScope, now)
	before := firstPeriod.expiresAt.Add(-codexSessionPeriodGrace).Add(-time.Second)
	after := firstPeriod.expiresAt.Add(time.Second)

	first := buildCodexTopologyAt(t, svc, account, codexRootTopology, before, 1, 11, false)
	secondFixture := strings.ReplaceAll(accountSwitchRootTopologyForTest,
		"01950000-0000-7000-8000-000000000101", gjson.Get(codexRootTopology, "session_id").String())
	second := buildCodexTopologyAt(t, svc, account, secondFixture, after, 1, 11, false)

	require.NotEqual(t, first.session(), second.session())
	require.NotEqual(t, first.thread(), second.thread())
}

func TestCodexBindingChildCannotReuseExpiredMainParent(t *testing.T) {
	server := miniredis.RunT(t)
	svc := newCodexPeriodRedisService(t, server)
	svc.cfg.Gateway.CodexIdentity.SessionBinding = config.CodexSessionBindingBinding
	account := newTestOAuthAccount(7706, map[string]any{codexFingerprintModeExtraKey: "session"})
	seed, ok := codexFingerprintSeed(account.Extra)
	require.True(t, ok)
	accountScope := codexSessionIdentityUpstreamScope(account)
	now := time.Now()
	period := resolveCodexSessionPeriod(seed, "user:1", accountScope, now)
	before := period.expiresAt.Add(-codexSessionPeriodGrace).Add(-time.Second)
	after := period.expiresAt.Add(time.Second)
	buildCodexTopologyAt(t, svc, account, codexRootTopology, before, 1, 11, false)

	c := newCodexSessionIdentityV2Context(t, 1, 99)
	stageCodexSessionIdentityInputMap(c, map[string]any{
		"client_metadata": map[string]any{openAIWSTurnMetadataHeader: codexChildTopology},
	})
	ids, err := svc.resolveCodexHTTPFingerprintIDs(context.Background(), c, account, after)
	require.ErrorIs(t, err, ErrCodexBindingUnresolvedAttribution)
	require.Nil(t, ids)
}

const accountSwitchRootTopologyForTest = `{
  "session_id":"01950000-0000-7000-8000-000000000101",
  "thread_id":"01950000-0000-7000-8000-000000000101",
  "turn_id":"01950000-0000-7000-8000-000000000111",
  "root_turn_id":"01950000-0000-7000-8000-000000000111",
  "turn_started_at_unix_ms":1740000000111,
  "thread_source":"user", "request_kind":"regular",
  "installation_id":"client-install", "sandbox":"seatbelt",
  "sandbox_mode":"workspace-write"
}`
