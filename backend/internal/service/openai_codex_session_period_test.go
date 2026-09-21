package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

var fingerprintProjectionTestStore = &codexSessionIdentityTestStore{values: make(map[string]string)}

// Existing projection tests use a real v3 snapshot with a fixed authenticated
// scope. End-to-end tests below separately exercise different users and tasks.
func resolveFingerprintFromRequestForTest(t *testing.T, account *Account, headers http.Header) *codexFingerprintIDs {
	t.Helper()
	c := newCodexSessionIdentityV2Context(t, 41, 51)
	if headers != nil {
		c.Request.Header = headers.Clone()
	}
	stageCodexSessionIdentityInputMap(c, nil)
	svc := &OpenAIGatewayService{cache: fingerprintProjectionTestStore}
	ids, err := svc.resolveCodexHTTPFingerprintIDs(context.Background(), c, account, time.Now())
	require.NoError(t, err)
	return ids
}

func resolveFingerprintForTest(t *testing.T, account *Account, session string, mode codexFingerprintMode) *codexFingerprintIDs {
	t.Helper()
	if mode != codexFingerprintSession {
		return resolveCodexFingerprintIDs(account, session, mode)
	}
	headers := make(http.Header)
	headers.Set("session-id", session)
	return resolveFingerprintFromRequestForTest(t, account, headers)
}

func newCodexPeriodRedisService(t *testing.T, server *miniredis.Miniredis) *OpenAIGatewayService {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return &OpenAIGatewayService{cache: &codexSessionIdentityRedisStore{client: client}, cfg: &config.Config{}, toolCorrector: NewCodexToolCorrector()}
}

func codexPeriodInput(t *testing.T, userID, apiKeyID int64, session, task, parent, cache string) (*gin.Context, []byte) {
	t.Helper()
	c := newCodexSessionIdentityV2Context(t, userID, apiKeyID)
	c.Request.Header.Set("User-Agent", "codex_cli_rs/0.144.1")
	c.Request.Header.Set("originator", "codex_cli_rs")
	c.Request.Header.Set("session-id", session)
	c.Request.Header.Set("thread-id", task)
	c.Request.Header.Set("x-client-request-id", "request-id-must-not-outrank-task")
	turn := uuid.Must(uuid.NewV7()).String()
	metadata := map[string]any{
		"session_id": session, "thread_id": task, "turn_id": turn,
		"turn-id": "old-turn-alias", "session-id": "old-session-alias", "thread-id": "old-thread-alias",
		"window_id": "old-window", "installation_id": "client-device",
	}
	nested := map[string]any{"session_id": session, "thread_id": task, "turn_id": turn, "turn-id": "old-turn-alias", "sandbox": "seatbelt", "sandbox_mode": "workspace-write"}
	if parent != "" {
		// Exercise the unprefixed flat parent carrier, not just the header.
		metadata["parent_thread_id"] = parent
	}
	rawNested, err := json.Marshal(nested)
	require.NoError(t, err)
	metadata[openAIWSTurnMetadataHeader] = string(rawNested)
	c.Request.Header.Set(openAIWSTurnMetadataHeader, string(rawNested))
	body, err := json.Marshal(map[string]any{
		"model": "gpt-5.2", "instructions": "Help with this task.", "stream": true,
		"input":           []any{map[string]any{"type": "message", "role": "user", "content": "hi"}},
		"client_metadata": metadata, "prompt_cache_key": cache,
	})
	require.NoError(t, err)
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	return c, body
}

type codexPeriodOutbound struct {
	session, thread, turn, cache, installation, parent string
}

func checkCodexPeriodOutbound(t *testing.T, headers http.Header, body []byte) codexPeriodOutbound {
	t.Helper()
	cm := gjson.GetBytes(body, "client_metadata")
	result := codexPeriodOutbound{
		session: headers.Get("session-id"), thread: headers.Get("thread-id"),
		turn: cm.Get("turn_id").String(), cache: gjson.GetBytes(body, "prompt_cache_key").String(),
		installation: headers.Get("x-codex-installation-id"), parent: headers.Get("x-codex-parent-thread-id"),
	}
	require.True(t, isCodexUUIDv7(result.session))
	require.True(t, isCodexUUIDv7(result.turn))
	require.NotEqual(t, result.session, result.thread)
	require.Equal(t, result.session, headers.Get("session_id"))
	require.Equal(t, result.thread, headers.Get("thread_id"))
	require.Equal(t, result.thread, headers.Get("x-client-request-id"))
	for _, raw := range []string{cm.Raw, cm.Get(openAIWSTurnMetadataHeader).String(), headers.Get(openAIWSTurnMetadataHeader)} {
		require.Equal(t, result.session, gjson.Get(raw, "session_id").String())
		require.Equal(t, result.thread, gjson.Get(raw, "thread_id").String())
		require.Equal(t, result.turn, gjson.Get(raw, "turn_id").String())
		require.Equal(t, result.turn, gjson.Get(raw, "turn-id").String())
		require.Equal(t, result.thread+":0", gjson.Get(raw, "window_id").String())
		require.Equal(t, result.installation, gjson.Get(raw, "installation_id").String())
		require.Equal(t, result.parent, gjson.Get(raw, "parent_thread_id").String())
		require.Positive(t, gjson.Get(raw, "turn_started_at_unix_ms").Int())
	}
	require.Equal(t, result.turn, cm.Get("turn-id").String())
	require.Equal(t, result.session, cm.Get("session-id").String())
	require.Equal(t, result.thread, cm.Get("thread-id").String())
	require.Equal(t, result.thread+":0", headers.Get("x-codex-window-id"))
	return result
}

func TestCodexSessionPeriodHTTPUserTaskIsolation(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		t.Run(fmt.Sprintf("passthrough=%v", passthrough), func(t *testing.T) {
			server := miniredis.RunT(t)
			svc := newCodexPeriodRedisService(t, server)
			account := newTestOAuthAccount(7601, map[string]any{codexFingerprintModeExtraKey: "session", "openai_passthrough": passthrough})
			account.Concurrency = 1
			account.Credentials = map[string]any{"access_token": "test", "chatgpt_account_id": "period-account"}
			forward := func(userID, keyID int64, session, task, parent, cache string) codexPeriodOutbound {
				t.Helper()
				c, body := codexPeriodInput(t, userID, keyID, session, task, parent, cache)
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: [DONE]\n\n"))}}
				svc.httpUpstream = upstream
				_, err := svc.Forward(context.Background(), c, account, body)
				require.NoError(t, err)
				require.Equal(t, passthrough, c.GetBool("openai_passthrough"))
				return checkCodexPeriodOutbound(t, upstream.lastReq.Header, upstream.lastBody)
			}
			rawV7 := uuid.Must(uuid.NewV7()).String()
			a := forward(1, 11, rawV7, "fix-bug", "", rawV7)
			b := forward(1, 11, rawV7, "refactor", "", rawV7)
			require.Equal(t, a.session, b.session)
			require.NotEqual(t, a.thread, b.thread)
			require.Equal(t, a.cache, b.cache, "threads sharing the original session/cache root share cache affinity")
			require.NotEqual(t, a.turn, b.turn)
			require.NotEqual(t, rawV7, a.session, "UUIDv7 cannot bypass user-period convergence")
			other := forward(2, 22, rawV7, "fix-bug", "", rawV7)
			require.NotEqual(t, a.session, other.session)
			require.NotEqual(t, a.thread, other.thread)
			require.NotEqual(t, a.cache, other.cache)
			require.Equal(t, a.installation, other.installation)
			for i := range 3 {
				again := forward(1, int64(50+i), rawV7, "fix-bug", "", rawV7)
				require.Equal(t, a.session, again.session, "API key rotation must not create a user session")
				require.Equal(t, a.thread, again.thread)
				require.Equal(t, a.cache, again.cache)
				require.NotEqual(t, a.turn, again.turn)
				a = again
			}
			for _, raw := range []string{uuid.NewString(), "opaque-session"} {
				again := forward(1, 11, raw, "fix-bug", "", raw)
				require.Equal(t, a.session, again.session)
				require.Equal(t, a.thread, again.thread)
				require.NotEqual(t, a.cache, again.cache, "a new original session/cache root receives its own cache")
			}
			child := forward(1, 11, rawV7, "write-tests", "fix-bug", rawV7)
			require.Equal(t, a.session, child.session)
			require.NotEqual(t, a.thread, child.thread)
			require.Equal(t, a.thread, child.parent)
			require.Equal(t, a.cache, child.cache)
			explicit := forward(1, 11, rawV7, "fix-bug", "", "explicit-cache")
			explicitAgain := forward(1, 51, rawV7, "fix-bug", "", "explicit-cache")
			require.Equal(t, explicit.cache, explicitAgain.cache)
			require.NotEqual(t, a.cache, explicit.cache)
			require.Equal(t, explicit.cache, forward(1, 11, rawV7, "refactor", "", "explicit-cache").cache)
			account.Credentials["chatgpt_account_id"] = "another-oauth-account"
			failover := forward(1, 11, rawV7, "fix-bug", "", rawV7)
			require.NotEqual(t, a.session, failover.session)
			require.NotEqual(t, a.thread, failover.thread)
			require.NotEqual(t, a.cache, failover.cache)
		})
	}
}

func TestCodexSessionPeriodFixedScheduleAndScope(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	boundaries := map[time.Time]bool{}
	for i := range 50 {
		p := resolveCodexSessionPeriod("seed", fmt.Sprintf("user:%d", i), "account", now)
		require.GreaterOrEqual(t, p.duration, codexSessionPeriodMin)
		require.LessOrEqual(t, p.duration, codexSessionPeriodMax)
		boundary := p.expiresAt.Add(-codexSessionPeriodGrace)
		before := resolveCodexSessionPeriod("seed", fmt.Sprintf("user:%d", i), "account", boundary.Add(-time.Millisecond))
		require.Equal(t, p, before)
		after := resolveCodexSessionPeriod("seed", fmt.Sprintf("user:%d", i), "account", boundary)
		require.NotEqual(t, p.key, after.key)
		require.Equal(t, p.epoch+1, after.epoch)
		boundaries[boundary] = true
	}
	require.Len(t, boundaries, 50, "users must not rotate on one shared boundary")
	c := newCodexSessionIdentityV2Context(t, 1, 11)
	require.Equal(t, "user:1", codexSessionIdentityDownstreamScope(c, 11))
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.UserID, int64(2)))
	require.Equal(t, "user:2", codexSessionIdentityDownstreamScope(c, 11))
	require.Equal(t, "api-key:12", codexSessionIdentityDownstreamScope(nil, 12))
	require.Empty(t, codexSessionIdentityDownstreamScope(nil, 0))
}

func TestCodexSessionPeriodRedisConcurrentCreationAndHardExpiry(t *testing.T) {
	server := miniredis.RunT(t)
	services := []*OpenAIGatewayService{newCodexPeriodRedisService(t, server), newCodexPeriodRedisService(t, server)}
	account := newTestOAuthAccount(7602, map[string]any{codexFingerprintModeExtraKey: "session"})
	now := time.Now()
	seed, _ := codexFingerprintSeed(account.Extra)
	period := resolveCodexSessionPeriod(seed, "user:1", codexSessionIdentityUpstreamScope(account), now)
	var wg sync.WaitGroup
	results := make(chan *codexFingerprintIDs, 16)
	for i := range 16 {
		wg.Go(func() {
			c, body := codexPeriodInput(t, 1, int64(i+1), uuid.NewString(), "same-task", "", "")
			stageCodexSessionIdentityInputRaw(c, body)
			ids, err := services[i%2].resolveCodexHTTPFingerprintIDs(context.Background(), c, account, now)
			require.NoError(t, err)
			results <- ids
		})
	}
	wg.Wait()
	close(results)
	var first *codexFingerprintIDs
	turns := map[string]bool{}
	for ids := range results {
		if first == nil {
			first = ids
		}
		require.Equal(t, first.sessionID, ids.sessionID)
		require.Equal(t, first.threadID, ids.threadID)
		turns[ids.turnID] = true
	}
	require.Len(t, turns, 16)
	require.Len(t, server.Keys(), 1)
	key := "openai_codex_session_identity:" + period.key
	ttl := server.TTL(key)
	require.InDelta(t, period.expiresAt.Sub(now).Milliseconds(), ttl.Milliseconds(), 1)
	server.FastForward(time.Minute)
	c, body := codexPeriodInput(t, 1, 99, "new-client-session", "new-task", "", "")
	stageCodexSessionIdentityInputRaw(c, body)
	ids, err := services[1].resolveCodexHTTPFingerprintIDs(context.Background(), c, account, now.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, first.sessionID, ids.sessionID)
	require.Equal(t, ttl-time.Minute, server.TTL(key), "a new task must not refresh the epoch TTL")
	server.FastForward(ttl)
	require.False(t, server.Exists(key), "old epochs must not accumulate")
}

func TestCodexSessionPeriodHTTPBoundaryAndAuthoritativeNormalization(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		t.Run(fmt.Sprintf("passthrough=%v", passthrough), func(t *testing.T) {
			server := miniredis.RunT(t)
			svc := newCodexPeriodRedisService(t, server)
			account := newTestOAuthAccount(7603, map[string]any{codexFingerprintModeExtraKey: "session"})
			account.Credentials = map[string]any{"access_token": "test", "chatgpt_account_id": "period-boundary"}
			seed, _ := codexFingerprintSeed(account.Extra)
			period := resolveCodexSessionPeriod(seed, "user:1", codexSessionIdentityUpstreamScope(account), time.Now())
			boundary := period.expiresAt.Add(-codexSessionPeriodGrace)
			rawSession := uuid.Must(uuid.NewV7()).String()
			build := func(now time.Time) codexPeriodOutbound {
				c, body := codexPeriodInput(t, 1, 11, rawSession, "same-task", "parent-task", "")
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
				result := checkCodexPeriodOutbound(t, req.Header, out)
				require.Equal(t, ids.sessionID, result.session)
				require.Equal(t, now.UnixMilli(), gjson.GetBytes(out, "client_metadata.turn_started_at_unix_ms").Int())
				// Deliberately feed stale carriers into the late map pass too.
				var decoded map[string]any
				require.NoError(t, json.Unmarshal(body, &decoded))
				staleHeaders := c.Request.Header.Clone()
				_, _, err = svc.normalizeCodexOutboundIdentityMap(context.Background(), c, account, staleHeaders, decoded, "stale-session")
				require.NoError(t, err)
				require.Equal(t, result, checkCodexPeriodOutbound(t, staleHeaders, mustJSONForSessionIdentityTest(t, decoded)))
				return result
			}
			before := build(boundary.Add(-time.Millisecond))
			after := build(boundary)
			require.NotEqual(t, before.session, after.session)
			require.NotEqual(t, before.thread, after.thread, "tasks move to a new thread namespace at the epoch boundary")
			require.NotEqual(t, before.parent, after.parent, "parent references follow the same new namespace")
			require.NotEqual(t, before.cache, after.cache)
			require.Equal(t, before.installation, after.installation)
			require.Len(t, server.Keys(), 2, "only v3 epochs are written; no second UUIDv7 isolation mapping")
		})
	}
}

func TestCodexSessionPeriodTaskFallbackAndMissingIdentity(t *testing.T) {
	svc := newCodexPeriodRedisService(t, miniredis.RunT(t))
	account := newTestOAuthAccount(7604, map[string]any{codexFingerprintModeExtraKey: "session"})
	now := time.Now()
	resolve := func(userID, keyID int64, session, task, requestID string) *codexFingerprintIDs {
		c := newCodexSessionIdentityV2Context(t, userID, keyID)
		c.Request.Header.Set("session-id", session)
		c.Request.Header.Set("thread-id", task)
		c.Request.Header.Set("x-client-request-id", requestID)
		stageCodexSessionIdentityInputMap(c, nil)
		ids, err := svc.resolveCodexHTTPFingerprintIDs(context.Background(), c, account, now)
		require.NoError(t, err)
		return ids
	}
	thread := resolve(1, 11, "session", "task", "request1")
	require.Equal(t, thread.threadID, resolve(1, 12, "another-session", "task", "request2").threadID)
	session := resolve(1, 11, "task", "", "request1")
	require.Equal(t, thread.threadID, session.threadID, "thread and session fallback use the same task namespace")
	require.Equal(t, session.threadID, resolve(1, 12, "task", "", "request2").threadID)
	require.Equal(t, thread.threadID, resolve(1, 11, "", "", "task").threadID)
	keyOnly := resolve(0, 11, "session", "task", "")
	require.Equal(t, keyOnly.sessionID, resolve(0, 11, "session2", "task2", "").sessionID)
	require.NotEqual(t, keyOnly.sessionID, resolve(0, 12, "session", "task", "").sessionID)
	require.Equal(t, codexFingerprintDevice, resolve(0, 0, "session", "task", "").mode)
	require.Equal(t, codexFingerprintDevice, resolve(1, 11, "", "", "").mode)
}

func TestCodexSessionPeriodHTTPFailsWithoutDurableStore(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		t.Run(fmt.Sprintf("passthrough=%v", passthrough), func(t *testing.T) {
			account := newTestOAuthAccount(7605, map[string]any{codexFingerprintModeExtraKey: "session", "openai_passthrough": passthrough})
			account.Credentials = map[string]any{"access_token": "test"}
			upstream := &httpUpstreamRecorder{}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream, toolCorrector: NewCodexToolCorrector()}
			c, body := codexPeriodInput(t, 1, 11, uuid.NewString(), "task", "", "")
			_, err := svc.Forward(context.Background(), c, account, body)
			require.ErrorIs(t, err, ErrCodexSessionIdentityStoreUnavailable)
			require.Nil(t, upstream.lastReq, "do not forward a temporary session when the store is unavailable")
		})
	}
}
