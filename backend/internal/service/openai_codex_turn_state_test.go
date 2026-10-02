package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newTurnStateTestContext(t *testing.T, apiKeyID int64, sessionID string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	if sessionID != "" {
		c.Request.Header.Set("session_id", sessionID)
	}
	if apiKeyID > 0 {
		c.Set("api_key", &APIKey{ID: apiKeyID})
	}
	return c, rec
}

// turnStateTestCache 嵌入 GatewayCache 接口以复用其余方法集（与
// stickyGatewayCacheHotpathStub 同样的手法），只实现溯源存储的两个方法。
// 生产实现是 repository.gatewayCache 的 Redis 版本，这里验证的是 service 侧的
// 读写契约与判定语义。
type turnStateTestCache struct {
	GatewayCache
	origins map[string]string
	ttls    map[string]time.Duration
}

func newTurnStateTestCache() *turnStateTestCache {
	return &turnStateTestCache{origins: map[string]string{}, ttls: map[string]time.Duration{}}
}

func (c *turnStateTestCache) SetCodexTurnStateOrigin(_ context.Context, key, value string, ttl time.Duration) error {
	c.origins[key] = value
	c.ttls[key] = ttl
	return nil
}

func (c *turnStateTestCache) GetCodexTurnStateOrigin(_ context.Context, key string) (string, error) {
	value, ok := c.origins[key]
	if !ok {
		return "", ErrCodexTurnStateOriginNotFound
	}
	return value, nil
}

// origin 按下游会话 seed 读回溯源记录，供断言使用。
func (c *turnStateTestCache) origin(t *testing.T, seed string) (openAICodexTurnStateOrigin, bool) {
	t.Helper()
	raw, ok := c.origins[codexTurnStateOriginKey(seed)]
	if !ok {
		return openAICodexTurnStateOrigin{}, false
	}
	var origin openAICodexTurnStateOrigin
	require.NoError(t, json.Unmarshal([]byte(raw), &origin))
	return origin, true
}

func newTurnStateTestService() (*OpenAIGatewayService, *turnStateTestCache) {
	cache := newTurnStateTestCache()
	return &OpenAIGatewayService{cache: cache}, cache
}

// codexTurnStateTestAccount 必须是 OAuth 类型：stagedCodexFingerprintIDs 只对
// Codex 协议的账号返回暂存的收敛 ID，身份戳因此才有值。
func codexTurnStateTestAccount(id int64) *Account {
	return &Account{ID: id, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
}

func stageCodexTurnStateIdentity(c *gin.Context, account *Account, mode codexFingerprintMode, sessionID string) {
	stageCodexFingerprintIDs(c, &codexFingerprintIDs{accountID: account.ID, mode: mode, sessionID: sessionID})
}

func TestOpenAICodexTurnStateSeed(t *testing.T) {
	c, _ := newTurnStateTestContext(t, 7, "sess-1")
	require.Equal(t, "7\x00sess-1", openAICodexTurnStateSeed(c))

	// 连字符形式优先（Codex CLI 标准头）
	c.Request.Header.Set("session-id", "sess-hyphen")
	require.Equal(t, "7\x00sess-hyphen", openAICodexTurnStateSeed(c))

	// 无会话标识 → 不跟踪
	cNoSession, _ := newTurnStateTestContext(t, 7, "")
	require.Empty(t, openAICodexTurnStateSeed(cNoSession))

	require.Empty(t, openAICodexTurnStateSeed(nil))
}

// 溯源键是 seed 的单向摘要：raw 的 API Key ID 与客户端会话标识不得进入键名。
func TestCodexTurnStateOriginKey_HidesRawIdentifiers(t *testing.T) {
	key := codexTurnStateOriginKey("7\x00sess-raw")
	require.NotContains(t, key, "sess-raw")
	require.NotContains(t, key, "\x00")
	require.Equal(t, key, codexTurnStateOriginKey("7\x00sess-raw"), "同一 seed 必须稳定映射到同一键")
	require.NotEqual(t, key, codexTurnStateOriginKey("7\x00sess-other"))
	require.NotEqual(t, key, codexTurnStateOriginKey("8\x00sess-raw"), "API Key 必须参与区分")
}

func TestRelayOpenAICodexTurnState_SetsHeaderAndRecordsProvenance(t *testing.T) {
	svc, cache := newTurnStateTestService()
	account := codexTurnStateTestAccount(42)
	c, _ := newTurnStateTestContext(t, 7, "sess-relay")

	upstream := http.Header{}
	upstream.Set("x-codex-turn-state", "blob-A")
	svc.relayOpenAICodexTurnState(c, account, upstream)

	require.Equal(t, "blob-A", c.Writer.Header().Get("X-Codex-Turn-State"))

	origin, ok := cache.origin(t, "7\x00sess-relay")
	require.True(t, ok)
	require.Equal(t, int64(42), origin.AccountID)
	require.Equal(t, openaiStickySessionTTL, cache.ttls[codexTurnStateOriginKey("7\x00sess-relay")],
		"TTL 沿用粘性会话窗口，过期交由 Redis 承担")
}

func TestRelayOpenAICodexTurnState_ClearsStaleValueWhenUpstreamAbsent(t *testing.T) {
	svc, cache := newTurnStateTestService()
	c, _ := newTurnStateTestContext(t, 7, "sess-stale")
	// 模拟上一 failover attempt 残留的值
	c.Writer.Header().Set("X-Codex-Turn-State", "blob-old")

	svc.relayOpenAICodexTurnState(c, codexTurnStateTestAccount(43), http.Header{})

	require.Empty(t, c.Writer.Header().Get("X-Codex-Turn-State"))
	_, ok := cache.origin(t, "7\x00sess-stale")
	require.False(t, ok)
}

func TestStageOpenAICodexTurnState_StagedHeaders(t *testing.T) {
	svc, cache := newTurnStateTestService()
	c, _ := newTurnStateTestContext(t, 9, "sess-staged")

	// nil 集合 + 上游有值 → 创建集合并写入，但此刻还不记录溯源
	var staged http.Header
	upstream := http.Header{}
	upstream.Set("x-codex-turn-state", "blob-B")
	stageOpenAICodexTurnState(&staged, upstream)
	require.NotNil(t, staged)
	require.Equal(t, "blob-B", staged.Get("X-Codex-Turn-State"))
	_, noted := cache.origin(t, "9\x00sess-staged")
	require.False(t, noted, "暂存阶段不得记录溯源：该 attempt 仍可能 failover 丢弃")

	// 真正提交时才记录
	svc.noteStagedOpenAICodexTurnStateCommitted(c, codexTurnStateTestAccount(44), staged)
	origin, ok := cache.origin(t, "9\x00sess-staged")
	require.True(t, ok)
	require.Equal(t, int64(44), origin.AccountID)

	// 上游无值 → 清除已暂存的值；nil 集合保持 nil
	stageOpenAICodexTurnState(&staged, http.Header{})
	require.Empty(t, staged.Get("X-Codex-Turn-State"))
	var nilStaged http.Header
	stageOpenAICodexTurnState(&nilStaged, http.Header{})
	require.Nil(t, nilStaged)
}

// 首输出超时导致 attempt 被丢弃时，溯源不得被该 attempt 污染——否则后续
// 请求会把客户端持有的合法 blob 误判成跨账号回带而剥离。
func TestStagedTurnState_AbandonedAttemptDoesNotPoisonProvenance(t *testing.T) {
	svc, cache := newTurnStateTestService()
	c, _ := newTurnStateTestContext(t, 11, "sess-abandoned")

	// 账号 A 的 attempt 暂存了 blob，但从未提交（首输出超时 → failover）
	var staged http.Header
	upstreamA := http.Header{}
	upstreamA.Set("x-codex-turn-state", "blob-A")
	stageOpenAICodexTurnState(&staged, upstreamA)

	// 账号 B 接手并真正提交
	accountB := codexTurnStateTestAccount(52)
	svc.relayOpenAICodexTurnState(c, accountB, upstreamA)

	// 客户端回带的 blob 来自 B，出站到 B 时不得被剥离
	h := http.Header{}
	h.Set("x-codex-turn-state", "blob-A")
	svc.guardOpenAICodexTurnStateEcho(c, accountB, h)
	require.Equal(t, "blob-A", h.Get("x-codex-turn-state"))

	origin, ok := cache.origin(t, "11\x00sess-abandoned")
	require.True(t, ok)
	require.Equal(t, int64(52), origin.AccountID)
}

func TestNoteStagedOpenAICodexTurnStateCommitted_NoopWithoutState(t *testing.T) {
	svc, cache := newTurnStateTestService()
	c, _ := newTurnStateTestContext(t, 12, "sess-nostate")

	svc.noteStagedOpenAICodexTurnStateCommitted(c, codexTurnStateTestAccount(60), nil)
	svc.noteStagedOpenAICodexTurnStateCommitted(c, codexTurnStateTestAccount(60), http.Header{"X-Request-Id": []string{"rid"}})

	_, ok := cache.origin(t, "12\x00sess-nostate")
	require.False(t, ok)
}

// 透传路径曾在拿到上游 200 响应头时就记账，但那时客户端一个字节都没收到
// （首个可见输出前 pendingLines 全缓冲），而首输出失败、断流、空终止事件都会
// 触发 failover 丢弃该 attempt。记账必须等到响应真正提交。
func TestNotePassthroughOpenAICodexTurnStateCommitted(t *testing.T) {
	const seed = "7\x00sess-passthrough"

	newResp := func(state string) *http.Response {
		resp := &http.Response{Header: http.Header{}}
		if state != "" {
			resp.Header.Set(openAICodexTurnStateHeader, state)
		}
		return resp
	}
	// gin 的 WriteHeader 只设 status，Written() 要等真正写出 body 才为 true——
	// 生产路径的 c.Data / pendingLines / keepalive 都会真写字节。
	commitResponse := func(t *testing.T, c *gin.Context) {
		t.Helper()
		_, err := c.Writer.WriteString("data: {}\n\n")
		require.NoError(t, err)
		require.True(t, c.Writer.Written())
	}

	t.Run("uncommitted_response_does_not_record", func(t *testing.T) {
		svc, cache := newTurnStateTestService()
		c, _ := newTurnStateTestContext(t, 7, "sess-passthrough")

		svc.notePassthroughOpenAICodexTurnStateCommitted(c, codexTurnStateTestAccount(42), newResp("blob-A"))

		require.False(t, c.Writer.Written())
		_, ok := cache.origin(t, seed)
		require.False(t, ok, "响应尚未提交，被丢弃的 attempt 不得留下溯源")
	})

	t.Run("committed_response_records", func(t *testing.T) {
		svc, cache := newTurnStateTestService()
		c, _ := newTurnStateTestContext(t, 7, "sess-passthrough")
		commitResponse(t, c)

		svc.notePassthroughOpenAICodexTurnStateCommitted(c, codexTurnStateTestAccount(42), newResp("blob-A"))

		origin, ok := cache.origin(t, seed)
		require.True(t, ok)
		require.Equal(t, int64(42), origin.AccountID)
	})

	t.Run("upstream_without_blob_does_not_record", func(t *testing.T) {
		svc, cache := newTurnStateTestService()
		c, _ := newTurnStateTestContext(t, 7, "sess-passthrough")
		commitResponse(t, c)

		svc.notePassthroughOpenAICodexTurnStateCommitted(c, codexTurnStateTestAccount(42), newResp(""))

		_, ok := cache.origin(t, seed)
		require.False(t, ok, "上游没给 blob，客户端手里也没有，无需记账")
	})

	t.Run("nil_response_is_noop", func(t *testing.T) {
		svc, cache := newTurnStateTestService()
		c, _ := newTurnStateTestContext(t, 7, "sess-passthrough")
		commitResponse(t, c)

		require.NotPanics(t, func() {
			svc.notePassthroughOpenAICodexTurnStateCommitted(c, codexTurnStateTestAccount(42), nil)
		})
		_, ok := cache.origin(t, seed)
		require.False(t, ok)
	})
}

func TestGuardOpenAICodexTurnStateEcho(t *testing.T) {
	newOutbound := func(state string) http.Header {
		h := http.Header{}
		if state != "" {
			h.Set("x-codex-turn-state", state)
		}
		return h
	}

	t.Run("same_account_keeps_echo", func(t *testing.T) {
		svc, _ := newTurnStateTestService()
		c, _ := newTurnStateTestContext(t, 7, "sess-g1")
		upstream := http.Header{}
		upstream.Set("x-codex-turn-state", "blob-A")
		svc.relayOpenAICodexTurnState(c, codexTurnStateTestAccount(42), upstream)

		h := newOutbound("blob-A")
		svc.guardOpenAICodexTurnStateEcho(c, codexTurnStateTestAccount(42), h)
		require.Equal(t, "blob-A", h.Get("x-codex-turn-state"))
	})

	t.Run("foreign_account_strips_echo", func(t *testing.T) {
		svc, _ := newTurnStateTestService()
		c, _ := newTurnStateTestContext(t, 7, "sess-g2")
		upstream := http.Header{}
		upstream.Set("x-codex-turn-state", "blob-A")
		svc.relayOpenAICodexTurnState(c, codexTurnStateTestAccount(42), upstream)

		// failover 换到账号 43：blob 由 42 铸造，必须剥离
		h := newOutbound("blob-A")
		svc.guardOpenAICodexTurnStateEcho(c, codexTurnStateTestAccount(43), h)
		require.Empty(t, h.Get("x-codex-turn-state"))
	})

	t.Run("no_provenance_passthrough", func(t *testing.T) {
		svc, _ := newTurnStateTestService()
		c, _ := newTurnStateTestContext(t, 7, "sess-g3")
		h := newOutbound("blob-unknown")
		svc.guardOpenAICodexTurnStateEcho(c, codexTurnStateTestAccount(43), h)
		require.Equal(t, "blob-unknown", h.Get("x-codex-turn-state"))
	})

	t.Run("corrupted_provenance_passthrough", func(t *testing.T) {
		svc, cache := newTurnStateTestService()
		c, _ := newTurnStateTestContext(t, 7, "sess-g4")
		cache.origins[codexTurnStateOriginKey("7\x00sess-g4")] = "{not json"

		h := newOutbound("blob-A")
		svc.guardOpenAICodexTurnStateEcho(c, codexTurnStateTestAccount(43), h)
		require.Equal(t, "blob-A", h.Get("x-codex-turn-state"))
	})

	t.Run("no_session_seed_noop", func(t *testing.T) {
		svc, _ := newTurnStateTestService()
		c, _ := newTurnStateTestContext(t, 7, "")
		h := newOutbound("blob-A")
		svc.guardOpenAICodexTurnStateEcho(c, codexTurnStateTestAccount(43), h)
		require.Equal(t, "blob-A", h.Get("x-codex-turn-state"))
	})

	t.Run("no_echo_noop", func(t *testing.T) {
		svc, _ := newTurnStateTestService()
		c, _ := newTurnStateTestContext(t, 7, "sess-g5")
		h := newOutbound("")
		svc.guardOpenAICodexTurnStateEcho(c, codexTurnStateTestAccount(43), h)
		require.Empty(t, h.Get("x-codex-turn-state"))
	})

	t.Run("store_unavailable_passthrough", func(t *testing.T) {
		// 无 cache（未实现可选接口）时溯源退化为不追踪，与本机制引入前一致。
		svc := &OpenAIGatewayService{}
		c, _ := newTurnStateTestContext(t, 7, "sess-g6")
		h := newOutbound("blob-A")
		require.NotPanics(t, func() {
			svc.guardOpenAICodexTurnStateEcho(c, codexTurnStateTestAccount(43), h)
		})
		require.Equal(t, "blob-A", h.Get("x-codex-turn-state"))
	})
}

// 同账号不足以证明可回放：device→session 模式切换、period 换代、main↔side
// 切换都会在同一账号下换掉上游 session_id，旧 blob 属于另一个上游会话。
func TestGuardOpenAICodexTurnStateEcho_StripsOnIdentityChange(t *testing.T) {
	echoGuard := func(t *testing.T, mintMode codexFingerprintMode, mintSession string, reqMode codexFingerprintMode, reqSession string) string {
		t.Helper()
		svc, _ := newTurnStateTestService()
		c, _ := newTurnStateTestContext(t, 7, "sess-ident")
		account := codexTurnStateTestAccount(42)

		stageCodexTurnStateIdentity(c, account, mintMode, mintSession)
		upstream := http.Header{}
		upstream.Set("x-codex-turn-state", "blob-minted")
		svc.relayOpenAICodexTurnState(c, account, upstream)

		stageCodexTurnStateIdentity(c, account, reqMode, reqSession)
		h := http.Header{}
		h.Set("x-codex-turn-state", "blob-minted")
		svc.guardOpenAICodexTurnStateEcho(c, account, h)
		return h.Get("x-codex-turn-state")
	}

	t.Run("device_to_session_switch_strips", func(t *testing.T) {
		// 切换到 session 模式后上游 session 由 period 分配，与 device 时期不同
		require.Empty(t, echoGuard(t, codexFingerprintDevice, "", codexFingerprintSession, "upstream-session-X"))
	})

	t.Run("period_rollover_strips", func(t *testing.T) {
		require.Empty(t, echoGuard(t, codexFingerprintSession, "upstream-session-1", codexFingerprintSession, "upstream-session-2"))
	})

	t.Run("main_to_side_strips", func(t *testing.T) {
		require.Empty(t, echoGuard(t, codexFingerprintSession, "upstream-main", codexFingerprintSession, "upstream-side"))
	})

	t.Run("same_identity_keeps_echo", func(t *testing.T) {
		require.Equal(t, "blob-minted", echoGuard(t, codexFingerprintSession, "upstream-session-1", codexFingerprintSession, "upstream-session-1"))
	})

	t.Run("unknown_identity_falls_back_to_account", func(t *testing.T) {
		// 记账时拿不到收敛 ID（如非 Codex 协议账号）→ 身份未知，只按账号判定
		svc, _ := newTurnStateTestService()
		c, _ := newTurnStateTestContext(t, 7, "sess-ident-unknown")
		plain := &Account{ID: 42}
		upstream := http.Header{}
		upstream.Set("x-codex-turn-state", "blob-minted")
		svc.relayOpenAICodexTurnState(c, plain, upstream)

		stageCodexTurnStateIdentity(c, codexTurnStateTestAccount(42), codexFingerprintSession, "upstream-session-X")
		h := http.Header{}
		h.Set("x-codex-turn-state", "blob-minted")
		svc.guardOpenAICodexTurnStateEcho(c, codexTurnStateTestAccount(42), h)
		require.Equal(t, "blob-minted", h.Get("x-codex-turn-state"), "身份未知一侧不参与判定")
	})
}

func TestWriteOpenAIPassthroughResponseHeaders_RelaysAndClearsTurnState(t *testing.T) {
	// filter=nil 走 content-type 兜底分支；turn-state 强制放行不依赖 filter。
	dst := http.Header{}
	src := http.Header{}
	src.Set("X-Codex-Turn-State", "blob-P")
	writeOpenAIPassthroughResponseHeaders(dst, src, nil)
	require.Equal(t, "blob-P", dst.Get("X-Codex-Turn-State"))

	// 上游缺失时清除残留（failover 换号防串扰）
	writeOpenAIPassthroughResponseHeaders(dst, http.Header{"Content-Type": []string{"application/json"}}, nil)
	require.Empty(t, dst.Get("X-Codex-Turn-State"))
}

func TestWriteOpenAIPassthroughResponseHeaders_RelaysReasoningIncluded(t *testing.T) {
	dst := http.Header{}
	src := http.Header{}
	src.Set("X-Reasoning-Included", "1")

	writeOpenAIPassthroughResponseHeaders(
		dst,
		src,
		responseheaders.CompileHeaderFilter(config.ResponseHeaderConfig{}),
	)
	require.Equal(t, "1", dst.Get("X-Reasoning-Included"))
}

func TestEnsureOpenAIRemoteCompactionV2BetaFeature(t *testing.T) {
	t.Run("absent_sets_feature", func(t *testing.T) {
		h := http.Header{}
		ensureOpenAIRemoteCompactionV2BetaFeature(h)
		require.Equal(t, "remote_compaction_v2", h.Get("x-codex-beta-features"))
	})

	t.Run("present_unchanged", func(t *testing.T) {
		h := http.Header{}
		h.Set("x-codex-beta-features", "responses_websockets_v2, remote_compaction_v2")
		ensureOpenAIRemoteCompactionV2BetaFeature(h)
		require.Equal(t, "responses_websockets_v2, remote_compaction_v2", h.Get("x-codex-beta-features"))
	})

	t.Run("other_tokens_merged", func(t *testing.T) {
		h := http.Header{}
		h.Set("x-codex-beta-features", "responses_websockets_v2")
		ensureOpenAIRemoteCompactionV2BetaFeature(h)
		require.Equal(t, "responses_websockets_v2,remote_compaction_v2", h.Get("x-codex-beta-features"))
	})

	t.Run("multi_line_values_merged_single_line", func(t *testing.T) {
		h := http.Header{}
		h.Add("x-codex-beta-features", "feature_a")
		h.Add("x-codex-beta-features", "feature_b")
		ensureOpenAIRemoteCompactionV2BetaFeature(h)
		require.Equal(t, []string{"feature_a,feature_b,remote_compaction_v2"}, h.Values("x-codex-beta-features"))
	})
}

// 对齐真实 Codex：该头是会话级常量，挂在 OAuth 的每个请求上，而不是只在
// 压缩回合出现（codex-rs build_model_client_beta_features_header）。
func TestApplyOpenAICodexBetaFeatures(t *testing.T) {
	oauthAccount := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	apiKeyAccount := &Account{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}

	t.Run("oauth_plain_request_gets_default_codex_shape", func(t *testing.T) {
		c, _ := newTurnStateTestContext(t, 7, "sess-beta")
		h := http.Header{}
		applyOpenAICodexBetaFeatures(c, oauthAccount, h)
		require.Equal(t, "remote_compaction_v2", h.Get("x-codex-beta-features"),
			"OAuth 的普通请求也必须带会话级 beta 头")
	})

	t.Run("client_declared_header_preserved", func(t *testing.T) {
		c, _ := newTurnStateTestContext(t, 7, "sess-beta")
		h := http.Header{}
		h.Set("x-codex-beta-features", "some_other_feature")
		applyOpenAICodexBetaFeatures(c, oauthAccount, h)
		require.Equal(t, "some_other_feature", h.Get("x-codex-beta-features"),
			"客户端显式声明的能力集不得被网关改写（非空即视为用户已关闭 v2）")
	})

	t.Run("native_v2_forces_feature_even_when_client_trimmed_it", func(t *testing.T) {
		c, _ := newTurnStateTestContext(t, 7, "sess-beta")
		MarkOpenAINativeCompactionV2(c)
		h := http.Header{}
		h.Set("x-codex-beta-features", "some_other_feature")
		applyOpenAICodexBetaFeatures(c, oauthAccount, h)
		require.Contains(t, h.Get("x-codex-beta-features"), "remote_compaction_v2",
			"body 带 compaction_trigger 是实锤，必须确保 v2 在列")
		require.Contains(t, h.Get("x-codex-beta-features"), "some_other_feature")
	})

	t.Run("native_v2_applies_to_non_oauth_too", func(t *testing.T) {
		c, _ := newTurnStateTestContext(t, 7, "sess-beta")
		MarkOpenAINativeCompactionV2(c)
		h := http.Header{}
		applyOpenAICodexBetaFeatures(c, apiKeyAccount, h)
		require.Equal(t, "remote_compaction_v2", h.Get("x-codex-beta-features"))
	})

	t.Run("non_oauth_plain_request_untouched", func(t *testing.T) {
		c, _ := newTurnStateTestContext(t, 7, "sess-beta")
		h := http.Header{}
		applyOpenAICodexBetaFeatures(c, apiKeyAccount, h)
		require.Empty(t, h.Get("x-codex-beta-features"),
			"非 Codex 后端不做会话级注入")
	})

	t.Run("nil_account_plain_request_untouched", func(t *testing.T) {
		c, _ := newTurnStateTestContext(t, 7, "sess-beta")
		h := http.Header{}
		applyOpenAICodexBetaFeatures(c, nil, h)
		require.Empty(t, h.Get("x-codex-beta-features"))
	})
}

// WS 握手与 HTTP 出站必须给出同一份会话级 beta 头：真实 Codex 的
// build_websocket_headers 复用 build_responses_headers（client.rs），
// 两侧不一致还会让预热连接与实际请求落进不同的连接池兼容分桶。
func TestBuildOpenAIWSHeaders_CarriesSessionBetaFeatures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &OpenAIGatewayService{}
	decision := OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2}

	build := func(t *testing.T, account *Account, clientBeta string) http.Header {
		t.Helper()
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
		if clientBeta != "" {
			c.Request.Header.Set("x-codex-beta-features", clientBeta)
		}
		headers, _, err := svc.buildOpenAIWSHeaders(
			context.Background(), c, account, "test-token", decision,
			true, "", "", "", "gpt-5.6-codex", "",
		)
		require.NoError(t, err)
		return headers
	}

	oauthAccount := &Account{
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Credentials: map[string]any{"chatgpt_account_id": "test-account"},
	}

	headers := build(t, oauthAccount, "")
	require.Equal(t, "remote_compaction_v2", headers.Get("x-codex-beta-features"),
		"WS 握手也必须带会话级 beta 头")

	declared := build(t, oauthAccount, "some_other_feature")
	require.Equal(t, []string{"some_other_feature"}, declared.Values("x-codex-beta-features"),
		"客户端已声明时原样保留")

	apiKeyHeaders := build(t, &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, "")
	require.Empty(t, apiKeyHeaders.Get("x-codex-beta-features"),
		"非 Codex 后端不注入")
}