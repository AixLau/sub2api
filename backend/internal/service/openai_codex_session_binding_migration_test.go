package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
)

// 分阶段迁移（staged migration）端到端验证。
//
// 运行手册给出的顺序是：
//  1. gate 开到 binding，账号保持**现有**模式，让绑定先累积；
//  2. 观察键覆盖；
//  3. 之后才改账号模式——旧会话沿用已提交规则，新会话采用新模式。
//
// 本文件验证这条顺序在门控入口上是否真的成立：旧会话规则不漂移、新会话才换规则、
// 换代不改写旧绑定、回滚后旧读路径看到同一批身份，以及 force_account_rule 逃生门
// 不读写绑定。任何一条不成立都是运行手册必须写明的风险。

// codexBindingRedisKeyPrefix 是 Redis 身份存储的键前缀（见 codexSessionIdentityRedisStore）。
const codexBindingRedisKeyPrefix = "openai_codex_session_identity:"

// codexBindingRedisValue 读取绑定记录在 Redis 中的原始字节。
func codexBindingRedisValue(t *testing.T, server *miniredis.Miniredis, key string) string {
	t.Helper()
	value, err := server.Get(codexBindingRedisKeyPrefix + key)
	require.NoError(t, err, "绑定键必须已持久化：%s", key)
	return value
}

// codexBindingFamilyKeys 在通用过滤之外补上 side 实体与寻址指针，
// 用于“一个绑定键都不许写”的否定断言。
func codexBindingFamilyKeys(server *miniredis.Miniredis) []string {
	keys := codexBindingKeys(server)
	for _, key := range server.Keys() {
		if strings.Contains(key, "side-session:v2") || strings.Contains(key, "session-current") {
			keys = append(keys, key)
		}
	}
	return keys
}

// TestCodexBindingStagedMigrationKeepsOldSessionsAndAdoptsNew 走完整的分阶段流程：
// 账号先是 device（gate 已开，绑定累积）→ 管理员切成 session → 在途 raw session 沿用
// 已提交的 flat 规则且旧记录逐字节不变，只有新的 raw session 才采用 period 规则。
func TestCodexBindingStagedMigrationKeepsOldSessionsAndAdoptsNew(t *testing.T) {
	server := miniredis.RunT(t)
	svc := newCodexBindingService(t, server)
	require.True(t, svc.codexSessionBindingEnabled(), "前置条件：门控已开")

	account := newTestOAuthAccount(9101, map[string]any{codexFingerprintModeExtraKey: string(codexFingerprintDevice)})
	require.Equal(t, codexFingerprintDevice, account.GetCodexFingerprintMode())
	now := time.Now()
	seed, ok := codexFingerprintSeed(account.Extra)
	require.True(t, ok, "device 账号必须有种子，否则绑定模型不参与")

	const userID, apiKeyID = int64(9101), int64(91011)
	userScope := fmt.Sprintf("user:%d", userID)
	accountScope := codexSessionIdentityUpstreamScope(account)
	scope := codexBindingScope(userScope, accountScope)

	rawSessionS1 := newCodexUUIDv7ForTest(t)
	resolve := func(rawSession string) *codexFingerprintIDs {
		t.Helper()
		ids, err := svc.resolveCodexHTTPFingerprintIDs(context.Background(),
			codexBindingRequest(t, userID, apiKeyID, rawSession, newCodexUUIDv7ForTest(t), ""), account, now)
		require.NoError(t, err)
		return ids
	}

	// 阶段 1：账号仍是 device，gate 已开 —— 根请求钉成 flat:0。
	first := resolve(rawSessionS1)
	require.NotNil(t, first)
	require.False(t, first.httpSessionIdentity, "device 规则不走会话投影")
	require.Equal(t, codexFingerprintDevice, first.mode)

	flatEntityKey := codexFlatEntityKey(scope, codexFingerprintDevice)
	flatBindingKey := codexSessionBindingKey(codexAttributionFlat, codexConstantGeneration, scope, rawSessionS1)
	flatBindingBefore := codexBindingRedisValue(t, server, flatBindingKey)
	flatEntityBefore := codexBindingRedisValue(t, server, flatEntityKey)
	require.Contains(t, codexBindingKeys(server), codexBindingRedisKeyPrefix+flatBindingKey,
		"flat 绑定必须真实落在 Redis 键集里")
	flatRecord, err := decodeCodexSessionBinding(flatBindingBefore)
	require.NoError(t, err)
	require.Equal(t, codexAttributionFlat, flatRecord.Attribution)
	require.Equal(t, codexConstantGeneration, flatRecord.Generation)
	require.Equal(t, flatEntityKey, flatRecord.EntityRef)

	// 阶段 3：管理员把账号切成 session —— 在途的 S1 必须继续沿用已提交的 flat 规则。
	account.Extra[codexFingerprintModeExtraKey] = string(codexFingerprintSession)

	second := resolve(rawSessionS1)
	require.NotNil(t, second, "旧会话在新账号模式下必须仍能解析出身份")
	require.False(t, second.httpSessionIdentity, "旧会话必须沿用已提交的 flat 规则")
	require.Equal(t, codexFingerprintDevice, second.mode)

	require.Equal(t, flatBindingBefore, codexBindingRedisValue(t, server, flatBindingKey),
		"切模式不得改写旧绑定记录（必须逐字节相同）")
	require.Equal(t, flatEntityBefore, codexBindingRedisValue(t, server, flatEntityKey),
		"切模式不得改写 flat 规则实体")

	epoch := resolveCodexSessionPeriod(seed, userScope, accountScope, now).epoch
	require.NotContains(t, codexBindingKeys(server),
		codexBindingRedisKeyPrefix+codexSessionBindingKey(codexAttributionPeriod, epoch, scope, rawSessionS1),
		"S1 上不得出现 period 绑定")

	// 新的 raw session 才采用账号当前模式。
	rawSessionS2 := newCodexUUIDv7ForTest(t)
	third := resolve(rawSessionS2)
	require.NotNil(t, third)
	require.True(t, third.httpSessionIdentity, "新会话必须采用账号当前模式（session）")
	require.Equal(t, codexFingerprintSession, third.mode)
	require.True(t, isCodexUUIDv7(third.sessionID))

	periodEntityKey := codexPeriodSessionEntityKey(scope, epoch)
	require.Contains(t, codexBindingKeys(server), codexBindingRedisKeyPrefix+periodEntityKey,
		"S2 必须建立共享 period 实体")
	periodBindingKey := codexSessionBindingKey(codexAttributionPeriod, epoch, scope, rawSessionS2)
	periodRecord, err := decodeCodexSessionBinding(codexBindingRedisValue(t, server, periodBindingKey))
	require.NoError(t, err)
	require.Equal(t, codexAttributionPeriod, periodRecord.Attribution)
	require.Equal(t, epoch, periodRecord.Generation)
	require.Equal(t, periodEntityKey, periodRecord.EntityRef)

	require.NotEqual(t, first.sessionID, third.sessionID, "S1 与 S2 不得共享会话身份")
	require.Empty(t, first.sessionID, "flat 不收敛 per-session 会话 id")
}

// TestCodexBindingStagedMigrationSharedPeriodAcrossRawSessions 同一 epoch 下两个不同的
// raw session 必须收敛到同一个上游会话 id（共享 period 实体），thread 仍按任务独立。
func TestCodexBindingStagedMigrationSharedPeriodAcrossRawSessions(t *testing.T) {
	server := miniredis.RunT(t)
	svc := newCodexBindingService(t, server)
	require.True(t, svc.codexSessionBindingEnabled())

	account := newTestOAuthAccount(9102, map[string]any{codexFingerprintModeExtraKey: string(codexFingerprintSession)})
	now := time.Now()
	seed, ok := codexFingerprintSeed(account.Extra)
	require.True(t, ok)

	const userID, apiKeyID = int64(9102), int64(91021)
	userScope := fmt.Sprintf("user:%d", userID)
	accountScope := codexSessionIdentityUpstreamScope(account)
	scope := codexBindingScope(userScope, accountScope)
	epoch := resolveCodexSessionPeriod(seed, userScope, accountScope, now).epoch

	resolve := func(rawSession, rawThread string) *codexFingerprintIDs {
		t.Helper()
		ids, err := svc.resolveCodexHTTPFingerprintIDs(context.Background(),
			codexBindingRequest(t, userID, apiKeyID, rawSession, rawThread, ""), account, now)
		require.NoError(t, err)
		require.NotNil(t, ids)
		return ids
	}

	rawSessionA, rawSessionB := newCodexUUIDv7ForTest(t), newCodexUUIDv7ForTest(t)
	require.NotEqual(t, rawSessionA, rawSessionB)
	first := resolve(rawSessionA, newCodexUUIDv7ForTest(t))
	second := resolve(rawSessionB, newCodexUUIDv7ForTest(t))

	require.True(t, first.httpSessionIdentity)
	require.True(t, second.httpSessionIdentity)
	require.True(t, isCodexUUIDv7(first.sessionID))
	require.Equal(t, first.sessionID, second.sessionID,
		"同一 epoch 下不同 raw session 必须共享同一个上游会话 id")
	require.NotEqual(t, first.threadID, second.threadID, "thread 仍按原始任务独立")
	require.Equal(t, first.installationID, second.installationID)

	// 共享的是实体而不是绑定：两个 raw session 各有一条不可变 period 绑定，都指向同一实体。
	entityKey := codexPeriodSessionEntityKey(scope, epoch)
	for _, rawSession := range []string{rawSessionA, rawSessionB} {
		binding, err := decodeCodexSessionBinding(codexBindingRedisValue(t, server,
			codexSessionBindingKey(codexAttributionPeriod, epoch, scope, rawSession)))
		require.NoError(t, err)
		require.Equal(t, codexAttributionPeriod, binding.Attribution)
		require.Equal(t, epoch, binding.Generation)
		require.Equal(t, entityKey, binding.EntityRef)
	}
	entityKeys := 0
	for _, key := range server.Keys() {
		if strings.Contains(key, "v4:period-session:") {
			entityKeys++
		}
	}
	require.Equal(t, 1, entityKeys, "同一 epoch 只应存在一个共享 period 实体")
}

// TestCodexBindingGenerationRolloverDoesNotRewriteOldBinding 换代只建新记录：
// 旧代次绑定的字节不变，指针推进到新代次后绝不回退。
func TestCodexBindingGenerationRolloverDoesNotRewriteOldBinding(t *testing.T) {
	ctx := context.Background()
	store := newCodexBindingTestStore()
	scope := testBindingScope()
	rawSession := newCodexUUIDv7ForTest(t)

	const generationA, generationB = int64(41), int64(42)

	read := func(key string) string {
		t.Helper()
		value, err := store.GetCodexSessionIdentity(ctx, key)
		require.NoError(t, err)
		return value
	}
	commit := func(generation int64) codexSessionCurrentWrite {
		t.Helper()
		_, write, err := commitCodexSessionBinding(ctx, store, scope, rawSession, codexSessionBinding{
			Attribution:   codexAttributionPeriod,
			Generation:    generation,
			EntityRef:     codexPeriodSessionEntityKey(scope, generation),
			CommittedAtMs: testBindingNowMs(),
		})
		require.NoError(t, err)
		return write
	}

	for _, generation := range []int64{generationA, generationB} {
		entity, err := getOrCreateCodexPeriodSessionEntity(ctx, store, scope, generation, codexFingerprintSession, testBindingNowMs())
		require.NoError(t, err)
		require.True(t, isCodexUUIDv7(entity.IdentitySessionID))
	}

	firstWrite := commit(generationA)
	require.True(t, firstWrite.Applied)
	require.Equal(t, generationA, firstWrite.Current.Generation)

	bindingKeyA := codexSessionBindingKey(codexAttributionPeriod, generationA, scope, rawSession)
	bytesA := read(bindingKeyA)
	require.Contains(t, bytesA, fmt.Sprintf(`"generation":%d`, generationA))
	require.Contains(t, bytesA, `"attribution":"period"`)

	secondWrite := commit(generationB)
	require.True(t, secondWrite.Applied, "较新的代次必须建立新绑定并推进指针")
	require.Equal(t, generationB, secondWrite.Current.Generation)

	bindingKeyB := codexSessionBindingKey(codexAttributionPeriod, generationB, scope, rawSession)
	require.NotEqual(t, bytesA, read(bindingKeyB), "换代必须建立新记录，而不是复用旧记录")

	// 旧代次记录逐字节不变（绑定不可变）。
	require.Equal(t, bytesA, read(bindingKeyA), "旧代次绑定记录不得被改写")

	// 指针只向前：再用旧代次提交一次，驻留指针不得回退。
	regressed := commit(generationA)
	require.False(t, regressed.Applied, "较旧代次不得回写指针")
	require.Equal(t, generationB, regressed.Current.Generation, "指针绝不回退")
	require.Equal(t, bytesA, read(bindingKeyA), "回写失败也不得触碰旧绑定")

	settled, err := resolveCodexSessionBindingCurrent(ctx, store, scope, rawSession, generationB)
	require.NoError(t, err)
	require.NotNil(t, settled)
	require.Equal(t, generationB, settled.Generation)
	require.Equal(t, codexPeriodSessionEntityKey(scope, generationB), settled.EntityRef)

	resident, err := decodeCodexSessionCurrent(read(codexSessionCurrentKey(scope, rawSession)))
	require.NoError(t, err)
	require.Equal(t, generationB, resident.Generation, "驻留指针必须停在较新代次")
}

// TestCodexBindingStagedMigrationRollbackReadsSameIdentities 回滚验证：新模型原值接管
// 旧 side 投影后，旧读路径（readCodexHTTPSideFork + v3 side-session 映射）看到的仍是
// 接管前的同一批身份；新键族与旧键名不冲突。
func TestCodexBindingStagedMigrationRollbackReadsSameIdentities(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	svc := newCodexBindingService(t, server)
	store, ok := svc.cache.(codexSessionIdentityStore)
	require.True(t, ok)

	userScope, accountScope := "user:9104", "platform:openai:account:9104"
	scope := codexBindingScope(userScope, accountScope)
	rawSession := newCodexUUIDv7ForTest(t)
	rawFork := newCodexUUIDv7ForTest(t)

	// 旧模型（pre-existing code）留下的投影：v3 side-session 与 v4 side-fork。
	legacySideSession := newCodexUUIDv7ForTest(t)
	legacySideKey := codexHTTPIdentityMappingKey("side-session", userScope, accountScope, rawSession)
	_, err := store.SetCodexSessionIdentityIfAbsent(ctx, legacySideKey, legacySideSession, 0)
	require.NoError(t, err)

	legacyForkKey := codexHTTPThreadKey("side-fork", userScope, accountScope, "", rawSession)
	legacyFork := codexHTTPSideFork{
		SourceKey:         codexHTTPIdentityMappingKey("thread", userScope, accountScope, rawFork),
		ThreadID:          newCodexUUIDv7ForTest(t),
		PreserveV3Threads: true,
	}
	encodedFork, err := json.Marshal(legacyFork)
	require.NoError(t, err)
	_, err = store.SetCodexSessionIdentityIfAbsent(ctx, legacyForkKey, string(encodedFork), 0)
	require.NoError(t, err)

	legacySideBefore := codexBindingRedisValue(t, server, legacySideKey)
	legacyForkBefore := codexBindingRedisValue(t, server, legacyForkKey)

	// 新模型原值接管。
	projection, err := readCodexLegacySideProjection(ctx, store, userScope, accountScope, rawSession, rawFork)
	require.NoError(t, err)
	require.Equal(t, legacySideSession, projection.SideSessionID)
	require.Equal(t, legacyFork.ThreadID, projection.ForkSource)
	require.True(t, projection.PreserveV3Threads)
	require.Equal(t, legacyFork.SourceKey, projection.SourceKey)

	entity, adopted, err := adoptCodexLegacySideEntity(ctx, store, scope, rawSession, projection,
		codexFingerprintSession, testBindingNowMs())
	require.NoError(t, err)
	require.True(t, adopted, "有旧 side 投影时必须接管，而不是新分配")
	require.Equal(t, legacySideSession, entity.IdentitySessionID, "接管必须原值，不重新分配")
	require.Equal(t, legacyFork.ThreadID, entity.ForkSource)
	require.True(t, entity.PreserveV3Threads)
	require.True(t, entity.Adopted)

	// 回滚：旧读路径看到的必须仍是接管前的原值（会话连续性，而非“能解析新记录”）。
	oldSide, err := store.GetCodexSessionIdentity(ctx, codexHTTPIdentityMappingKey("side-session", userScope, accountScope, rawSession))
	require.NoError(t, err)
	require.Equal(t, legacySideBefore, oldSide)
	require.Equal(t, legacySideSession, strings.TrimSpace(oldSide))
	require.Equal(t, entity.IdentitySessionID, strings.TrimSpace(oldSide),
		"新模型与旧读路径必须指向同一个 side 会话身份")

	forkBack, err := readCodexHTTPSideFork(ctx, store, legacyForkKey)
	require.NoError(t, err)
	require.Equal(t, legacyFork, forkBack, "旧 side-fork 记录不得被改写")
	require.Equal(t, legacyForkBefore, codexBindingRedisValue(t, server, legacyForkKey))

	// 新键族与旧键名不冲突：既不相等，也不共享前缀语义。
	newKeyFamily := []string{
		codexSideEntityKey(scope, rawSession),
		codexPeriodSessionEntityKey(scope, 1),
		codexFlatEntityKey(scope, codexFingerprintDevice),
		codexSessionCurrentKey(scope, rawSession),
		codexSessionBindingKey(codexAttributionSide, codexConstantGeneration, scope, rawSession),
		codexThreadExactKey(scope, rawFork, codexSideEntityKey(scope, rawSession)),
		codexThreadLatestKey(scope, rawFork),
	}
	require.True(t, strings.HasPrefix(legacySideKey, "v3:side-session:"))
	require.True(t, strings.HasPrefix(legacyForkKey, "v4:side-fork:"))
	seen := map[string]bool{}
	for _, key := range newKeyFamily {
		require.NotEqual(t, legacySideKey, key, "新键族不得与旧 side-session 键同名")
		require.NotEqual(t, legacyForkKey, key, "新键族不得与旧 side-fork 键同名")
		require.False(t, seen[key], "新键族内部不得重复：%s", key)
		require.True(t, strings.HasPrefix(key, "v4:"), "新键族统一在 v4 命名空间：%s", key)
		seen[key] = true
	}
	require.True(t, strings.HasPrefix(codexSideEntityKey(scope, rawSession), "v4:side-session:v2:"))
	require.NotEqual(t, legacySideKey, codexSideEntityKey(scope, rawSession))
	require.NotEqual(t, legacyForkKey, codexThreadLatestKey(scope, rawFork))
}

// TestCodexBindingForceAccountRuleDisablesBindingIO 逃生门：force_account_rule 打开后
// 完全按账号当前模式解析（即 legacy 行为），且一个绑定键都不读写。
func TestCodexBindingForceAccountRuleDisablesBindingIO(t *testing.T) {
	server := miniredis.RunT(t)
	svc := newCodexBindingService(t, server)
	require.True(t, svc.codexSessionBindingEnabled())
	svc.cfg.Gateway.CodexIdentity.ForceAccountRule = true
	require.True(t, svc.codexForceAccountRule())

	account := newTestOAuthAccount(9105, map[string]any{codexFingerprintModeExtraKey: string(codexFingerprintDevice)})
	now := time.Now()
	seed, ok := codexFingerprintSeed(account.Extra)
	require.True(t, ok)
	rawSession := newCodexUUIDv7ForTest(t)

	resolve := func() *codexFingerprintIDs {
		t.Helper()
		ids, err := svc.resolveCodexHTTPFingerprintIDs(context.Background(),
			codexBindingRequest(t, 9105, 91051, rawSession, newCodexUUIDv7ForTest(t), ""), account, now)
		require.NoError(t, err)
		return ids
	}

	// device 阶段：跟随账号模式，且不写任何绑定键。
	device := resolve()
	require.NotNil(t, device)
	require.False(t, device.httpSessionIdentity)
	require.Equal(t, codexFingerprintDevice, device.mode)
	require.Empty(t, codexBindingFamilyKeys(server), "force_account_rule 下不得写任何绑定键")

	// 账号切成 session：逃生门必须完全走 legacy 解析器。
	account.Extra[codexFingerprintModeExtraKey] = string(codexFingerprintSession)
	session := resolve()
	require.NotNil(t, session)
	require.True(t, session.httpSessionIdentity, "必须跟随账号当前模式")
	require.Equal(t, codexFingerprintSession, session.mode)
	require.True(t, isCodexUUIDv7(session.sessionID))
	require.Empty(t, codexBindingFamilyKeys(server), "force_account_rule 下不得写任何绑定键")

	// legacy 解析器确实跑了：period 映射以既有键名驻留，且与出站身份一致。
	periodKey := resolveCodexSessionPeriod(seed, "user:9105", codexSessionIdentityUpstreamScope(account), now).key
	legacyPeriod, err := server.Get(codexBindingRedisKeyPrefix + periodKey)
	require.NoError(t, err, "legacy period 映射必须存在，证明逃生门走的是旧解析器")
	require.Equal(t, session.sessionID, legacyPeriod)
}

// TestCodexBindingStagedMigrationSessionModeGateFlipPreservesIdentity 记录 gate 翻转
// 自身的连续性边界：对已经处于 session 模式的账号，旧解析器把上游会话 id 存在 v3 period
// 映射里（键就是 period.key），新模型为同一 (user, account, epoch) 分配一个全新的 period
// 实体 —— 两者不是同一个 UUID，也没有任何接管。因此在途会话在 gate 打开后的第一次请求会
// 静默换一个上游会话 id（不报错）。运行手册必须把 gate 翻转本身当作一次会话边界，
// 报告里的“旧会话不改规则”只对 **模式翻转** 成立，不对 gate 翻转成立。
func TestCodexBindingStagedMigrationSessionModeGateFlipPreservesIdentity(t *testing.T) {
	server := miniredis.RunT(t)
	svc := newCodexPeriodRedisService(t, server) // gate 关闭 = legacy
	require.False(t, svc.codexSessionBindingEnabled())

	account := newTestOAuthAccount(9103, map[string]any{codexFingerprintModeExtraKey: string(codexFingerprintSession)})
	now := time.Now()
	rawSession := newCodexUUIDv7ForTest(t)

	legacy, err := svc.resolveCodexHTTPFingerprintIDs(context.Background(),
		codexBindingRequest(t, 9103, 91031, rawSession, newCodexUUIDv7ForTest(t), ""), account, now)
	require.NoError(t, err)
	require.True(t, legacy.httpSessionIdentity)
	require.True(t, isCodexUUIDv7(legacy.sessionID))

	// 运行手册第 1 步：只打开 gate，账号模式保持不变。
	svc.cfg.Gateway.CodexIdentity.SessionBinding = config.CodexSessionBindingBinding

	session, err := svc.resolveCodexHTTPFingerprintIDs(context.Background(),
		codexBindingRequest(t, 9103, 91031, rawSession, newCodexUUIDv7ForTest(t), ""), account, now)
	require.NoError(t, err, "gate 翻转本身不报错")
	require.True(t, session.httpSessionIdentity)
	require.Equal(t, legacy.sessionID, session.sessionID,
		"gate 翻转必须**原值接管** legacy 的 period 会话 id：已在 session 模式下的在途会话不得换一个上游 session")
}

// TestCodexBindingStagedMigrationLegacySideProjectionIsAdopted 锁定不变量：
// 文档承诺“旧 side 的投影（session id、fork 源、preserve_v3_threads）原值接管，不重算”。
// 这条用例曾经是**失败的反例**——当时门控入口只认新键族的 thread-latest/thread-exact，
// 从不读 v3:side-session / v4:side-fork，导致 gate 一打开、带原生 side 证据的在途会话
// 立刻以 ErrCodexBindingUnresolvedSource 断流（正是分段迁移第 1 步要避免的）。
// 现在 side 分支先做原值接管，因此本用例转为绿灯并成为该不变量的回归锁：
// 若有人再把接管路径摘掉，这里会立刻变红。
func TestCodexBindingStagedMigrationLegacySideProjectionIsAdopted(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	svc := newCodexBindingService(t, server)
	store, ok := svc.cache.(codexSessionIdentityStore)
	require.True(t, ok)

	const userID, apiKeyID = int64(9106), int64(91061)
	userScope := fmt.Sprintf("user:%d", userID)
	account := newTestOAuthAccount(9106, map[string]any{codexFingerprintModeExtraKey: string(codexFingerprintSession)})
	accountScope := codexSessionIdentityUpstreamScope(account)
	rawSession := newCodexUUIDv7ForTest(t)
	rawFork := newCodexUUIDv7ForTest(t)
	legacySideSession := newCodexUUIDv7ForTest(t)

	// 旧模型留下的投影：v3 side-session + v4 side-fork（在途 side 会话的全部证据）。
	legacySideKey := codexHTTPIdentityMappingKey("side-session", userScope, accountScope, rawSession)
	_, err := store.SetCodexSessionIdentityIfAbsent(ctx, legacySideKey, legacySideSession, 0)
	require.NoError(t, err)
	fork := codexHTTPSideFork{
		SourceKey:         codexHTTPIdentityMappingKey("thread", userScope, accountScope, rawFork),
		ThreadID:          newCodexUUIDv7ForTest(t),
		PreserveV3Threads: true,
	}
	encodedFork, err := json.Marshal(fork)
	require.NoError(t, err)
	_, err = store.SetCodexSessionIdentityIfAbsent(ctx, codexHTTPThreadKey("side-fork", userScope, accountScope, "", rawSession), string(encodedFork), 0)
	require.NoError(t, err)

	// 在途 side 会话的原生证据：fork 标记 + fork ordinal（legacy 判定 side root 的依据）。
	c := newCodexSessionIdentityV2Context(t, userID, apiKeyID)
	c.Request.Header.Set("session-id", rawSession)
	c.Request.Header.Set("thread-id", newCodexUUIDv7ForTest(t))
	c.Request.Header.Set("x-codex-turn-metadata",
		fmt.Sprintf(`{"forked_from_thread_id":%q,"forked_from_ordinal_exclusive":1}`, rawFork))
	stageCodexSessionIdentityInputMap(c, nil)

	ids, err := svc.resolveCodexHTTPFingerprintIDs(ctx, c, account, time.Now())
	require.NoError(t, err, "在途 side 会话在 gate 打开后不得解析失败（文档承诺原值接管）")
	require.NotNil(t, ids)
	require.Equal(t, legacySideSession, ids.sessionID, "接管必须保持旧 side 的会话身份")
}
