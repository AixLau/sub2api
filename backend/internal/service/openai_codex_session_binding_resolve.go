package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// 把权威会话绑定模型接入归属判定，并把判定结果投影成出站身份。
//
// 门控：gateway.codex_identity.session_binding 默认为 legacy。默认状态下本文件
// 的函数**不会被调用**，既有解析器逐字节不变——部署本改动不会让现网进入新语义。
//
// 解析顺序严格是"先确认归属，再分配身份"：
//  1. 已提交绑定优先（current 缺失时由探测修复，绝不判为新会话）；
//  2. 未提交时按证据判定：原生 side 证据 → side（须解析出**真实** fork 源）；
//     带 parent 引用 → 继承父绑定；两者都不成立 → root。
//  3. 证据不足一律明确失败，绝不默认当普通会话——那正是漂移的成因。

func (s *OpenAIGatewayService) codexSessionBindingEnabled() bool {
	if s == nil || s.cfg == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(s.cfg.Gateway.CodexIdentity.SessionBinding), "binding")
}

func (s *OpenAIGatewayService) codexForceAccountRule() bool {
	if s == nil || s.cfg == nil {
		return false
	}
	return s.cfg.Gateway.CodexIdentity.ForceAccountRule
}

// codexSessionBindingDecision 是一次请求的归属判定结果。
type codexSessionBindingDecision struct {
	Attribution       codexSessionAttribution
	Generation        int64
	EntityRef         string
	IdentitySessionID string
	Rule              codexFingerprintMode
	ThreadID          string
	// Restored 表示判定来自已提交绑定，而不是本次新建。
	Restored bool
}

// resolveCodexSessionBindingDecision 完成归属判定。
//
// 返回 (nil, nil) 只用于"账号没有可用种子"这一种情况：没有稳定种子就没有可言的
// 身份，调用方按既有语义回退。其余异常一律返回错误，绝不静默降级。
func (s *OpenAIGatewayService) resolveCodexSessionBindingDecision(
	ctx context.Context, c *gin.Context, account *Account, input *codexSessionIdentityInput,
	userScope, accountScope string, observedAt time.Time,
) (*codexSessionBindingDecision, error) {
	if account == nil || input == nil || userScope == "" || accountScope == "" {
		return nil, ErrCodexBindingUnresolvedAttribution
	}
	store, err := s.codexHTTPIdentityStore()
	if err != nil {
		return nil, err
	}
	seed, ok := codexFingerprintSeed(account.Extra)
	if !ok {
		return nil, nil
	}
	rawSession := codexFirstIdentityValue(input.originalSessionID, input.sessionID)
	if rawSession == "" {
		return nil, ErrCodexBindingUnresolvedAttribution
	}
	scope := codexBindingScope(userScope, accountScope)
	period := resolveCodexSessionPeriod(seed, userScope, accountScope, observedAt)
	ctx = codexHTTPIdentityOwnershipContext(ctx, c, account, observedAt, false)

	accountRule := account.GetCodexFingerprintMode()

	// 1) 已提交绑定优先。current 缺失不等于新会话：resolveCodexSessionBindingCurrent
	//    会探测已提交的 binding 并修复指针，只有确实没有任何绑定才返回 nil。
	current, err := resolveCodexSessionBindingCurrent(ctx, store, scope, rawSession, period.epoch)
	if err != nil {
		return nil, err
	}
	if current != nil {
		// The current pointer is durable so side and flat bindings can outlive
		// ordinary periods, but a period binding is valid only for its fixed
		// epoch. Advance an old main binding before materializing it; otherwise
		// the binding model would silently keep one main upstream session forever.
		if current.Attribution == codexAttributionPeriod && current.Generation < period.epoch {
			// A child/reference request cannot bootstrap the new period from an
			// old parent. The root must establish the current-period binding first,
			// matching the legacy HTTP resolver's epoch boundary behavior.
			if input.parentReferencePresent || input.parentThreadID != "" || codexHTTPInputHasSideRootEvidence(input) {
				return nil, ErrCodexBindingUnresolvedAttribution
			}
			return s.commitCodexPeriodSessionBinding(ctx, store, scope, rawSession, input, period, observedAt)
		}
		return s.materializeCodexSessionBindingDecision(ctx, store, scope, rawSession, input,
			current.Attribution, current.Generation, current.EntityRef, true, observedAt)
	}

	// 2) 未提交：按证据判定归属。
	if codexHTTPInputHasSideRootEvidence(input) {
		// 先尝试**原值接管**既有 side：v3:side-session / v4:side-fork 是权威记录，
		// 必须原样沿用其 session id、已固定的 fork 源与 preserve_v3_threads，绝不
		// 重新分配或重算——否则 gate 一打开，在途 side 会话会被换成新身份甚至失败。
		projection, legacyErr := readCodexLegacySideProjection(ctx, store, userScope, accountScope, rawSession, input.forkedFromThreadID)
		if legacyErr != nil {
			return nil, legacyErr
		}
		if projection.SideSessionID != "" {
			if _, adopted, adoptErr := adoptCodexLegacySideEntity(ctx, store, scope, rawSession, projection,
				accountRule, observedAt.UnixMilli()); adoptErr != nil {
				return nil, adoptErr
			} else if adopted {
				if err := s.commitCodexBindingFor(ctx, store, scope, rawSession,
					codexAttributionSide, codexConstantGeneration, codexSideEntityKey(scope, rawSession), observedAt); err != nil {
					return nil, err
				}
				return s.materializeCodexSessionBindingDecision(ctx, store, scope, rawSession, input,
					codexAttributionSide, codexConstantGeneration, codexSideEntityKey(scope, rawSession), false, observedAt)
			}
		}
		forkSource, err := s.resolveCodexBindingForkSource(ctx, store, scope, input)
		if err != nil {
			return nil, err
		}
		_, sourceRule, err := readCodexSessionEntity(ctx, store, forkSource.EntityRef)
		if err != nil {
			return nil, err
		}
		// 规则继承真实 fork 源所在会话（决策：一条 lineage 内规则一致）。
		if _, err := getOrCreateCodexSideEntity(ctx, store, scope, rawSession, sourceRule,
			forkSource.ThreadID, false, observedAt.UnixMilli()); err != nil {
			return nil, err
		}
		if err := s.commitCodexBindingFor(ctx, store, scope, rawSession,
			codexAttributionSide, codexConstantGeneration, codexSideEntityKey(scope, rawSession), observedAt); err != nil {
			return nil, err
		}
		return s.materializeCodexSessionBindingDecision(ctx, store, scope, rawSession, input,
			codexAttributionSide, codexConstantGeneration, codexSideEntityKey(scope, rawSession), false, observedAt)
	}
	if input.parentReferencePresent || input.parentThreadID != "" {
		if strings.TrimSpace(input.parentThreadID) == "" {
			// 有 parent 引用却拿不到 parent 线程标识：证据不足。
			return nil, ErrCodexBindingUnresolvedAttribution
		}
		parent, parentErr := resolveCodexThreadForkSource(ctx, store, scope, input.parentThreadID)
		if parentErr != nil {
			if errors.Is(parentErr, ErrCodexBindingUnresolvedSource) {
				// 父线程没有任何已提交证据：证据不足，绝不自建普通会话。
				return nil, ErrCodexBindingUnresolvedAttribution
			}
			return nil, parentErr
		}
		// A normal parent from an expired period is historical evidence, not a
		// valid parent for the new main session. Side parents are independent and
		// intentionally remain valid across ordinary period rotation.
		if parent.Attribution == codexAttributionPeriod && parent.Generation != period.epoch {
			return nil, ErrCodexBindingUnresolvedAttribution
		}
		// 继承父绑定的归属、代次与实体——一条 lineage 内不换规则。
		if err := s.commitCodexBindingFor(ctx, store, scope, rawSession,
			parent.Attribution, parent.Generation, parent.EntityRef, observedAt); err != nil {
			return nil, err
		}
		return s.materializeCodexSessionBindingDecision(ctx, store, scope, rawSession, input,
			parent.Attribution, parent.Generation, parent.EntityRef, false, observedAt)
	}

	// 3) 无 parent、无 side 证据：root。归属类别由**当前模式**决定：
	//    session 有自然代次（epoch）；off/device/full 没有自然周期，恒为 flat:0，
	//    永不换代——把 device 账号建成 period 会让它在 epoch 推进时被换代，
	//    而 device 会话根本没有代次可言。
	if accountRule == codexFingerprintSession {
		return s.commitCodexPeriodSessionBinding(ctx, store, scope, rawSession, input, period, observedAt)
	}
	if _, err := getOrCreateCodexFlatEntity(ctx, store, scope, accountRule, observedAt.UnixMilli()); err != nil {
		return nil, err
	}
	if err := s.commitCodexBindingFor(ctx, store, scope, rawSession,
		codexAttributionFlat, codexConstantGeneration, codexFlatEntityKey(scope, accountRule), observedAt); err != nil {
		return nil, err
	}
	return s.materializeCodexSessionBindingDecision(ctx, store, scope, rawSession, input,
		codexAttributionFlat, codexConstantGeneration, codexFlatEntityKey(scope, accountRule), false, observedAt)
}

// resolveCodexBindingForkSource 解析 side 的**真实** fork 源。
//
// 只认 thread-exact 结论；latest 命中不算确定。缺证据时明确失败，绝不合成。
func (s *OpenAIGatewayService) resolveCodexBindingForkSource(
	ctx context.Context, store codexSessionIdentityStore, scope string, input *codexSessionIdentityInput,
) (codexThreadExact, error) {
	rawFork := strings.TrimSpace(input.forkedFromThreadID)
	if rawFork == "" {
		return codexThreadExact{}, ErrCodexBindingUnresolvedSource
	}
	return resolveCodexThreadForkSource(ctx, store, scope, rawFork)
}

func (s *OpenAIGatewayService) commitCodexBindingFor(
	ctx context.Context, store codexSessionIdentityStore, scope, rawSession string,
	attribution codexSessionAttribution, generation int64, entityRef string, observedAt time.Time,
) error {
	_, _, err := commitCodexSessionBinding(ctx, store, scope, rawSession, codexSessionBinding{
		Attribution: attribution, Generation: generation, EntityRef: entityRef,
		CommittedAtMs: observedAt.UnixMilli(),
	})
	return err
}

// commitCodexPeriodSessionBinding establishes the shared main session for one
// user/account period. Legacy period values are adopted before a new entity is
// allocated so enabling the binding model or advancing a period never creates
// a second upstream session for an already-known period.
func (s *OpenAIGatewayService) commitCodexPeriodSessionBinding(
	ctx context.Context, store codexSessionIdentityStore, scope, rawSession string,
	input *codexSessionIdentityInput, period codexSessionPeriod, observedAt time.Time,
) (*codexSessionBindingDecision, error) {
	if _, adopted, err := adoptLegacyCodexPeriodSessionEntity(ctx, store, scope, period.epoch,
		period.key, codexFingerprintSession, observedAt.UnixMilli()); err != nil {
		return nil, err
	} else if !adopted {
		if _, err := getOrCreateCodexPeriodSessionEntity(ctx, store, scope, period.epoch,
			codexFingerprintSession, observedAt.UnixMilli()); err != nil {
			return nil, err
		}
	}
	entityRef := codexPeriodSessionEntityKey(scope, period.epoch)
	if err := s.commitCodexBindingFor(ctx, store, scope, rawSession,
		codexAttributionPeriod, period.epoch, entityRef, observedAt); err != nil {
		return nil, err
	}
	return s.materializeCodexSessionBindingDecision(ctx, store, scope, rawSession, input,
		codexAttributionPeriod, period.epoch, entityRef, false, observedAt)
}

// materializeCodexSessionBindingDecision 把归属解析成具体身份，并登记 thread 两级记录。
//
// flat（off/device/full）不收敛 per-session 会话 id：IdentitySessionID 保持为空，
// 由调用方按各自模式的既有投影处理。
func (s *OpenAIGatewayService) materializeCodexSessionBindingDecision(
	ctx context.Context, store codexSessionIdentityStore, scope, rawSession string,
	input *codexSessionIdentityInput, attribution codexSessionAttribution, generation int64,
	entityRef string, restored bool, observedAt time.Time,
) (*codexSessionBindingDecision, error) {
	identitySessionID, rule, err := readCodexSessionEntity(ctx, store, entityRef)
	if err != nil {
		return nil, err
	}
	decision := &codexSessionBindingDecision{
		Attribution: attribution, Generation: generation, EntityRef: entityRef,
		IdentitySessionID: identitySessionID, Rule: rule, Restored: restored,
	}
	if identitySessionID == "" {
		// flat：没有会话身份可登记 thread。
		return decision, nil
	}
	task := codexFirstIdentityValue(input.threadID, input.originalSessionID, input.clientRequestID)
	if task == "" {
		return decision, nil
	}
	// thread 身份由 (session, raw thread) 确定性导出；先写权威 exact，再推进 latest 索引。
	threadID := codexHTTPSessionThreadID(identitySessionID, task)
	exact, err := registerCodexThreadExact(ctx, store, scope, task, codexThreadExact{
		SessionID: identitySessionID, ThreadID: threadID, EntityRef: entityRef,
		Attribution: attribution, Generation: generation,
	})
	if err != nil {
		return nil, err
	}
	if err := recordCodexThreadLatest(ctx, store, scope, task, codexThreadLatest{
		EntityRef: entityRef, Attribution: attribution, Generation: generation,
		ObservedAtMs: observedAt.UnixMilli(),
	}); err != nil {
		return nil, err
	}
	decision.ThreadID = exact.ThreadID
	return decision, nil
}

// projectCodexSessionFingerprintIDs 把判定结果投影成出站身份集合。
//
// 沿用既有派生函数（installation、thread、prompt cache），因此与 legacy 路径在
// 身份形态上保持一致；差别只在"session 从哪来"——这里是已提交绑定，而不是
// 客户端 session 或账号当前模式。
func (s *OpenAIGatewayService) projectCodexSessionFingerprintIDs(
	account *Account, input *codexSessionIdentityInput, decision *codexSessionBindingDecision,
	observedAt time.Time,
) *codexFingerprintIDs {
	if account == nil || decision == nil || decision.IdentitySessionID == "" {
		return nil
	}
	seed, ok := codexFingerprintSeed(account.Extra)
	if !ok {
		return nil
	}
	installationID := resolveConvergedInstallationID(account, seed)
	if installationID == "" {
		return nil
	}
	task := ""
	if input != nil {
		task = codexFirstIdentityValue(input.threadID, input.originalSessionID, input.clientRequestID)
	}
	threadID := decision.ThreadID
	if threadID == "" {
		threadID = codexHTTPSessionThreadID(decision.IdentitySessionID, task)
	}
	ids := &codexFingerprintIDs{
		accountID:           account.ID,
		mode:                codexFingerprintSession,
		httpSessionIdentity: true,
		seed:                seed,
		installationID:      installationID,
		sessionID:           decision.IdentitySessionID,
		threadID:            threadID,
		windowID:            threadID + ":0",
		turnStartedAtUnixMs: observedAt.UnixMilli(),
	}
	if input != nil {
		ids.parentTurnID = input.parentTurnID
		ids.rootTurnID = input.rootTurnID
		if input.turnStartedAtUnixMs != nil {
			ids.turnStartedAtUnixMs = *input.turnStartedAtUnixMs
		}
		ids.promptCacheKey = codexHTTPPromptCacheKey(decision.EntityRef, input, task)
	}
	turnID := ""
	if input != nil {
		turnID = strings.TrimSpace(input.turnID)
	}
	if turnID == "" {
		generated, err := newCodexTurnID()
		if err != nil {
			return nil
		}
		turnID = generated
	}
	ids.turnID = turnID
	return ids
}

func newCodexTurnID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("generate Codex turn identity: %w", err)
	}
	return id.String(), nil
}

// resolveCodexBindingFingerprintIDs 是绑定模式下的入口，与 legacy 入口同签名。
//
// 只有"能寻址到某条绑定"时才走新模型；缺任务身份或下游作用域时无法寻址任何
// 绑定，沿用既有保守 device 投影（与 legacy 行为一致），不把它当成归属问题。
func (s *OpenAIGatewayService) resolveCodexBindingFingerprintIDs(
	ctx context.Context, c *gin.Context, account *Account, now time.Time,
) (*codexFingerprintIDs, error) {
	var headers http.Header
	if c != nil && c.Request != nil {
		headers = c.Request.Header
	}
	input := stagedCodexSessionIdentityInput(c)
	if input == nil {
		return resolveCodexFingerprintIDs(account, "", codexFingerprintDevice), nil
	}
	userScope := codexSessionIdentityDownstreamScope(c, getAPIKeyIDFromContext(c))
	if userScope == "" {
		return resolveCodexFingerprintIDs(account, "", codexFingerprintDevice), nil
	}
	if codexFirstIdentityValue(input.originalSessionID, input.sessionID) == "" {
		// 请求没有任何可寻址的会话标识：无法定位任何绑定，沿用既有保守 device
		// 投影——与 legacy 一致，也与设计文档"缺少可识别用户或任务时保守使用
		// device 投影"一致。这不是归属证据不足，不应当成失败。
		return resolveCodexFingerprintIDs(account, "", codexFingerprintDevice), nil
	}
	accountScope := codexSessionIdentityUpstreamScope(account)
	decision, err := s.resolveCodexSessionBindingDecision(ctx, c, account, input, userScope, accountScope, now)
	if err != nil {
		return nil, err
	}
	if decision == nil {
		// 账号没有可用种子：没有稳定身份可言，按既有语义回退 device。
		return resolveCodexFingerprintIDs(account, "", codexFingerprintDevice), nil
	}
	if decision.Restored {
		RecordCodexIdentityEvent("session_binding", "restored")
	} else {
		RecordCodexIdentityEvent("session_binding", "created")
	}
	if decision.Rule == codexFingerprintOff {
		return nil, nil
	}
	if decision.Rule != codexFingerprintSession {
		// 非 session 规则沿用既有按模式的无状态投影，出站形态与 legacy 一致。
		return resolveCodexFingerprintIDs(account, extractClientSessionID(headers), decision.Rule), nil
	}
	return s.projectCodexSessionFingerprintIDs(account, input, decision, now), nil
}
