package service

import (
	"context"
	"log/slog"
	mathrand "math/rand"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

// OpenAI 粘性会话借道（spillover）与容量加权选号。
//
// 模型：每个会话有一个「主账号」（原有粘性绑定）和至多一个「备用账号」。
//
//   - 主账号有空闲槽位：始终用主账号（缓存连续性最好）。
//   - 主账号并发已满、同组有空闲账号：立即借道，不排队。优先借道到备用账号
//     （它已经服务过该会话，上游 Prompt Cache 是温热的）；没有备用账号或备用
//     账号也满时，才按容量加权选一个新账号并记为备用账号（一次冷启动）。
//   - 借道命中温热的备用账号时主备互换：会话的最近上下文已经在备用账号上，
//     原主账号仍是温热的回退目标。切换只会发生在这两个账号之间，且只在当前
//     主账号满载时发生，因此不会在多个账号间抖动，也不会产生新的冷启动。
//   - 全部账号都满：回到主账号的有界等待（StickySessionMaxWaiting /
//     StickySessionWaitTimeout），并按 WaitReselectInterval 周期性重新选号。
//
// 备用账号只是一个提示：每次使用前都要重新通过与普通候选账号完全相同的资格
// 校验（分组、模型、健康、限流、利润门等），因此不会绕过任何调度约束。

const openAISpilloverSessionKeyPrefix = "openai:spill:"

const defaultOpenAIStickySpilloverTTL = 10 * time.Minute

// OpenAIStickySpilloverMetricsSnapshot 借道相关的进程内累计计数。
type OpenAIStickySpilloverMetricsSnapshot struct {
	// WarmHitTotal 借道命中温热备用账号的次数（无缓存冷启动）。
	WarmHitTotal int64
	// ColdTotal 借道到新账号的次数（该请求在新账号上缓存冷启动）。
	ColdTotal int64
	// SwapTotal 主备互换次数。
	SwapTotal int64
	// PrimaryWaitTotal 全部账号已满、回到主账号排队的次数。
	PrimaryWaitTotal int64
}

var (
	openAIStickySpilloverWarmHitTotal     atomic.Int64
	openAIStickySpilloverColdTotal        atomic.Int64
	openAIStickySpilloverSwapTotal        atomic.Int64
	openAIStickySpilloverPrimaryWaitTotal atomic.Int64
)

// SnapshotOpenAIStickySpilloverMetrics 返回借道计数快照。
func SnapshotOpenAIStickySpilloverMetrics() OpenAIStickySpilloverMetricsSnapshot {
	return OpenAIStickySpilloverMetricsSnapshot{
		WarmHitTotal:     openAIStickySpilloverWarmHitTotal.Load(),
		ColdTotal:        openAIStickySpilloverColdTotal.Load(),
		SwapTotal:        openAIStickySpilloverSwapTotal.Load(),
		PrimaryWaitTotal: openAIStickySpilloverPrimaryWaitTotal.Load(),
	}
}

func (s *OpenAIGatewayService) openAIStickySpilloverEnabled() bool {
	return s != nil && s.cfg != nil && s.cache != nil && s.concurrencyService != nil &&
		s.cfg.Gateway.Scheduling.StickySessionSpilloverEnabled
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

// recordOpenAIStickySpillover 在借道请求拿到槽位后更新会话的主/备账号。
//
// primaryID 是借道前的主账号，previousSpillID 是选号前读到的备用账号（可为 0），
// selectedID 是本次实际拿到槽位的账号。allowRebind=false（利润门生效或要求
// 保留绑定）时只记录备用账号，不改写主绑定。
func (s *OpenAIGatewayService) recordOpenAIStickySpillover(
	ctx context.Context,
	groupID *int64,
	sessionHash string,
	primaryID int64,
	previousSpillID int64,
	selectedID int64,
	allowRebind bool,
) {
	if s == nil || strings.TrimSpace(sessionHash) == "" || selectedID <= 0 || selectedID == primaryID {
		return
	}
	warm := previousSpillID > 0 && previousSpillID == selectedID
	swapped := false
	if warm && allowRebind && primaryID > 0 {
		// 主备互换：两个账号都服务过该会话，互换不会产生冷启动。先写主绑定，
		// 中途失败最多留下「备用 == 主」的无效提示，读取时会被忽略。
		if err := s.setStickySessionAccountID(ctx, groupID, sessionHash, selectedID, openaiStickySessionTTL); err == nil {
			s.setOpenAISpilloverAccountID(ctx, groupID, sessionHash, primaryID)
			swapped = true
		}
	}
	if !swapped {
		s.setOpenAISpilloverAccountID(ctx, groupID, sessionHash, selectedID)
	}
	if warm {
		openAIStickySpilloverWarmHitTotal.Add(1)
	} else {
		openAIStickySpilloverColdTotal.Add(1)
	}
	if swapped {
		openAIStickySpilloverSwapTotal.Add(1)
	}
	slog.Info("openai.sticky_spillover",
		"group_id", derefGroupID(groupID),
		"primary_account_id", primaryID,
		"selected_account_id", selectedID,
		"warm", warm,
		"swapped", swapped,
	)
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
