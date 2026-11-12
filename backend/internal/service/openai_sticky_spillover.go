package service

import (
	"context"
	"log/slog"
	mathrand "math/rand"
	"sort"
	"strings"
	"sync"
	"time"
)

// OpenAI 粘性会话借道（spillover）与容量加权选号。
//
// 模型：每个会话有一个「主账号」（原有粘性绑定）和至多一个「备用账号」。
//
//   - 主账号有空闲槽位：始终用主账号（缓存连续性最好）。
//   - 主账号并发已满、同组有空闲账号：立即借道，不排队。优先借道到备用账号
//     （它在 TTL 内服务过该会话，上游 Prompt Cache 可能仍在）；没有备用账号或
//     备用账号也满时，才按容量加权选一个新账号并记为备用账号。
//   - 借道复用备用账号时主备互换：会话的最近上下文已经在备用账号上，原主账号
//     作为回退目标。切换只会发生在这两个账号之间，且只在当前主账号满载时发生，
//     因此不会在多个账号间抖动。
//
// 注意「复用备用账号」只是账号层面的复用，不等于上游 Prompt Cache 命中：备用
// 关联的 TTL 是会话空闲过期（每次会话被服务都会续期），而上游缓存是否仍在取决于
// 该账号上次服务此会话的时间和上游的保留策略，两者是独立的生命周期。真实命中率以上游返回的 cached_tokens 为准，见
// openai_sticky_spillover_metrics.go 中按路由统计的 input/cached token。
//   - 全部账号都满：回到主账号的有界等待（StickySessionMaxWaiting /
//     StickySessionWaitTimeout），并按 WaitReselectInterval 周期性重新选号。
//
// 主/备绑定的写入是原子的比较并更新（见 updateOpenAIStickySpilloverBinding）。
//
// 备用账号只是一个提示：每次使用前都要重新通过与普通候选账号完全相同的资格
// 校验（分组、模型、健康、限流、利润门等），因此不会绕过任何调度约束。

const openAISpilloverSessionKeyPrefix = "openai:spill:"

const defaultOpenAIStickySpilloverTTL = 10 * time.Minute

// openAIStickySpilloverEnabled 报告该分组是否启用粘性借道。
// sticky_session_spillover_group_ids 非空时只对列出的分组生效（灰度），为空表示全部分组。
func (s *OpenAIGatewayService) openAIStickySpilloverEnabled(groupID *int64) bool {
	if s == nil || s.cfg == nil || s.cache == nil || s.concurrencyService == nil ||
		!s.cfg.Gateway.Scheduling.StickySessionSpilloverEnabled {
		return false
	}
	allowed := s.cfg.Gateway.Scheduling.StickySessionSpilloverGroupIDs
	if len(allowed) == 0 {
		return true
	}
	target := derefGroupID(groupID)
	for _, id := range allowed {
		if id == target {
			return true
		}
	}
	return false
}

func (s *OpenAIGatewayService) openAICapacityWeightedSelectionEnabled() bool {
	return s != nil && s.cfg != nil && s.cfg.Gateway.Scheduling.CapacityWeightedSelection
}

func (s *OpenAIGatewayService) openAIStickySpilloverTTL() time.Duration {
	if s != nil && s.cfg != nil && s.cfg.Gateway.Scheduling.StickySessionSpilloverTTL > 0 {
		return s.cfg.Gateway.Scheduling.StickySessionSpilloverTTL
	}
	return defaultOpenAIStickySpilloverTTL
}

// openAIWaitReselectInterval 返回排队期间的重选间隔；0 表示不重选。
func (s *OpenAIGatewayService) openAIWaitReselectInterval() time.Duration {
	if s == nil || s.cfg == nil || s.cfg.Gateway.Scheduling.WaitReselectInterval <= 0 {
		return 0
	}
	return s.cfg.Gateway.Scheduling.WaitReselectInterval
}

func openAISpilloverSessionKey(sessionHash string) string {
	normalized := strings.TrimSpace(sessionHash)
	if normalized == "" {
		return ""
	}
	return openAISpilloverSessionKeyPrefix + normalized
}

// getOpenAISpilloverAccountID 读取会话的备用账号；没有或出错时返回 0。
// 键按分组隔离（与主绑定一致），不同分组/会话之间不会共享。
func (s *OpenAIGatewayService) getOpenAISpilloverAccountID(ctx context.Context, groupID *int64, sessionHash string) int64 {
	if s == nil || s.cache == nil {
		return 0
	}
	key := openAISpilloverSessionKey(sessionHash)
	if key == "" {
		return 0
	}
	accountID, err := s.cache.GetSessionAccountID(ctx, derefGroupID(groupID), key)
	if err != nil || accountID <= 0 {
		return 0
	}
	return accountID
}

// refreshOpenAISpilloverTTL 在会话被主账号正常服务时续期备用账号关联。
//
// 备用关联的 TTL 是「会话空闲过期」：只要会话仍在活动，关联就保留，持续活跃的
// 会话不会因为固定时长到期而在下一次溢出时被换到第三个账号。它与上游 Prompt
// Cache 的生命周期无关——后者取决于备用账号上次服务该会话的时间，只能通过上游
// 返回的 cached_tokens 观察。
func (s *OpenAIGatewayService) refreshOpenAISpilloverTTL(ctx context.Context, groupID *int64, sessionHash string) {
	if !s.openAIStickySpilloverEnabled(groupID) {
		return
	}
	key := openAISpilloverSessionKey(sessionHash)
	if key == "" {
		return
	}
	_ = s.cache.RefreshSessionTTL(ctx, derefGroupID(groupID), key, s.openAIStickySpilloverTTL())
}

func (s *OpenAIGatewayService) setOpenAISpilloverAccountID(ctx context.Context, groupID *int64, sessionHash string, accountID int64) {
	if s == nil || s.cache == nil || accountID <= 0 {
		return
	}
	key := openAISpilloverSessionKey(sessionHash)
	if key == "" {
		return
	}
	_ = s.cache.SetSessionAccountID(ctx, derefGroupID(groupID), key, accountID, s.openAIStickySpilloverTTL())
}

// OpenAIStickySpilloverBindingOutcome 是一次主/备绑定更新的结果。
type OpenAIStickySpilloverBindingOutcome int

const (
	// OpenAIStickySpilloverBindingConflict：绑定在选号期间已被其他请求改写，本次不写入。
	OpenAIStickySpilloverBindingConflict OpenAIStickySpilloverBindingOutcome = iota
	// OpenAIStickySpilloverBindingRecorded：已把本次账号记为备用账号（主绑定不变）。
	OpenAIStickySpilloverBindingRecorded
	// OpenAIStickySpilloverBindingSwapped：主备已互换。
	OpenAIStickySpilloverBindingSwapped
)

// OpenAIStickySpilloverBindingUpdate 描述一次「比较并更新」：只有当缓存里的主/备
// 绑定仍与选号时读到的一致，才写入新状态。
type OpenAIStickySpilloverBindingUpdate struct {
	PrimaryKey   string
	SecondaryKey string
	// ExpectedPrimaryID / ExpectedSecondaryID 是选号时读到的主/备账号（备用可为 0）。
	ExpectedPrimaryID   int64
	ExpectedSecondaryID int64
	// SelectedID 是本次实际拿到槽位的账号。
	SelectedID int64
	// Swap 为 true 时把 SelectedID 提升为主账号、原主账号降为备用账号。
	Swap         bool
	PrimaryTTL   time.Duration
	SecondaryTTL time.Duration
}

// OpenAIStickySpilloverBinder 是 GatewayCache 的可选能力：在一次原子操作里完成
// 主/备绑定的比较并更新。Redis 实现用 Lua 脚本；未实现该接口的缓存（测试桩）
// 退回进程内加锁的等价逻辑。
type OpenAIStickySpilloverBinder interface {
	UpdateStickySpilloverBinding(ctx context.Context, groupID int64, update OpenAIStickySpilloverBindingUpdate) (OpenAIStickySpilloverBindingOutcome, error)
}

// openAIStickySpilloverFallbackMu 只保护非原子缓存实现上的读-改-写回退路径。
var openAIStickySpilloverFallbackMu sync.Mutex

// updateOpenAIStickySpilloverBinding 原子地更新主/备绑定。规则（与 Redis 脚本一致）：
//   - 主绑定已被改成别的账号 → 冲突，不写；
//   - 备用绑定已被别的请求改成第三个账号 → 冲突，不写；
//   - 否则 Swap 且主绑定仍在 → 主备互换；其余情况只把 SelectedID 记为备用。
func (s *OpenAIGatewayService) updateOpenAIStickySpilloverBinding(ctx context.Context, groupID int64, update OpenAIStickySpilloverBindingUpdate) (OpenAIStickySpilloverBindingOutcome, error) {
	if binder, ok := s.cache.(OpenAIStickySpilloverBinder); ok {
		return binder.UpdateStickySpilloverBinding(ctx, groupID, update)
	}
	openAIStickySpilloverFallbackMu.Lock()
	defer openAIStickySpilloverFallbackMu.Unlock()
	currentPrimary, err := s.cache.GetSessionAccountID(ctx, groupID, update.PrimaryKey)
	if err != nil {
		currentPrimary = 0
	}
	if currentPrimary > 0 && currentPrimary != update.ExpectedPrimaryID {
		return OpenAIStickySpilloverBindingConflict, nil
	}
	currentSecondary, err := s.cache.GetSessionAccountID(ctx, groupID, update.SecondaryKey)
	if err != nil || currentSecondary == update.ExpectedPrimaryID {
		currentSecondary = 0
	}
	if currentSecondary > 0 && currentSecondary != update.ExpectedSecondaryID && currentSecondary != update.SelectedID {
		return OpenAIStickySpilloverBindingConflict, nil
	}
	if update.Swap && currentPrimary > 0 {
		if err := s.cache.SetSessionAccountID(ctx, groupID, update.PrimaryKey, update.SelectedID, update.PrimaryTTL); err != nil {
			return OpenAIStickySpilloverBindingConflict, err
		}
		if err := s.cache.SetSessionAccountID(ctx, groupID, update.SecondaryKey, update.ExpectedPrimaryID, update.SecondaryTTL); err != nil {
			return OpenAIStickySpilloverBindingSwapped, err
		}
		return OpenAIStickySpilloverBindingSwapped, nil
	}
	if err := s.cache.SetSessionAccountID(ctx, groupID, update.SecondaryKey, update.SelectedID, update.SecondaryTTL); err != nil {
		return OpenAIStickySpilloverBindingConflict, err
	}
	return OpenAIStickySpilloverBindingRecorded, nil
}

// recordOpenAIStickySpillover 在借道请求拿到槽位后更新会话的主/备账号，并返回本次
// 请求的调度路由（复用备用账号 / 借道到新账号）。
//
// primaryID 是借道前的主账号，previousSpillID 是选号前读到的备用账号（可为 0），
// selectedID 是本次实际拿到槽位的账号。allowRebind=false（利润门生效或要求
// 保留绑定）时只记录备用账号，不改写主绑定。primaryTTL 是互换时写主绑定的 TTL。
//
// 同一会话的并发借道可能落到不同账号：请求本身照常用各自拿到的账号服务，但绑定
// 只接受与当前缓存状态一致的那次更新（先到先得），其余记为冲突并丢弃，保证主/备
// 始终是一对自洽的账号，不会被过期视图覆盖成第三个账号。
func (s *OpenAIGatewayService) recordOpenAIStickySpillover(
	ctx context.Context,
	groupID *int64,
	sessionHash string,
	primaryID int64,
	previousSpillID int64,
	selectedID int64,
	allowRebind bool,
	primaryTTL time.Duration,
) string {
	if s == nil || s.cache == nil || strings.TrimSpace(sessionHash) == "" || selectedID <= 0 || selectedID == primaryID {
		return ""
	}
	secondaryReused := previousSpillID > 0 && previousSpillID == selectedID
	route := OpenAISchedulingRouteSpillNew
	if secondaryReused {
		route = OpenAISchedulingRouteSpillSecondary
	}
	if primaryTTL <= 0 {
		primaryTTL = openaiStickySessionTTL
	}
	outcome, err := s.updateOpenAIStickySpilloverBinding(ctx, derefGroupID(groupID), OpenAIStickySpilloverBindingUpdate{
		PrimaryKey:          s.openAISessionCacheKey(sessionHash),
		SecondaryKey:        openAISpilloverSessionKey(sessionHash),
		ExpectedPrimaryID:   primaryID,
		ExpectedSecondaryID: previousSpillID,
		SelectedID:          selectedID,
		Swap:                secondaryReused && allowRebind && primaryID > 0,
		PrimaryTTL:          primaryTTL,
		SecondaryTTL:        s.openAIStickySpilloverTTL(),
	})
	swapped := err == nil && outcome == OpenAIStickySpilloverBindingSwapped
	if swapped && s.openAISessionHashDualWriteOldEnabled() {
		// 与 setStickySessionAccountID 一致：迁移期旧 key 跟随主绑定（尽力而为）。
		if legacyKey := s.openAILegacySessionCacheKey(ctx, sessionHash); legacyKey != "" {
			_ = s.cache.SetSessionAccountID(ctx, derefGroupID(groupID), legacyKey, selectedID, s.openAIStickyLegacyTTL(primaryTTL))
		}
	}
	conflict := err == nil && outcome == OpenAIStickySpilloverBindingConflict
	recordOpenAIStickySpilloverEvent(secondaryReused, swapped, conflict)
	slog.Info("openai.sticky_spillover",
		"group_id", derefGroupID(groupID),
		"primary_account_id", primaryID,
		"selected_account_id", selectedID,
		"secondary_reused", secondaryReused,
		"swapped", swapped,
		"binding_conflict", conflict,
	)
	return route
}

// openAIAccountFreeSlots 估算账号的空闲槽位数（并发上限 - 在途 - 排队）。
func openAIAccountFreeSlots(account *Account, loadInfo *AccountLoadInfo) int {
	if account == nil {
		return 0
	}
	capacity := account.EffectiveLoadFactor()
	if loadInfo == nil {
		return capacity
	}
	free := capacity - loadInfo.CurrentConcurrency - loadInfo.WaitingCount
	if free < 0 {
		return 0
	}
	return free
}

// orderOpenAIAccountsByFreeCapacity 把候选重排为：优先级升序，同优先级内按
// 「空闲槽位数」做不放回加权随机。
//
// 原有的「负载率 → LRU」排序只看瞬时在途请求：低峰期所有账号负载率都是 0，
// 新会话按 LRU 均匀轮转，结果 5 并发与 50 并发的账号绑定了同样多的会话，小
// 账号在会话同时活跃时必然排队。按空闲槽位加权后，会话数量的期望与账号容量
// 成正比，同时满载账号的权重自然趋近于 0。
func orderOpenAIAccountsByFreeCapacity(available []accountWithLoad, randFloat func() float64) {
	if len(available) <= 1 {
		return
	}
	if randFloat == nil {
		randFloat = mathrand.Float64
	}
	sort.SliceStable(available, func(i, j int) bool {
		return available[i].account.Priority < available[j].account.Priority
	})
	for start := 0; start < len(available); {
		end := start + 1
		for end < len(available) && available[end].account.Priority == available[start].account.Priority {
			end++
		}
		tier := available[start:end]
		weights := make([]float64, len(tier))
		for i, item := range tier {
			weights[i] = openAIFreeCapacityWeight(openAIAccountFreeSlots(item.account, item.loadInfo))
		}
		for i := 0; i < len(tier)-1; i++ {
			total := 0.0
			for _, w := range weights[i:] {
				total += w
			}
			pick := i
			r := randFloat() * total
			acc := 0.0
			for j := i; j < len(tier); j++ {
				acc += weights[j]
				if r <= acc {
					pick = j
					break
				}
			}
			tier[i], tier[pick] = tier[pick], tier[i]
			weights[i], weights[pick] = weights[pick], weights[i]
		}
		start = end
	}
}

// openAIFreeCapacityWeight 把空闲槽位数转换为采样权重；满载账号保留一个极小
// 权重，保证它仍在候选顺序里（负载快照可能略旧，真实抢槽才是最终裁决）。
func openAIFreeCapacityWeight(freeSlots int) float64 {
	if freeSlots <= 0 {
		return 0.01
	}
	return float64(freeSlots)
}

// moveOpenAIAccountToFront 把指定账号稳定地移到候选顺序最前；不在列表中时不变。
func moveOpenAIAccountToFront(order []accountWithLoad, accountID int64) {
	if accountID <= 0 {
		return
	}
	for i := range order {
		if order[i].account != nil && order[i].account.ID == accountID {
			item := order[i]
			copy(order[1:i+1], order[:i])
			order[0] = item
			return
		}
	}
}

// moveOpenAIAccountPointerToFront 是 moveOpenAIAccountToFront 的 []*Account 版本。
func moveOpenAIAccountPointerToFront(order []*Account, accountID int64) {
	if accountID <= 0 {
		return
	}
	for i := range order {
		if order[i] != nil && order[i].ID == accountID {
			item := order[i]
			copy(order[1:i+1], order[:i])
			order[0] = item
			return
		}
	}
}
