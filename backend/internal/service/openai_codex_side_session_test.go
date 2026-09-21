package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Drive the same HTTP request builders used by Forward with an explicit clock,
// so a period boundary can be tested without sleeping or a production clock hook.
func buildCodexTopologyAt(t *testing.T, svc *OpenAIGatewayService, account *Account, fixture string, now time.Time, user, apiKey int64, passthrough bool) codexTopologyOutbound {
	t.Helper()
	c := newCodexSessionIdentityV2Context(t, user, apiKey)
	c.Request.Header.Set("session-id", gjson.Get(fixture, "session_id").String())
	c.Request.Header.Set("thread-id", gjson.Get(fixture, "thread_id").String())
	body := mustJSONForSessionIdentityTest(t, map[string]any{
		"model": "gpt-5.2", "input": []any{},
		"prompt_cache_key": gjson.Get(fixture, "session_id").String(),
		"client_metadata":  map[string]any{openAIWSTurnMetadataHeader: fixture},
	})
	stageCodexSessionIdentityInputRaw(c, body)
	ids, err := svc.resolveCodexHTTPFingerprintIDs(context.Background(), c, account, now)
	require.NoError(t, err)
	stageCodexFingerprintIDs(c, ids)
	var req *http.Request
	if passthrough {
		req, err = svc.buildUpstreamRequestOpenAIPassthrough(context.Background(), c, account, body, "test")
	} else {
		req, err = svc.buildUpstreamRequest(context.Background(), c, account, body, "test", true, "", true)
	}
	require.NoError(t, err)
	out, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	require.NoError(t, req.Body.Close())
	result := codexTopologyOutbound{req.Header, gjson.ParseBytes(out)}
	for _, carrier := range result.carriers() {
		require.Equal(t, result.session(), carrier.Get("session_id").String())
		require.Equal(t, result.thread(), carrier.Get("thread_id").String())
		for _, field := range []string{"turn_id", "parent_turn_id", "root_turn_id", "turn_started_at_unix_ms"} {
			require.Equal(t, gjson.Get(fixture, field).Value(), carrier.Get(field).Value(), field)
		}
	}
	return result
}

func TestCodexSideSessionForkAfterPeriodExpiry(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		t.Run(fmt.Sprintf("passthrough=%v", passthrough), func(t *testing.T) {
			server := miniredis.RunT(t)
			account := newTestOAuthAccount(7620, map[string]any{codexFingerprintModeExtraKey: "session"})
			account.Credentials = map[string]any{"chatgpt_account_id": "side-account"}
			seed, _ := codexFingerprintSeed(account.Extra)
			accountScope := codexSessionIdentityUpstreamScope(account)
			period := resolveCodexSessionPeriod(seed, "user:1", accountScope, time.Now())
			before := period.expiresAt.Add(-codexSessionPeriodGrace).Add(-time.Second)
			after := period.expiresAt.Add(time.Second)
			root := buildCodexTopologyAt(t, newCodexPeriodRedisService(t, server), account, codexRootTopology, before, 1, 11, passthrough)
			server.FastForward(after.Sub(before))
			require.False(t, server.Exists("openai_codex_session_identity:"+period.key))

			// Directly fork in epoch 2: the parent is deliberately not requested
			// again. A new gateway instance must recover its epoch 1 thread.
			side := buildCodexTopologyAt(t, newCodexPeriodRedisService(t, server), account, codexSideTopology, after, 1, 99, passthrough)
			require.NotEqual(t, root.session(), side.session())
			require.NotEqual(t, root.thread(), side.thread())
			require.NotEqual(t, root.cache(), side.cache())
			for _, carrier := range side.carriers() {
				require.Equal(t, root.thread(), carrier.Get("forked_from_thread_id").String())
			}
			nextPeriod := resolveCodexSessionPeriod(seed, "user:1", accountScope, after)
			require.False(t, server.Exists("openai_codex_session_identity:"+nextPeriod.key), "creating a side must not create a period session")
			newTask := strings.ReplaceAll(codexRootTopology, "000000000001", "000000000009")
			normal := buildCodexTopologyAt(t, newCodexPeriodRedisService(t, server), account, newTask, after, 1, 11, passthrough)
			require.NotEqual(t, root.session(), normal.session())
			require.NotEqual(t, normal.session(), side.session())

			// Side lifetime is the original session's lifetime, independent of
			// repeated normal period expiry, service restarts and API key changes.
			server.FastForward(30 * 24 * time.Hour)
			later := after.Add(30 * 24 * time.Hour)
			continuation := buildCodexTopologyAt(t, newCodexPeriodRedisService(t, server), account, codexSideContinuationTopology, later, 1, 101, passthrough)
			child := buildCodexTopologyAt(t, newCodexPeriodRedisService(t, server), account, codexSideChildTopology, later, 1, 102, passthrough)
			require.Equal(t, side.session(), continuation.session())
			require.Equal(t, side.thread(), continuation.thread())
			require.Equal(t, side.cache(), continuation.cache())
			require.Equal(t, side.session(), child.session())
			require.Equal(t, side.cache(), child.cache())
			require.NotEqual(t, side.thread(), child.thread())
			require.Equal(t, side.thread(), child.headers.Get("x-codex-parent-thread-id"))
			sideKey := codexHTTPIdentityMappingKey("side-session", "user:1", accountScope, gjson.Get(codexSideTopology, "session_id").String())
			require.True(t, server.Exists("openai_codex_session_identity:"+sideKey))
			require.Zero(t, server.TTL("openai_codex_session_identity:"+sideKey))
			// Re-visiting the original parent also recovers the old thread ID.
			rootAgain := buildCodexTopologyAt(t, newCodexPeriodRedisService(t, server), account, codexRootTopology, later, 1, 103, passthrough)
			require.Equal(t, root.thread(), rootAgain.thread())
			require.NotEqual(t, root.session(), rootAgain.session())
		})
	}
}

func TestCodexSideSessionConcurrentRegistrationAcrossEpochs(t *testing.T) {
	server := miniredis.RunT(t)
	services := []*OpenAIGatewayService{newCodexPeriodRedisService(t, server), newCodexPeriodRedisService(t, server)}
	account := newTestOAuthAccount(7621, map[string]any{codexFingerprintModeExtraKey: "session"})
	seed, _ := codexFingerprintSeed(account.Extra)
	period := resolveCodexSessionPeriod(seed, "user:1", codexSessionIdentityUpstreamScope(account), time.Now())
	boundary := period.expiresAt.Add(-codexSessionPeriodGrace)
	root := buildCodexTopologyAt(t, services[0], account, codexRootTopology, boundary.Add(-time.Second), 1, 11, false)
	var wg sync.WaitGroup
	results := make(chan *codexFingerprintIDs, 16)
	for i := range 16 {
		wg.Go(func() {
			c := newCodexSessionIdentityV2Context(t, 1, int64(100+i))
			stageCodexSessionIdentityInputMap(c, map[string]any{"client_metadata": map[string]any{openAIWSTurnMetadataHeader: codexSideTopology}})
			now := boundary.Add(time.Duration(i%2*2-1) * time.Millisecond)
			ids, err := services[i%2].resolveCodexHTTPFingerprintIDs(context.Background(), c, account, now)
			require.NoError(t, err)
			results <- ids
		})
	}
	wg.Wait()
	close(results)
	var first *codexFingerprintIDs
	for ids := range results {
		if first == nil {
			first = ids
		}
		require.True(t, isCodexUUIDv7(ids.sessionID))
		require.NotEqual(t, root.session(), ids.sessionID)
		require.Equal(t, root.thread(), ids.forkedFromThreadID)
		require.Equal(t, first.sessionID, ids.sessionID)
		require.Equal(t, first.threadID, ids.threadID)
		require.Equal(t, first.promptCacheKey, ids.promptCacheKey)
	}
	require.Len(t, server.Keys(), 4, "one period, two threads and one side session")
}

func TestCodexSideSessionMissingReferenceIsNotInvented(t *testing.T) {
	for _, fixture := range []string{codexSideTopology, codexSideChildTopology} {
		server := miniredis.RunT(t)
		svc := newCodexPeriodRedisService(t, server)
		account := newTestOAuthAccount(7622, map[string]any{codexFingerprintModeExtraKey: "session"})
		c := newCodexSessionIdentityV2Context(t, 1, 11)
		stageCodexSessionIdentityInputMap(c, map[string]any{"client_metadata": map[string]any{openAIWSTurnMetadataHeader: fixture}})
		_, err := svc.resolveCodexHTTPFingerprintIDs(context.Background(), c, account, time.Now())
		require.ErrorIs(t, err, ErrCodexSessionIdentityNotFound)
		require.Empty(t, server.Keys(), "no guessed parent/fork target or partially registered side")
	}
}

func TestCodexSideSessionReadFailureDoesNotFallBackToPeriod(t *testing.T) {
	server := miniredis.RunT(t)
	account := newTestOAuthAccount(7623, map[string]any{codexFingerprintModeExtraKey: "session"})
	now := time.Now()
	svc := newCodexPeriodRedisService(t, server)
	buildCodexTopologyAt(t, svc, account, codexRootTopology, now, 1, 11, false)
	side := buildCodexTopologyAt(t, svc, account, codexSideTopology, now, 1, 11, false)
	keys := server.Keys()
	c := newCodexSessionIdentityV2Context(t, 1, 99)
	stageCodexSessionIdentityInputMap(c, map[string]any{"client_metadata": map[string]any{openAIWSTurnMetadataHeader: codexSideContinuationTopology}})
	server.SetError("ERR injected identity store failure")
	ids, err := svc.resolveCodexHTTPFingerprintIDs(context.Background(), c, account, now.Add(30*24*time.Hour))
	require.ErrorContains(t, err, "injected identity store failure")
	require.Nil(t, ids)
	server.SetError("")
	require.Equal(t, keys, server.Keys(), "a failed side lookup must not allocate a period session")
	continued := buildCodexTopologyAt(t, svc, account, codexSideContinuationTopology, now.Add(30*24*time.Hour), 1, 99, false)
	require.Equal(t, side.session(), continued.session())
}
