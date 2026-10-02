package service

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
)

// 事故回归锁定：2026-10-01 线上把两个账号（335/336）从 device 翻成 session，
// 在途 Codex 会话（带 parent/fork 引用）随即以 58 次
// ErrCodexSessionIdentityNotFound -> HTTP 500 openai.forward_failed 失败。
//
// 成因：session 从 v3 持久命名空间解析身份，与 device 的 v2 命名空间不相交，
// 翻转即从空存储开始；而旧解析器对引用父线程的请求拒绝凭空造映射。
//
// 本文件通过真实门控入口 resolveCodexHTTPFingerprintIDs 复现事故拓扑，断言：
//  1. 翻转前已提交绑定的在途 child 必须沿用旧规则存活（事故是否真被修好的判定）；
//  2. 翻转与门控同时发生（无任何已提交绑定）时必须明确失败，绝不静默归错 lineage；
//  3. 翻转前后完整请求序列都不得再返回旧错误。
//
// 刻意不弱化断言：若第 1 条不通过，说明事故并未修复，而不是测试需要放宽。

// codexBindingRawValues 收集所有匹配子串的绑定键及其原始字节，
// 用于断言"已提交绑定不被账号翻转改写"。
func codexBindingRawValues(server *miniredis.Miniredis, substr string) map[string]string {
	values := map[string]string{}
	for _, key := range server.Keys() {
		if !strings.Contains(key, substr) {
			continue
		}
		value, err := server.Get(key)
		if err == nil {
			values[key] = value
		}
	}
	return values
}

// codexBindingKeySnapshot 是绑定记录键的排序快照，
// 用于断言"某次请求没有写入任何绑定键"。
func codexBindingKeySnapshot(server *miniredis.Miniredis) []string {
	keys := codexBindingKeys(server)
	sort.Strings(keys)
	return keys
}

// 事故拓扑的回归锁：翻转前已提交 flat 绑定的在途 child 必须存活，且沿用旧规则。
func TestCodexBindingIncidentInFlightChildSurvivesAccountFlip(t *testing.T) {
	server := miniredis.RunT(t)
	svc := newCodexBindingService(t, server)
	require.True(t, svc.codexSessionBindingEnabled(), "本用例必须在绑定门控开启下运行")

	const userID, apiKeyID = int64(3351), int64(33511)
	account := newTestOAuthAccount(335, map[string]any{codexFingerprintModeExtraKey: "device"})
	now := time.Now()
	rawSession := newCodexUUIDv7ForTest(t)
	parentThread := newCodexUUIDv7ForTest(t)

	// Step 1：账号仍为 device，root 请求先跑，必须提交 flat:0 绑定并落在旧规则上。
	parent, err := svc.resolveCodexHTTPFingerprintIDs(context.Background(),
		codexBindingRequest(t, userID, apiKeyID, rawSession, parentThread, ""), account, now)
	require.NoError(t, err)
	require.NotNil(t, parent)
	require.False(t, parent.httpSessionIdentity, "device 账号的 root 必须落在 flat 规则上，而不是会话投影")
	require.Equal(t, codexFingerprintDevice, parent.mode)

	flatBindings := codexBindingRawValues(server, "session-binding:flat:0")
	require.Len(t, flatBindings, 1, "root 必须先提交唯一一条 flat:0 绑定")

	// Step 2：事故动作 —— 运维把账号从 device 改成 session。
	account.Extra[codexFingerprintModeExtraKey] = "session"

	keysBefore := codexBindingKeySnapshot(server)

	// Step 3：同一 raw session 上的在途 child，携带父线程引用与新的 thread。
	child, err := svc.resolveCodexHTTPFingerprintIDs(context.Background(),
		codexBindingRequest(t, userID, apiKeyID, rawSession, newCodexUUIDv7ForTest(t), parentThread), account, now)

	require.NoError(t, err, "账号翻转绝不能让在途 child 失败")
	require.NotErrorIs(t, err, ErrCodexSessionIdentityNotFound)
	require.NotNil(t, child)
	require.False(t, child.httpSessionIdentity,
		"child 必须沿用已提交的 device 规则：账号翻转不得重写已提交绑定")
	require.Equal(t, codexFingerprintDevice, child.mode)
	require.Equal(t, parent.installationID, child.installationID, "账号级 installation 必须保持不变")
	// device 投影本身不携带 session/thread（这正是"旧规则"的形态），
	// 这里断言 child 与 root 的投影完全一致，而不是被翻转成会话投影。
	require.Equal(t, parent.sessionID, child.sessionID)
	require.Equal(t, parent.threadID, child.threadID)

	require.Equal(t, flatBindings, codexBindingRawValues(server, "session-binding:flat:0"),
		"已提交的 flat 绑定字节不得被账号翻转改写")
	require.Equal(t, keysBefore, codexBindingKeySnapshot(server),
		"走已提交绑定的请求不得写入任何新的绑定记录")
}

// 不安全序列：账号翻转与门控开启同时发生，翻转前没有任何已提交绑定。
func TestCodexBindingSameTimeSwitchStillFailsExplicitly(t *testing.T) {
	server := miniredis.RunT(t)
	svc := newCodexPeriodRedisService(t, server) // 门控关闭：parent 在旧模型下运行
	require.False(t, svc.codexSessionBindingEnabled())

	const userID, apiKeyID = int64(3361), int64(33611)
	account := newTestOAuthAccount(336, map[string]any{codexFingerprintModeExtraKey: "device"})
	now := time.Now()
	rawSession := newCodexUUIDv7ForTest(t)
	parentThread := newCodexUUIDv7ForTest(t)

	// parent 在绑定模型之外运行：不提交任何证据。
	parent, err := svc.resolveCodexHTTPFingerprintIDs(context.Background(),
		codexBindingRequest(t, userID, apiKeyID, rawSession, parentThread, ""), account, now)
	require.NoError(t, err)
	require.NotNil(t, parent)
	require.Empty(t, codexBindingKeys(server), "门控关闭时不得写入任何绑定记录")

	// 同时切换：账号改成 session，并且门控开启——翻转前没有任何已提交绑定。
	account.Extra[codexFingerprintModeExtraKey] = "session"
	svc.cfg.Gateway.CodexIdentity.SessionBinding = config.CodexSessionBindingBinding
	require.True(t, svc.codexSessionBindingEnabled())

	// 带 parent 引用的 child 到达：父线程在绑定模型内没有任何 exact 证据。
	child, err := svc.resolveCodexHTTPFingerprintIDs(context.Background(),
		codexBindingRequest(t, userID, apiKeyID, rawSession, newCodexUUIDv7ForTest(t), parentThread), account, now)

	require.NotErrorIs(t, err, ErrCodexSessionIdentityNotFound, "新模型绝不能再返回事故错误")
	if err != nil {
		// 实际观察到的行为：err 是 ErrCodexBindingUnresolvedAttribution，ids 为 nil，
		// 且不写任何绑定键。父线程没有 latest/exact 证据，resolveCodexThreadForkSource
		// 返回 ErrCodexBindingUnresolvedSource，被上抛为"归属证据不足"的明确失败。
		// 这正是设计意图：宁可明确失败，也不把 child 静默归成另一条 lineage 的普通会话。
		require.ErrorIs(t, err, ErrCodexBindingUnresolvedAttribution)
		require.Nil(t, child, "归属不足时不得返回任何身份投影")
		require.Empty(t, codexBindingKeys(server), "归属不足时不得为该 raw session 写入绑定")
		return
	}
	// 允许的另一种结果：与已提交绑定一致地成功——即该 raw session 上确实已有绑定，
	// 而不是被默认当成普通会话。当前实现不会走到这里，此分支用于锁定语义而非放宽。
	require.NotNil(t, child)
	require.NotEmpty(t, codexBindingRawValues(server, "session-binding"),
		"成功必须来自已提交绑定，而不是默认当普通会话")
}

// 翻转前后完整请求序列：任何一步都不得返回旧错误。
func TestCodexBindingIncidentNoForwardFailureOnFlip(t *testing.T) {
	server := miniredis.RunT(t)
	svc := newCodexBindingService(t, server)

	const userID, apiKeyID = int64(3371), int64(33711)
	account := newTestOAuthAccount(337, map[string]any{codexFingerprintModeExtraKey: "device"})
	now := time.Now()

	step := func(name, rawSession, rawThread, parent string) *codexFingerprintIDs {
		t.Helper()
		ids, err := svc.resolveCodexHTTPFingerprintIDs(context.Background(),
			codexBindingRequest(t, userID, apiKeyID, rawSession, rawThread, parent), account, now)
		require.NotErrorIs(t, err, ErrCodexSessionIdentityNotFound, "%s 不得复现事故错误", name)
		require.NoError(t, err, "%s 必须成功", name)
		require.NotNil(t, ids, "%s 必须产出身份投影", name)
		return ids
	}

	// 1) device 时代 root：提交 flat 绑定。
	deviceSession := newCodexUUIDv7ForTest(t)
	deviceThread := newCodexUUIDv7ForTest(t)
	deviceRoot := step("device-era root", deviceSession, deviceThread, "")
	require.False(t, deviceRoot.httpSessionIdentity)

	// 事故动作：账号 device -> session。
	account.Extra[codexFingerprintModeExtraKey] = "session"

	// 2) 在途 child（同一 raw session，引用父线程）：必须沿用已提交规则。
	inFlight := step("in-flight child after flip", deviceSession, newCodexUUIDv7ForTest(t), deviceThread)
	require.False(t, inFlight.httpSessionIdentity, "在途 child 必须沿用已提交规则")
	require.Equal(t, deviceRoot.installationID, inFlight.installationID)

	// 3) 翻转后的新会话 root：按 session 规则提交 period 绑定。
	sessionA := newCodexUUIDv7ForTest(t)
	sessionAThread := newCodexUUIDv7ForTest(t)
	sessionRoot := step("session-era root", sessionA, sessionAThread, "")
	require.True(t, sessionRoot.httpSessionIdentity, "翻转后的新会话应采用 session 规则")

	// 4) 同一 raw session 的后续 child：恢复已提交的 period 绑定，落在同一上游会话。
	childSameSession := step("child of session root", sessionA, newCodexUUIDv7ForTest(t), sessionAThread)
	require.True(t, childSameSession.httpSessionIdentity)
	require.Equal(t, sessionRoot.sessionID, childSameSession.sessionID)

	// 5) spawn 出的新 raw session 走父线程继承：必须落在父会话同一个上游 session。
	spawnChild := step("spawned child inherits parent", newCodexUUIDv7ForTest(t), newCodexUUIDv7ForTest(t), sessionAThread)
	require.True(t, spawnChild.httpSessionIdentity)
	require.Equal(t, sessionRoot.sessionID, spawnChild.sessionID, "父线程继承必须落在同一个上游会话")

	// 6) 再翻回 device：已提交的 period 规则不受影响。
	account.Extra[codexFingerprintModeExtraKey] = "device"
	lateChild := step("late child after flip back", sessionA, newCodexUUIDv7ForTest(t), sessionAThread)
	require.True(t, lateChild.httpSessionIdentity, "已提交绑定不得被翻回 device 改写")
	require.Equal(t, sessionRoot.sessionID, lateChild.sessionID)

	// 7) 翻回后的全新会话：按当前 device 规则建立 flat 绑定，仍然不得报旧错误。
	lateDevice := step("device root after flip back", newCodexUUIDv7ForTest(t), newCodexUUIDv7ForTest(t), "")
	require.False(t, lateDevice.httpSessionIdentity)
}
