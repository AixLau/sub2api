package service

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 阶段 A 反例测试：只针对绑定模型本身，不接入转发路径。
//
// 这些用例逐条对应方案里的 A1–A10，且刻意围绕三条不变量构造：
//  1. latest 命中 ≠ 来源代次已确定
//  2. current 缺失 ≠ 新会话
//  3. 单 key SETNX ≠ 整个关系已原子提交

const (
	testBindingUserScope    = "user:4242"
	testBindingAccountScope = "platform:openai:account:335"
)

func newCodexBindingTestStore() *codexSessionIdentityTestStore {
	return &codexSessionIdentityTestStore{values: make(map[string]string)}
}

func testBindingScope() string {
	return codexBindingScope(testBindingUserScope, testBindingAccountScope)
}

func testBindingNowMs() int64 { return 1_700_000_000_000 }

// ---------------------------------------------------------------------------
// A2 不同 raw session 共享 period
// ---------------------------------------------------------------------------

func TestCodexSessionBindingPeriodEntityIsSharedAcrossRawSessions(t *testing.T) {
	ctx := context.Background()
	store := newCodexBindingTestStore()
	scope := testBindingScope()

	first, err := getOrCreateCodexPeriodSessionEntity(ctx, store, scope, 7, codexFingerprintDevice, testBindingNowMs())
	require.NoError(t, err)
	second, err := getOrCreateCodexPeriodSessionEntity(ctx, store, scope, 7, codexFingerprintSession, testBindingNowMs()+1)
	require.NoError(t, err)

	require.Equal(t, first.IdentitySessionID, second.IdentitySessionID,
		"同一 (scope, generation) 下所有 raw session 必须共享同一个上游 session id")
	require.Equal(t, codexFingerprintDevice, second.Rule,
		"规则归共享实体：后来的调用不得改写首个建立者决定的规则")
	require.True(t, isCodexUUIDv7(first.IdentitySessionID))

	nextGeneration, err := getOrCreateCodexPeriodSessionEntity(ctx, store, scope, 8, codexFingerprintDevice, testBindingNowMs())
	require.NoError(t, err)
	require.NotEqual(t, first.IdentitySessionID, nextGeneration.IdentitySessionID,
		"换代必须建立新的共享实体，旧实体保持不动")

	rawA := newCodexUUIDv7ForTest(t)
	rawB := newCodexUUIDv7ForTest(t)
	_, _, err = commitCodexSessionBinding(ctx, store, scope, rawA, codexSessionBinding{
		Attribution: codexAttributionPeriod, Generation: 7, EntityRef: codexPeriodSessionEntityKey(scope, 7),
	})
	require.NoError(t, err)
	_, _, err = commitCodexSessionBinding(ctx, store, scope, rawB, codexSessionBinding{
		Attribution: codexAttributionPeriod, Generation: 8, EntityRef: codexPeriodSessionEntityKey(scope, 8),
	})
	require.NoError(t, err)

	currentA, err := resolveCodexSessionBindingCurrent(ctx, store, scope, rawA, 8)
	require.NoError(t, err)
	require.Equal(t, int64(7), currentA.Generation, "两个 raw session 的绑定必须互不覆盖")
	currentB, err := resolveCodexSessionBindingCurrent(ctx, store, scope, rawB, 8)
	require.NoError(t, err)
	require.Equal(t, int64(8), currentB.Generation)
}

func TestCodexSessionBindingConcurrentPeriodAllocationConverges(t *testing.T) {
	ctx := context.Background()
	store := newCodexBindingTestStore()
	scope := testBindingScope()

	const workers = 16
	results := make([]string, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			entity, err := getOrCreateCodexPeriodSessionEntity(ctx, store, scope, 3, codexFingerprintSession, testBindingNowMs())
			assert.NoError(t, err)
			results[index] = entity.IdentitySessionID
		}(i)
	}
	wg.Wait()

	for i := 1; i < workers; i++ {
		require.Equal(t, results[0], results[i], "并发分配必须收敛到同一个共享 session id")
	}
	require.Equal(t, 1, store.sets, "只应产生一次真实写入")
}

// ---------------------------------------------------------------------------
// A3 写 binding 后中断（不变量 2、3）
// ---------------------------------------------------------------------------

func TestCodexSessionBindingRecoversCurrentAfterCommitInterruption(t *testing.T) {
	ctx := context.Background()
	store := newCodexBindingTestStore()
	scope := testBindingScope()
	rawSession := newCodexUUIDv7ForTest(t)

	entity, err := getOrCreateCodexPeriodSessionEntity(ctx, store, scope, 11, codexFingerprintSession, testBindingNowMs())
	require.NoError(t, err)

	binding := codexSessionBinding{
		Version: codexSessionBindingVersion, Attribution: codexAttributionPeriod, Generation: 11,
		EntityRef: codexPeriodSessionEntityKey(scope, 11), CommittedAtMs: testBindingNowMs(),
	}
	encoded, err := encodeCodexBindingRecord(binding)
	require.NoError(t, err)
	handle := codexSessionBindingKey(codexAttributionPeriod, 11, scope, rawSession)
	_, err = store.SetCodexSessionIdentityIfAbsent(ctx, handle, encoded, 0)
	require.NoError(t, err)
	_, hasCurrent := store.values[codexSessionCurrentKey(scope, rawSession)]
	require.False(t, hasCurrent, "前置条件：模拟写完 binding 后中断，current 尚未写入")

	current, err := resolveCodexSessionBindingCurrent(ctx, store, scope, rawSession, 11)
	require.NoError(t, err)
	require.NotNil(t, current, "current 缺失不得被判定为新会话：必须探测到已提交的 binding")
	require.Equal(t, int64(11), current.Generation)
	require.Equal(t, binding.EntityRef, current.EntityRef)

	sessionID, rule, err := readCodexSessionEntity(ctx, store, current.EntityRef)
	require.NoError(t, err)
	require.Equal(t, entity.IdentitySessionID, sessionID)
	require.Equal(t, codexFingerprintSession, rule)

	// 修复必须幂等。
	again, err := resolveCodexSessionBindingCurrent(ctx, store, scope, rawSession, 11)
	require.NoError(t, err)
	require.Equal(t, *current, *again)

	// 完全无记录时才是"没有已提交绑定"——这与"新会话"仍需证据判定是两回事。
	none, err := resolveCodexSessionBindingCurrent(ctx, store, scope, newCodexUUIDv7ForTest(t), 11)
	require.NoError(t, err)
	require.Nil(t, none)
}

func TestCodexSessionBindingCurrentWithoutBindingIsAnomaly(t *testing.T) {
	ctx := context.Background()
	store := newCodexBindingTestStore()
	scope := testBindingScope()
	rawSession := newCodexUUIDv7ForTest(t)

	orphan, err := encodeCodexBindingRecord(codexSessionCurrent{
		Version: codexSessionBindingVersion, Attribution: codexAttributionPeriod,
		Generation: 12, EntityRef: codexPeriodSessionEntityKey(scope, 12),
	})
	require.NoError(t, err)
	store.values[codexSessionCurrentKey(scope, rawSession)] = orphan

	_, err = resolveCodexSessionBindingCurrent(ctx, store, scope, rawSession, 12)
	require.ErrorIs(t, err, ErrCodexBindingRecordAnomaly,
		"current 指向不存在的 binding 属于记录自相矛盾，必须明确失败而不是静默重建")
}

// ---------------------------------------------------------------------------
// A6 跨代次：旧绑定不被改写
// ---------------------------------------------------------------------------

func TestCodexSessionBindingAcrossGenerationsDoesNotRewriteOldBinding(t *testing.T) {
	ctx := context.Background()
	store := newCodexBindingTestStore()
	scope := testBindingScope()
	rawSession := newCodexUUIDv7ForTest(t)

	_, err := getOrCreateCodexPeriodSessionEntity(ctx, store, scope, 21, codexFingerprintDevice, testBindingNowMs())
	require.NoError(t, err)
	_, _, err = commitCodexSessionBinding(ctx, store, scope, rawSession, codexSessionBinding{
		Attribution: codexAttributionPeriod, Generation: 21, EntityRef: codexPeriodSessionEntityKey(scope, 21),
	})
	require.NoError(t, err)
	oldHandle := codexSessionBindingKey(codexAttributionPeriod, 21, scope, rawSession)
	oldBytes := store.values[oldHandle]

	_, err = getOrCreateCodexPeriodSessionEntity(ctx, store, scope, 22, codexFingerprintSession, testBindingNowMs())
	require.NoError(t, err)
	_, _, err = commitCodexSessionBinding(ctx, store, scope, rawSession, codexSessionBinding{
		Attribution: codexAttributionPeriod, Generation: 22, EntityRef: codexPeriodSessionEntityKey(scope, 22),
	})
	require.NoError(t, err)

	require.Equal(t, oldBytes, store.values[oldHandle], "跨代次只建立新记录，绝不改写旧绑定")
	current, err := resolveCodexSessionBindingCurrent(ctx, store, scope, rawSession, 22)
	require.NoError(t, err)
	require.Equal(t, int64(22), current.Generation, "指针在换代时推进到新代次")
}

// ---------------------------------------------------------------------------
// A1 / A8 来源解析：latest 只是线索
// ---------------------------------------------------------------------------

func TestCodexForkSourceLatestWithoutExactIsUnresolved(t *testing.T) {
	ctx := context.Background()
	store := newCodexBindingTestStore()
	scope := testBindingScope()
	rawThread := newCodexUUIDv7ForTest(t)

	require.NoError(t, recordCodexThreadLatest(ctx, store, scope, rawThread, codexThreadLatest{
		EntityRef: codexPeriodSessionEntityKey(scope, 31), Attribution: codexAttributionPeriod,
		Generation: 31, ObservedAtMs: testBindingNowMs(),
	}))

	_, err := resolveCodexThreadForkSource(ctx, store, scope, rawThread)
	require.ErrorIs(t, err, ErrCodexBindingUnresolvedSource,
		"latest 命中不构成来源已确定：exact 缺失时必须明确失败")

	before, err := resolveCodexThreadForkSource(ctx, store, scope, newCodexUUIDv7ForTest(t))
	require.ErrorIs(t, err, ErrCodexBindingUnresolvedSource)
	require.Empty(t, before)
}

func TestCodexForkSourceConcurrentResolutionConvergesOnRealSource(t *testing.T) {
	ctx := context.Background()
	store := newCodexBindingTestStore()
	scope := testBindingScope()
	rawThread := newCodexUUIDv7ForTest(t)

	entity, err := getOrCreateCodexPeriodSessionEntity(ctx, store, scope, 41, codexFingerprintSession, testBindingNowMs())
	require.NoError(t, err)
	entityRef := codexPeriodSessionEntityKey(scope, 41)
	realThreadID := codexHTTPSessionThreadID(entity.IdentitySessionID, rawThread)
	_, err = registerCodexThreadExact(ctx, store, scope, rawThread, codexThreadExact{
		SessionID: entity.IdentitySessionID, ThreadID: realThreadID, EntityRef: entityRef,
		Attribution: codexAttributionPeriod, Generation: 41,
	})
	require.NoError(t, err)
	require.NoError(t, recordCodexThreadLatest(ctx, store, scope, rawThread, codexThreadLatest{
		EntityRef: entityRef, Attribution: codexAttributionPeriod, Generation: 41, ObservedAtMs: testBindingNowMs(),
	}))

	const workers = 16
	results := make([]codexThreadExact, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			exact, resolveErr := resolveCodexThreadForkSource(ctx, store, scope, rawThread)
			assert.NoError(t, resolveErr)
			results[index] = exact
		}(i)
	}
	wg.Wait()

	for i := 1; i < workers; i++ {
		require.Equal(t, results[0], results[i], "并发解析必须收敛到同一个真实来源")
	}
	require.Equal(t, realThreadID, results[0].ThreadID)
}

func TestCodexThreadForkSourceReturnsRealChildBindingNotSynthesizedValue(t *testing.T) {
	ctx := context.Background()
	store := newCodexBindingTestStore()
	scope := testBindingScope()
	rawThread := newCodexUUIDv7ForTest(t)

	// 该 thread 先作为普通 child 在 period 实体中被确定。
	period, err := getOrCreateCodexPeriodSessionEntity(ctx, store, scope, 51, codexFingerprintSession, testBindingNowMs())
	require.NoError(t, err)
	periodRef := codexPeriodSessionEntityKey(scope, 51)
	realThreadID := codexHTTPSessionThreadID(period.IdentitySessionID, rawThread)
	_, err = registerCodexThreadExact(ctx, store, scope, rawThread, codexThreadExact{
		SessionID: period.IdentitySessionID, ThreadID: realThreadID, EntityRef: periodRef,
		Attribution: codexAttributionPeriod, Generation: 51,
	})
	require.NoError(t, err)
	require.NoError(t, recordCodexThreadLatest(ctx, store, scope, rawThread, codexThreadLatest{
		EntityRef: periodRef, Attribution: codexAttributionPeriod, Generation: 51, ObservedAtMs: testBindingNowMs(),
	}))

	// 随后被 /side 当作 fork 源引用。
	exact, err := resolveCodexThreadForkSource(ctx, store, scope, rawThread)
	require.NoError(t, err)
	require.Equal(t, realThreadID, exact.ThreadID)
	require.Equal(t, period.IdentitySessionID, exact.SessionID)

	// 合成值 f(sideSession, rawFork) 与真实来源不是同一绑定——必须能区分开。
	sideSessionID := newCodexUUIDv7ForTest(t)
	synthesized := codexHTTPSessionThreadID(sideSessionID, rawThread)
	require.NotEqual(t, exact.ThreadID, synthesized,
		"fork 源必须是真实来源，而不是在新 side session 内重算的替代品")
}

// ---------------------------------------------------------------------------
// A9 来源记录缺失：明确失败且不写任何 key
// ---------------------------------------------------------------------------

func TestCodexUnresolvedForkSourceWritesNoKeys(t *testing.T) {
	ctx := context.Background()
	store := newCodexBindingTestStore()
	scope := testBindingScope()

	_, err := resolveCodexThreadForkSource(ctx, store, scope, newCodexUUIDv7ForTest(t))
	require.ErrorIs(t, err, ErrCodexBindingUnresolvedSource)
	require.Empty(t, store.values, "源未登记时不得写入任何 key")

	_, err = getOrCreateCodexSideEntity(ctx, store, scope, newCodexUUIDv7ForTest(t),
		codexFingerprintSession, "", false, testBindingNowMs())
	require.ErrorIs(t, err, ErrCodexBindingUnresolvedSource,
		"来源未确证时不得建立 side 实体（不合成、不重开）")
	require.Empty(t, store.values)

	// 源登记之后，同一 raw session 的重试才能成功。
	rawThread := newCodexUUIDv7ForTest(t)
	period, err := getOrCreateCodexPeriodSessionEntity(ctx, store, scope, 61, codexFingerprintSession, testBindingNowMs())
	require.NoError(t, err)
	periodRef := codexPeriodSessionEntityKey(scope, 61)
	_, err = registerCodexThreadExact(ctx, store, scope, rawThread, codexThreadExact{
		SessionID: period.IdentitySessionID, ThreadID: codexHTTPSessionThreadID(period.IdentitySessionID, rawThread),
		EntityRef: periodRef, Attribution: codexAttributionPeriod, Generation: 61,
	})
	require.NoError(t, err)
	require.NoError(t, recordCodexThreadLatest(ctx, store, scope, rawThread, codexThreadLatest{
		EntityRef: periodRef, Attribution: codexAttributionPeriod, Generation: 61, ObservedAtMs: testBindingNowMs(),
	}))

	rawSession := newCodexUUIDv7ForTest(t)
	side, err := getOrCreateCodexSideEntity(ctx, store, scope, rawSession, codexFingerprintSession,
		codexHTTPSessionThreadID(period.IdentitySessionID, rawThread), false, testBindingNowMs())
	require.NoError(t, err)
	require.Equal(t, codexHTTPSessionThreadID(period.IdentitySessionID, rawThread), side.ForkSource)
	require.False(t, side.Adopted)
}

// ---------------------------------------------------------------------------
// A7 / A4 旧 side 完整投影接管
// ---------------------------------------------------------------------------

func seedLegacySide(t *testing.T, store *codexSessionIdentityTestStore, rawSession, rawFork string) (string, string) {
	t.Helper()
	legacySideSessionID := newCodexUUIDv7ForTest(t)
	forkThreadID := newCodexUUIDv7ForTest(t)
	store.values[codexHTTPIdentityMappingKey("side-session", testBindingUserScope, testBindingAccountScope, rawSession)] = legacySideSessionID
	legacyFork, err := json.Marshal(codexHTTPSideFork{
		SourceKey:         codexHTTPIdentityMappingKey("thread", testBindingUserScope, testBindingAccountScope, rawFork),
		ThreadID:          forkThreadID,
		PreserveV3Threads: true,
	})
	require.NoError(t, err)
	store.values[codexHTTPThreadKey("side-fork", testBindingUserScope, testBindingAccountScope, "", rawSession)] = string(legacyFork)
	return legacySideSessionID, forkThreadID
}

func TestCodexLegacySideProjectionIsAdoptedVerbatim(t *testing.T) {
	ctx := context.Background()
	store := newCodexBindingTestStore()
	scope := testBindingScope()
	rawSession := newCodexUUIDv7ForTest(t)
	rawFork := newCodexUUIDv7ForTest(t)

	legacySideSessionID, forkThreadID := seedLegacySide(t, store, rawSession, rawFork)
	sideSessionKey := codexHTTPIdentityMappingKey("side-session", testBindingUserScope, testBindingAccountScope, rawSession)
	forkKey := codexHTTPThreadKey("side-fork", testBindingUserScope, testBindingAccountScope, "", rawSession)
	legacyBytes := map[string]string{
		sideSessionKey: store.values[sideSessionKey],
		forkKey:        store.values[forkKey],
	}

	projection, err := readCodexLegacySideProjection(ctx, store, testBindingUserScope, testBindingAccountScope, rawSession, rawFork)
	require.NoError(t, err)
	require.Equal(t, legacySideSessionID, projection.SideSessionID)
	require.Equal(t, forkThreadID, projection.ForkSource)
	require.True(t, projection.PreserveV3Threads)

	entity, adopted, err := adoptCodexLegacySideEntity(ctx, store, scope, rawSession, projection,
		codexFingerprintSession, testBindingNowMs())
	require.NoError(t, err)
	require.True(t, adopted)
	require.True(t, entity.Adopted)
	require.Equal(t, legacySideSessionID, entity.IdentitySessionID,
		"接管必须用旧存值，不得重新分配 session id")
	require.Equal(t, forkThreadID, entity.ForkSource,
		"接管必须固定真实来源，不得改写指向")
	require.True(t, entity.PreserveV3Threads, "投影必须整套接管")

	for key, want := range legacyBytes {
		require.Equal(t, want, store.values[key], "接管只读旧键，绝不就地改写")
	}
}

func TestCodexAdoptedSideContinuesWithoutForkEvidence(t *testing.T) {
	ctx := context.Background()
	store := newCodexBindingTestStore()
	scope := testBindingScope()
	rawSession := newCodexUUIDv7ForTest(t)
	rawFork := newCodexUUIDv7ForTest(t)

	legacySideSessionID, forkThreadID := seedLegacySide(t, store, rawSession, rawFork)
	projection, err := readCodexLegacySideProjection(ctx, store, testBindingUserScope, testBindingAccountScope, rawSession, rawFork)
	require.NoError(t, err)
	entity, adopted, err := adoptCodexLegacySideEntity(ctx, store, scope, rawSession, projection,
		codexFingerprintSession, testBindingNowMs())
	require.NoError(t, err)
	require.True(t, adopted)

	_, _, err = commitCodexSessionBinding(ctx, store, scope, rawSession, codexSessionBinding{
		Attribution: codexAttributionSide, Generation: codexConstantGeneration, EntityRef: codexSideEntityKey(scope, rawSession),
	})
	require.NoError(t, err)

	// 后续 continuation / spawn_agent 请求只带 raw session，不再带 fork 字段：
	// 由 current → binding → 实体解出即可，全程不触碰 fork 证据。
	current, err := resolveCodexSessionBindingCurrent(ctx, store, scope, rawSession, 99)
	require.NoError(t, err)
	require.NotNil(t, current)
	require.Equal(t, codexAttributionSide, current.Attribution,
		"已建立的 side 不得因缺少 fork 字段回落到 period")
	require.Equal(t, codexConstantGeneration, current.Generation)

	continuedSessionID, continuedRule, err := readCodexSessionEntity(ctx, store, current.EntityRef)
	require.NoError(t, err)
	require.Equal(t, legacySideSessionID, continuedSessionID)
	require.Equal(t, codexFingerprintSession, continuedRule)
	require.Equal(t, forkThreadID, entity.ForkSource)

	_, hasPeriodEntity := store.values[codexPeriodSessionEntityKey(scope, 99)]
	require.False(t, hasPeriodEntity, "side 继续时不得连带建立 period 实体")
}

// ---------------------------------------------------------------------------
// A5 / A10 回滚：验证会话连续性，而不只是"旧版能解析"
// ---------------------------------------------------------------------------

func TestCodexBindingRollbackKeepsLegacySessionContinuity(t *testing.T) {
	ctx := context.Background()
	store := newCodexBindingTestStore()
	scope := testBindingScope()
	rawSession := newCodexUUIDv7ForTest(t)
	rawFork := newCodexUUIDv7ForTest(t)

	legacySideSessionID, forkThreadID := seedLegacySide(t, store, rawSession, rawFork)
	sideSessionKey := codexHTTPIdentityMappingKey("side-session", testBindingUserScope, testBindingAccountScope, rawSession)
	forkKey := codexHTTPThreadKey("side-fork", testBindingUserScope, testBindingAccountScope, "", rawSession)
	legacyBytes := map[string]string{
		sideSessionKey: store.values[sideSessionKey],
		forkKey:        store.values[forkKey],
	}
	projection, err := readCodexLegacySideProjection(ctx, store, testBindingUserScope, testBindingAccountScope, rawSession, rawFork)
	require.NoError(t, err)
	_, adopted, err := adoptCodexLegacySideEntity(ctx, store, scope, rawSession, projection,
		codexFingerprintSession, testBindingNowMs())
	require.NoError(t, err)
	require.True(t, adopted)
	_, _, err = commitCodexSessionBinding(ctx, store, scope, rawSession, codexSessionBinding{
		Attribution: codexAttributionSide, Generation: codexConstantGeneration, EntityRef: codexSideEntityKey(scope, rawSession),
	})
	require.NoError(t, err)
	_, err = registerCodexThreadExact(ctx, store, scope, rawFork, codexThreadExact{
		SessionID: legacySideSessionID, ThreadID: forkThreadID,
		EntityRef: codexSideEntityKey(scope, rawSession), Attribution: codexAttributionSide, Generation: codexConstantGeneration,
	})
	require.NoError(t, err)

	// 回滚到旧版读取路径：旧读取器只看旧键。
	rolledBackFork, err := readCodexHTTPSideFork(ctx, store,
		codexHTTPThreadKey("side-fork", testBindingUserScope, testBindingAccountScope, "", rawSession))
	require.NoError(t, err)
	require.Equal(t, forkThreadID, rolledBackFork.ThreadID, "回滚后旧版必须读到接管前的同一个 fork 目标")
	require.True(t, rolledBackFork.PreserveV3Threads)

	rolledBackSide, err := store.GetCodexSessionIdentity(ctx,
		codexHTTPIdentityMappingKey("side-session", testBindingUserScope, testBindingAccountScope, rawSession))
	require.NoError(t, err)
	require.Equal(t, legacySideSessionID, rolledBackSide, "回滚后旧版必须读到接管前的同一个 side session")

	// 新键族不得被旧读取器误读成正常固定来源：回滚读取器使用的键与新键不是同一个。
	require.NotEqual(t, sideSessionKey, codexSideEntityKey(scope, rawSession),
		"side 实体键不得与旧 side-session 键重合，否则回滚会误读新格式")
	require.NotEqual(t, forkKey, codexThreadExactKey(scope, rawFork, codexSideEntityKey(scope, rawSession)),
		"thread-exact 键不得与旧 side-fork 键重合")
	require.Equal(t, legacyBytes[sideSessionKey], store.values[sideSessionKey])
	require.Equal(t, legacyBytes[forkKey], store.values[forkKey])
}

// ---------------------------------------------------------------------------
// session-current 的并发语义
// ---------------------------------------------------------------------------

// countCodexBindingKeys 统计满足前缀的键数量，用于断言"只产生一条记录"。
func countCodexBindingKeys(store *codexSessionIdentityTestStore, prefix string) int {
	count := 0
	for key := range store.values {
		if strings.HasPrefix(key, prefix) {
			count++
		}
	}
	return count
}

// 同代次并发创建：正常 CAS 竞争不是记录异常，且必须整份复用胜出绑定。
func TestCodexSessionCurrentSameGenerationConcurrentCommitReusesWholeWinner(t *testing.T) {
	ctx := context.Background()
	store := newCodexBindingTestStore()
	scope := testBindingScope()
	rawSession := newCodexUUIDv7ForTest(t)

	entity, err := getOrCreateCodexPeriodSessionEntity(ctx, store, scope, 71, codexFingerprintSession, testBindingNowMs())
	require.NoError(t, err)
	entityRef := codexPeriodSessionEntityKey(scope, 71)

	const workers = 16
	got := make([]codexSessionBinding, workers)
	writes := make([]codexSessionCurrentWrite, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			// 每个调用方都携带**不同**的候选（CommittedAtMs 各自不同），
			// 以此验证返回值整份来自胜出记录，而不是候选与胜出混用字段。
			binding, write, commitErr := commitCodexSessionBinding(ctx, store, scope, rawSession, codexSessionBinding{
				Attribution: codexAttributionPeriod, Generation: 71, EntityRef: entityRef,
				CommittedAtMs: testBindingNowMs() + int64(index),
			})
			got[index], writes[index], errs[index] = binding, write, commitErr
		}(i)
	}
	wg.Wait()

	for i := 0; i < workers; i++ {
		require.NoError(t, errs[i], "同键合法竞争不得被当作记录异常")
	}
	for i := 1; i < workers; i++ {
		require.Equal(t, got[0], got[i], "同键合法竞争必须复用整份胜出绑定，不得混用候选字段")
		require.True(t, writes[i].Applied, "同一目标值的并发写入都应视为已就位")
	}
	require.GreaterOrEqual(t, got[0].CommittedAtMs, testBindingNowMs())
	require.Less(t, got[0].CommittedAtMs, testBindingNowMs()+int64(workers))
	require.Equal(t, 1, countCodexBindingKeys(store, "v4:session-binding:period:71:"),
		"同代次只应产生一条绑定记录")
	require.True(t, isCodexUUIDv7(entity.IdentitySessionID))
}

// 跨代次竞争：指针只向前推进，且请求各自保持已选代次。
func TestCodexSessionCurrentCrossGenerationContentionNeverRegresses(t *testing.T) {
	ctx := context.Background()
	scope := testBindingScope()
	const olderGeneration, newerGeneration = int64(81), int64(82)

	// 竞争结果与调度顺序有关，因此反复运行以覆盖两种交错。
	for iteration := 0; iteration < 50; iteration++ {
		store := newCodexBindingTestStore()
		rawSession := newCodexUUIDv7ForTest(t)
		_, err := getOrCreateCodexPeriodSessionEntity(ctx, store, scope, olderGeneration, codexFingerprintDevice, testBindingNowMs())
		require.NoError(t, err)
		_, err = getOrCreateCodexPeriodSessionEntity(ctx, store, scope, newerGeneration, codexFingerprintSession, testBindingNowMs())
		require.NoError(t, err)

		bindings := make([]codexSessionBinding, 2)
		errs := make([]error, 2)
		var wg sync.WaitGroup
		for i, generation := range []int64{olderGeneration, newerGeneration} {
			wg.Add(1)
			go func(index int, gen int64) {
				defer wg.Done()
				binding, _, commitErr := commitCodexSessionBinding(ctx, store, scope, rawSession, codexSessionBinding{
					Attribution: codexAttributionPeriod, Generation: gen,
					EntityRef: codexPeriodSessionEntityKey(scope, gen),
				})
				bindings[index], errs[index] = binding, commitErr
			}(i, generation)
		}
		wg.Wait()

		for i := range errs {
			require.NoError(t, errs[i], "并发跨代次不得报记录异常")
		}
		require.Equal(t, olderGeneration, bindings[0].Generation, "请求必须保持自己已选的代次")
		require.Equal(t, newerGeneration, bindings[1].Generation, "请求必须保持自己已选的代次")

		resident, err := resolveCodexSessionBindingCurrent(ctx, store, scope, rawSession, olderGeneration)
		require.NoError(t, err)
		require.Equal(t, newerGeneration, resident.Generation,
			"指针只向前推进：最终驻留在较高的代次，无论交错顺序")
	}
}

func TestCodexSessionCurrentDoesNotRegressToOlderGeneration(t *testing.T) {
	ctx := context.Background()
	store := newCodexBindingTestStore()
	scope := testBindingScope()
	rawSession := newCodexUUIDv7ForTest(t)

	commitFor := func(generation int64) (codexSessionBinding, codexSessionCurrentWrite) {
		t.Helper()
		binding, write, err := commitCodexSessionBinding(ctx, store, scope, rawSession, codexSessionBinding{
			Attribution: codexAttributionPeriod, Generation: generation,
			EntityRef: codexPeriodSessionEntityKey(scope, generation),
		})
		require.NoError(t, err)
		return binding, write
	}

	_, newerWrite := commitFor(92)
	require.True(t, newerWrite.Applied)
	require.Equal(t, int64(92), newerWrite.Current.Generation)

	// 延迟到达的旧请求：已选代次是 91。
	olderBinding, olderWrite := commitFor(91)
	require.Equal(t, int64(91), olderBinding.Generation, "请求保持自己已选的代次")
	require.False(t, olderWrite.Applied, "指针已在更新的代次上，本次不推进")
	require.Equal(t, int64(92), olderWrite.Current.Generation, "指针不得回退到更早的代次")

	resident, err := resolveCodexSessionBindingCurrent(ctx, store, scope, rawSession, 91)
	require.NoError(t, err)
	require.Equal(t, int64(92), resident.Generation)
}

// flat:0 稳定性：off/device/full 没有自然周期，永不换代。
func TestCodexSessionCurrentFlatGenerationZeroIsStable(t *testing.T) {
	ctx := context.Background()
	store := newCodexBindingTestStore()
	scope := testBindingScope()
	rawSession := newCodexUUIDv7ForTest(t)

	flatEntity, err := getOrCreateCodexFlatEntity(ctx, store, scope, codexFingerprintDevice, testBindingNowMs())
	require.NoError(t, err)
	require.Equal(t, codexFingerprintDevice, flatEntity.Rule)
	// 同一 (scope, rule) 幂等：重复调用读到同一实体，不改写。
	sameRule, err := getOrCreateCodexFlatEntity(ctx, store, scope, codexFingerprintDevice, testBindingNowMs()+1)
	require.NoError(t, err)
	require.Equal(t, flatEntity.CreatedAtMs, sameRule.CreatedAtMs, "同规则重复调用不得改写实体")

	// 不同规则必须是不同实体——这正是 flat 内切换能对新会话生效的前提。
	// 若像早期实现那样只按 scope 定键并由首个创建者固定规则，device→full 这类
	// 切换对新会话将永不生效，违背"新会话使用新规则"。
	otherRule, err := getOrCreateCodexFlatEntity(ctx, store, scope, codexFingerprintFull, testBindingNowMs()+2)
	require.NoError(t, err)
	require.Equal(t, codexFingerprintFull, otherRule.Rule)
	require.NotEqual(t, codexFlatEntityKey(scope, codexFingerprintDevice), codexFlatEntityKey(scope, codexFingerprintFull),
		"规则必须进入 flat 实体键")

	flatRef := codexFlatEntityKey(scope, codexFingerprintDevice)
	_, write, err := commitCodexSessionBinding(ctx, store, scope, rawSession, codexSessionBinding{
		Attribution: codexAttributionFlat, Generation: codexConstantGeneration, EntityRef: flatRef,
	})
	require.NoError(t, err)
	require.True(t, write.Applied)
	require.Equal(t, codexConstantGeneration, write.Current.Generation)

	identitySessionID, rule, err := readCodexSessionEntity(ctx, store, flatRef)
	require.NoError(t, err)
	require.Empty(t, identitySessionID, "flat 不持有会话身份，空值是语义本身")
	require.Equal(t, codexFingerprintDevice, rule)

	// 关键：调用方的 period 代次无论推进到多大，flat 会话都驻留在 0，不被换代。
	for _, periodGeneration := range []int64{1, 1 << 40} {
		current, resolveErr := resolveCodexSessionBindingCurrent(ctx, store, scope, rawSession, periodGeneration)
		require.NoError(t, resolveErr)
		require.Equal(t, codexAttributionFlat, current.Attribution,
			"flat 会话不得因 period 换代被判成 period")
		require.Equal(t, codexConstantGeneration, current.Generation)
	}

	_, repeated, err := commitCodexSessionBinding(ctx, store, scope, rawSession, codexSessionBinding{
		Attribution: codexAttributionFlat, Generation: codexConstantGeneration, EntityRef: flatRef,
	})
	require.NoError(t, err)
	require.True(t, repeated.Applied, "重复提交幂等")
	require.Equal(t, codexConstantGeneration, repeated.Current.Generation)
	require.Equal(t, 1, countCodexBindingKeys(store, "v4:session-binding:flat:0:"))
}

// 归属一经提交即固定：同一 raw session 上出现不同归属必须明确失败，绝不静默选边。
func TestCodexSessionCurrentAttributionConflictIsExplicit(t *testing.T) {
	ctx := context.Background()

	t.Run("side 已提交后不得改判为 flat", func(t *testing.T) {
		store := newCodexBindingTestStore()
		scope := testBindingScope()
		rawSession := newCodexUUIDv7ForTest(t)

		_, _, err := commitCodexSessionBinding(ctx, store, scope, rawSession, codexSessionBinding{
			Attribution: codexAttributionSide, Generation: codexConstantGeneration,
			EntityRef: codexSideEntityKey(scope, rawSession),
		})
		require.NoError(t, err)

		_, _, err = commitCodexSessionBinding(ctx, store, scope, rawSession, codexSessionBinding{
			Attribution: codexAttributionFlat, Generation: codexConstantGeneration,
			EntityRef: codexFlatEntityKey(scope, codexFingerprintDevice),
		})
		require.ErrorIs(t, err, ErrCodexBindingAttributionConflict,
			"side 与 flat 的代次都是 0，跨归属绝不能被当成正常竞争")

		resident, err := resolveCodexSessionBindingCurrent(ctx, store, scope, rawSession, 5)
		require.NoError(t, err)
		require.Equal(t, codexAttributionSide, resident.Attribution, "原归属不得被改写")
	})

	t.Run("flat 已提交后不得改判为 period", func(t *testing.T) {
		store := newCodexBindingTestStore()
		scope := testBindingScope()
		rawSession := newCodexUUIDv7ForTest(t)

		_, _, err := commitCodexSessionBinding(ctx, store, scope, rawSession, codexSessionBinding{
			Attribution: codexAttributionFlat, Generation: codexConstantGeneration,
			EntityRef: codexFlatEntityKey(scope, codexFingerprintDevice),
		})
		require.NoError(t, err)

		// flat(0) 与 period(G) 之间不存在"向前推进"：切模式不得移动旧会话。
		_, _, err = commitCodexSessionBinding(ctx, store, scope, rawSession, codexSessionBinding{
			Attribution: codexAttributionPeriod, Generation: 900,
			EntityRef: codexPeriodSessionEntityKey(scope, 900),
		})
		require.ErrorIs(t, err, ErrCodexBindingAttributionConflict,
			"flat 会话不得因 period 代次更大而被改判")
	})

	t.Run("探测到多个归属时明确失败", func(t *testing.T) {
		store := newCodexBindingTestStore()
		scope := testBindingScope()
		rawSession := newCodexUUIDv7ForTest(t)

		// 构造撕裂状态：两条不同归属的绑定并存，且 current 缺失。
		for _, binding := range []codexSessionBinding{
			{
				Version: codexSessionBindingVersion, Attribution: codexAttributionSide,
				Generation: codexConstantGeneration, EntityRef: codexSideEntityKey(scope, rawSession),
			},
			{
				Version: codexSessionBindingVersion, Attribution: codexAttributionFlat,
				Generation: codexConstantGeneration, EntityRef: codexFlatEntityKey(scope, codexFingerprintDevice),
			},
		} {
			encoded, err := encodeCodexBindingRecord(binding)
			require.NoError(t, err)
			_, err = store.SetCodexSessionIdentityIfAbsent(ctx,
				codexSessionBindingKey(binding.Attribution, binding.Generation, scope, rawSession), encoded, 0)
			require.NoError(t, err)
		}

		_, err := resolveCodexSessionBindingCurrent(ctx, store, scope, rawSession, 5)
		require.ErrorIs(t, err, ErrCodexBindingAttributionConflict,
			"多归属并存时不得挑任意一个，必须明确失败")
	})
}
