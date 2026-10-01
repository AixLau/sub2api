package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 接入层测试：验证门控、归属判定与故障语义。
// 只覆盖"能寻址到绑定"的新模型路径；门控关闭时的既有行为单独验证其惰性。

func newCodexBindingService(t *testing.T, server *miniredis.Miniredis) *OpenAIGatewayService {
	t.Helper()
	svc := newCodexPeriodRedisService(t, server)
	svc.cfg.Gateway.CodexIdentity.SessionBinding = config.CodexSessionBindingBinding
	return svc
}

// codexBindingFailingStore 让所有 identity 读写都失败，用于验证"存储故障不
// 偷偷改用账号当前模式"。
type codexBindingFailingStore struct{ GatewayCache }

func (codexBindingFailingStore) GetCodexSessionIdentity(context.Context, string) (string, error) {
	return "", errors.New("identity store unavailable")
}

func (codexBindingFailingStore) SetCodexSessionIdentityIfAbsent(context.Context, string, string, time.Duration) (bool, error) {
	return false, errors.New("identity store unavailable")
}

func (codexBindingFailingStore) CompareAndSwapCodexSessionIdentity(context.Context, string, string, string, time.Duration) (bool, error) {
	return false, errors.New("identity store unavailable")
}

func codexBindingRequest(t *testing.T, userID, apiKeyID int64, rawSession, rawThread, parent string) *gin.Context {
	t.Helper()
	c := newCodexSessionIdentityV2Context(t, userID, apiKeyID)
	c.Request.Header.Set("session-id", rawSession)
	c.Request.Header.Set("thread-id", rawThread)
	if parent != "" {
		c.Request.Header.Set("x-codex-parent-thread-id", parent)
	}
	stageCodexSessionIdentityInputMap(c, nil)
	return c
}

func codexBindingKeys(server *miniredis.Miniredis) []string {
	var matched []string
	for _, key := range server.Keys() {
		if strings.Contains(key, "session-binding") || strings.Contains(key, "thread-exact") ||
			strings.Contains(key, "thread-latest") || strings.Contains(key, "flat-entity") ||
			strings.Contains(key, "period-session") {
			matched = append(matched, key)
		}
	}
	return matched
}

// 门控默认关闭时，新模型必须完全惰性：一个绑定键都不写。
func TestCodexBindingGateOffWritesNoBindingRecords(t *testing.T) {
	server := miniredis.RunT(t)
	svc := newCodexPeriodRedisService(t, server) // cfg 未设置 => legacy
	require.False(t, svc.codexSessionBindingEnabled(), "默认必须是 legacy")

	account := newTestOAuthAccount(9001, map[string]any{codexFingerprintModeExtraKey: "session"})
	c := codexBindingRequest(t, 901, 9011, newCodexUUIDv7ForTest(t), newCodexUUIDv7ForTest(t), "")

	ids, err := svc.resolveCodexHTTPFingerprintIDs(context.Background(), c, account, time.Now())
	require.NoError(t, err)
	require.NotNil(t, ids, "legacy 路径行为不变")
	require.Empty(t, codexBindingKeys(server), "门控关闭时不得写入任何绑定记录")
}

// 同一 epoch 下不同 raw session 必须共享同一个上游会话 id。
func TestCodexBindingSharesPeriodSessionAcrossRawSessions(t *testing.T) {
	server := miniredis.RunT(t)
	svc := newCodexBindingService(t, server)
	account := newTestOAuthAccount(9002, map[string]any{codexFingerprintModeExtraKey: "session"})
	now := time.Now()

	resolve := func() *codexFingerprintIDs {
		t.Helper()
		c := codexBindingRequest(t, 902, 9021, newCodexUUIDv7ForTest(t), newCodexUUIDv7ForTest(t), "")
		ids, err := svc.resolveCodexHTTPFingerprintIDs(context.Background(), c, account, now)
		require.NoError(t, err)
		require.NotNil(t, ids)
		return ids
	}
	first, second := resolve(), resolve()

	require.True(t, first.httpSessionIdentity)
	require.Equal(t, first.sessionID, second.sessionID,
		"同一 epoch 下不同 raw session 必须共享同一个上游会话 id")
	require.NotEqual(t, first.threadID, second.threadID, "thread 仍按原始任务独立")
	require.Equal(t, first.installationID, second.installationID)
}

// device 账号没有自然周期：必须建成 flat:0，绝不能建成会被代次换代推翻的 period。
func TestCodexBindingDeviceAccountUsesFlatNotPeriod(t *testing.T) {
	server := miniredis.RunT(t)
	svc := newCodexBindingService(t, server)
	account := newTestOAuthAccount(9003, map[string]any{codexFingerprintModeExtraKey: "device"})
	now := time.Now()

	c := codexBindingRequest(t, 903, 9031, newCodexUUIDv7ForTest(t), newCodexUUIDv7ForTest(t), "")
	ids, err := svc.resolveCodexHTTPFingerprintIDs(context.Background(), c, account, now)
	require.NoError(t, err)
	require.NotNil(t, ids)
	require.False(t, ids.httpSessionIdentity, "device 规则不走走会话投影")

	var flatBinding, periodBinding bool
	for _, key := range codexBindingKeys(server) {
		if strings.Contains(key, "session-binding:flat:0:") {
			flatBinding = true
		}
		if strings.Contains(key, "session-binding:period:") {
			periodBinding = true
		}
	}
	require.True(t, flatBinding, "device 必须建成 flat:0 绑定")
	require.False(t, periodBinding, "device 不得建成 period 绑定")
}

// 切模式后旧会话沿用已提交规则：不得因账号改成 session 就改用会话投影。
func TestCodexBindingKeepsCommittedRuleAfterAccountFlip(t *testing.T) {
	server := miniredis.RunT(t)
	svc := newCodexBindingService(t, server)
	account := newTestOAuthAccount(9004, map[string]any{codexFingerprintModeExtraKey: "device"})
	now := time.Now()
	rawSession := newCodexUUIDv7ForTest(t)

	first, err := svc.resolveCodexHTTPFingerprintIDs(context.Background(),
		codexBindingRequest(t, 904, 9041, rawSession, newCodexUUIDv7ForTest(t), ""), account, now)
	require.NoError(t, err)
	require.False(t, first.httpSessionIdentity)

	// 管理员把账号改成 session —— 已提交绑定不受影响。
	account.Extra[codexFingerprintModeExtraKey] = "session"

	second, err := svc.resolveCodexHTTPFingerprintIDs(context.Background(),
		codexBindingRequest(t, 904, 9041, rawSession, newCodexUUIDv7ForTest(t), ""), account, now)
	require.NoError(t, err)
	require.NotNil(t, second)
	require.False(t, second.httpSessionIdentity,
		"旧会话必须沿用已提交规则，不因账号改默认值而中途换规则")

	// 新会话（新的 raw session）才采用新模式。
	third, err := svc.resolveCodexHTTPFingerprintIDs(context.Background(),
		codexBindingRequest(t, 904, 9041, newCodexUUIDv7ForTest(t), newCodexUUIDv7ForTest(t), ""), account, now)
	require.NoError(t, err)
	require.NotNil(t, third)
	require.True(t, third.httpSessionIdentity, "新会话应采用新的账号模式")
}

// child 先到且归属证据不足：明确失败、不写任何键，绝不默认当普通会话。
func TestCodexBindingInsufficientAttributionFailsExplicitly(t *testing.T) {
	server := miniredis.RunT(t)
	svc := newCodexBindingService(t, server)
	account := newTestOAuthAccount(9005, map[string]any{codexFingerprintModeExtraKey: "session"})

	c := codexBindingRequest(t, 905, 9051,
		newCodexUUIDv7ForTest(t), newCodexUUIDv7ForTest(t), newCodexUUIDv7ForTest(t))
	ids, err := svc.resolveCodexHTTPFingerprintIDs(context.Background(), c, account, time.Now())

	require.ErrorIs(t, err, ErrCodexBindingUnresolvedAttribution)
	require.Nil(t, ids)
	require.Empty(t, codexBindingKeys(server), "证据不足时不得写入任何绑定记录")
}

// 存储故障必须明确失败，绝不偷偷改用账号当前模式。
func TestCodexBindingStoreFailureDoesNotFallBackToAccountMode(t *testing.T) {
	svc := &OpenAIGatewayService{
		cache: codexBindingFailingStore{},
		cfg: &config.Config{Gateway: config.GatewayConfig{
			CodexIdentity: config.CodexIdentityConfig{SessionBinding: config.CodexSessionBindingBinding},
		}},
	}
	require.True(t, svc.codexSessionBindingEnabled())
	account := newTestOAuthAccount(9006, map[string]any{codexFingerprintModeExtraKey: "session"})
	c := codexBindingRequest(t, 906, 9061, newCodexUUIDv7ForTest(t), newCodexUUIDv7ForTest(t), "")

	ids, err := svc.resolveCodexHTTPFingerprintIDs(context.Background(), c, account, time.Now())
	require.Error(t, err, "存储故障必须明确失败")
	require.Nil(t, ids, "不得返回账号当前模式的投影")
}

// 强制开关：忽略已提交绑定，按账号当前模式处理（即 legacy 行为）。
func TestCodexBindingForceAccountRuleIgnoresCommittedBinding(t *testing.T) {
	server := miniredis.RunT(t)
	svc := newCodexBindingService(t, server)
	account := newTestOAuthAccount(9007, map[string]any{codexFingerprintModeExtraKey: "device"})
	now := time.Now()
	rawSession := newCodexUUIDv7ForTest(t)

	_, err := svc.resolveCodexHTTPFingerprintIDs(context.Background(),
		codexBindingRequest(t, 907, 9071, rawSession, newCodexUUIDv7ForTest(t), ""), account, now)
	require.NoError(t, err)

	account.Extra[codexFingerprintModeExtraKey] = "session"
	svc.cfg.Gateway.CodexIdentity.ForceAccountRule = true

	forced, err := svc.resolveCodexHTTPFingerprintIDs(context.Background(),
		codexBindingRequest(t, 907, 9071, rawSession, newCodexUUIDv7ForTest(t), ""), account, now)
	require.NoError(t, err)
	require.NotNil(t, forced)
	require.True(t, forced.httpSessionIdentity, "强制开关必须忽略已提交绑定，跟随账号当前模式")
}

// flat 之间的模式切换（device→full）必须对**新会话**生效。
//
// 回归锁：flat 实体曾经只按 scope 定键、规则由首个创建者永久固定，于是
// device→full/off 这类切换对新会话也永不生效，直接违背"新会话使用新规则"。
// 现在键里含规则：新会话指向新实体取新规则，旧绑定仍指向旧实体、继续用旧规则。
func TestCodexBindingFlatModeChangeAppliesToNewSessionsOnly(t *testing.T) {
	server := miniredis.RunT(t)
	svc := newCodexBindingService(t, server)
	account := newTestOAuthAccount(9008, map[string]any{codexFingerprintModeExtraKey: "device"})
	now := time.Now()
	oldRawSession := newCodexUUIDv7ForTest(t)

	before, err := svc.resolveCodexHTTPFingerprintIDs(context.Background(),
		codexBindingRequest(t, 908, 9081, oldRawSession, newCodexUUIDv7ForTest(t), ""), account, now)
	require.NoError(t, err)
	require.Equal(t, codexFingerprintDevice, before.mode)

	// flat 内部的模式切换
	account.Extra[codexFingerprintModeExtraKey] = "full"

	after, err := svc.resolveCodexHTTPFingerprintIDs(context.Background(),
		codexBindingRequest(t, 908, 9081, oldRawSession, newCodexUUIDv7ForTest(t), ""), account, now)
	require.NoError(t, err)
	require.Equal(t, codexFingerprintDevice, after.mode,
		"旧会话必须沿用已提交规则，不因 flat 内切换而改变")

	fresh, err := svc.resolveCodexHTTPFingerprintIDs(context.Background(),
		codexBindingRequest(t, 908, 9081, newCodexUUIDv7ForTest(t), newCodexUUIDv7ForTest(t), ""), account, now)
	require.NoError(t, err)
	require.NotNil(t, fresh)
	require.Equal(t, codexFingerprintFull, fresh.mode,
		"新会话必须采用新的 flat 规则，否则 flat 内切换对新会话永不生效")
}
