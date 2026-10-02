package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// Codex 会话绑定模型（阶段 A 原型）。
//
// 本文件只提供数据模型与纯解析逻辑，**不接入转发主路径**：没有任何既有函数
// 调用这里的符号，既有转发行为不变。阶段 B 才把它接到 resolveCodexHTTPSession。
//
// 模型把三件事分开，因为此前把它们挤在一个"模式戳"里是漂移的根源：
//
//	A. 身份实体：持有 session UUID 与规则。period 是**共享**的（同一
//	   user+account+epoch 下所有 raw session 共用同一个上游 session id），
//	   side 是 per-raw-session 的。
//	B. 归属绑定：按 raw session、代次进键、**不可变**。它只引用 A，不复制身份。
//	C. thread 两级寻址：thread-exact 是权威且不可变，thread-latest 只是候选线索。
//
// 三条不变量在下面的实现里必须成立，任何简化都视为回归：
//
//  1. latest 命中 ≠ 来源代次已确定（必须由 exact 确认）。
//  2. current 缺失 ≠ 新会话（必须探测 binding，可能只是提交中断）。
//  3. 单 key SETNX ≠ 整个关系已原子提交（写序：先 binding，后 current）。
const codexSessionBindingVersion = 1

type codexSessionAttribution string

const (
	// period 有自然代次（period epoch），换代时建立新绑定。
	codexAttributionPeriod codexSessionAttribution = "period"
	// flat 覆盖 off/device/full：没有自然周期，代次恒为 0，永不换代。
	codexAttributionFlat codexSessionAttribution = "flat"
	// side 建立即固定，代次恒为 0，永不换代。
	codexAttributionSide codexSessionAttribution = "side"
)

// flat/side 的恒定代次。它们不假装拥有自然周期。
const codexConstantGeneration int64 = 0

var (
	// ErrCodexBindingUnresolvedAttribution：归属证据不足。调用方必须明确失败，
	// 绝不能默认当作 period——那正是"child 先到先归普通会话、root 到了再漂移"的成因。
	ErrCodexBindingUnresolvedAttribution = errors.New("Codex session attribution evidence is insufficient")
	// ErrCodexBindingUnresolvedSource：fork 源未被确证。latest 命中不算确证。
	ErrCodexBindingUnresolvedSource = errors.New("Codex fork source is not resolved")
	// ErrCodexBindingRecordAnomaly：记录之间自相矛盾（例如 current 指向不存在的 binding）。
	ErrCodexBindingRecordAnomaly = errors.New("Codex session binding record is inconsistent")
	// ErrCodexBindingUnsupportedVersion：记录版本不被本版本支持。不得静默降级采用账号模式。
	ErrCodexBindingUnsupportedVersion = errors.New("Codex session binding record version is not supported")
	// ErrCodexBindingInvalidValue：记录内容无法解析。
	ErrCodexBindingInvalidValue = errors.New("Codex session binding record value is invalid")
	// ErrCodexBindingAttributionConflict：同一 raw session 上出现了不同归属。
	// 归属一经提交即固定，绝不允许静默改归属——那正是"先归普通会话、root 到了
	// 再漂到 side"的成因，必须明确失败。
	ErrCodexBindingAttributionConflict = errors.New("Codex session attribution conflicts with the committed binding")
)

// ---------------------------------------------------------------------------
// 记录定义
// ---------------------------------------------------------------------------

// codexPeriodSessionEntity 是共享的身份实体：同一 (scope, generation) 下所有
// raw session 共用同一个 IdentitySessionID，因此规则也只能放在这里——同一上游
// session id 下出现两套投影会自相矛盾。
type codexPeriodSessionEntity struct {
	Version           int                  `json:"v"`
	IdentitySessionID string               `json:"identity_session_id"`
	Rule              codexFingerprintMode `json:"rule"`
	CreatedAtMs       int64                `json:"created_at_ms"`
}

// codexSideEntity 是 per-raw-session 的身份实体。ForkSource 只记录**真实**来源，
// 缺失时保持为空——合成一个 f(sideSession, rawFork) 不是恢复，是伪造关系。
type codexSideEntity struct {
	Version           int                  `json:"v"`
	IdentitySessionID string               `json:"identity_session_id"`
	Rule              codexFingerprintMode `json:"rule"`
	ForkSource        string               `json:"fork_source,omitempty"`
	PreserveV3Threads bool                 `json:"preserve_v3_threads"`
	Adopted           bool                 `json:"adopted"`
	CreatedAtMs       int64                `json:"created_at_ms"`
}

// codexSessionCurrent 是按 raw session 的可变寻址指针。只有它会被改写，且只在
// 代次推进时改写；period 换代建新 binding，旧 binding 永不改写。
type codexSessionCurrent struct {
	Version     int                     `json:"v"`
	Attribution codexSessionAttribution `json:"attribution"`
	Generation  int64                   `json:"generation"`
	EntityRef   string                  `json:"entity_ref"`
}

// codexSessionBinding 是每代次一条的**不可变**归属提交记录。
type codexSessionBinding struct {
	Version       int                     `json:"v"`
	Attribution   codexSessionAttribution `json:"attribution"`
	Generation    int64                   `json:"generation"`
	EntityRef     string                  `json:"entity_ref"`
	CommittedAtMs int64                   `json:"committed_at_ms"`
}

// codexThreadExact 是权威的 thread 归属：该 raw thread **在该实体中**被确定的身份。
// 不可变。fork 源解析与 child→parent 继承都只认它。
type codexThreadExact struct {
	Version     int                     `json:"v"`
	SessionID   string                  `json:"session_id"`
	ThreadID    string                  `json:"thread_id"`
	EntityRef   string                  `json:"entity_ref"`
	Attribution codexSessionAttribution `json:"attribution"`
	Generation  int64                   `json:"generation"`
}

// codexThreadLatest 只是"最近一次所在实体"的候选线索（latest-wins + CAS）。
// 命中它**不**代表来源代次已确定。
type codexThreadLatest struct {
	Version      int                     `json:"v"`
	EntityRef    string                  `json:"entity_ref"`
	Attribution  codexSessionAttribution `json:"attribution"`
	Generation   int64                   `json:"generation"`
	ObservedAtMs int64                   `json:"observed_at_ms"`
}

// ---------------------------------------------------------------------------
// 键构造
// ---------------------------------------------------------------------------

// codexBindingDigest 与既有 codexHTTPIdentityMappingKey/codexHTTPThreadKey 同风格：
// 把分量 JSON 编码后 sha256，raw 标识不出现在键名里。
func codexBindingDigest(prefix string, parts ...string) string {
	canonical, _ := json.Marshal(parts)
	digest := sha256.Sum256(canonical)
	return fmt.Sprintf("%s:%x", prefix, digest[:])
}

// codexBindingScope 把下游与上游作用域合成一个哈希分量。
func codexBindingScope(userScope, accountScope string) string {
	canonical, _ := json.Marshal([]string{userScope, accountScope})
	digest := sha256.Sum256(canonical)
	return fmt.Sprintf("%x", digest[:])
}

func codexPeriodSessionEntityKey(scope string, generation int64) string {
	return codexBindingDigest(fmt.Sprintf("v4:period-session:%d", generation), scope)
}

func codexSideEntityKey(scope, rawSession string) string {
	return codexBindingDigest("v4:side-session:v2", scope, rawSession)
}

func codexSessionCurrentKey(scope, rawSession string) string {
	return codexBindingDigest("v4:session-current", scope, rawSession)
}

func codexSessionBindingKey(attribution codexSessionAttribution, generation int64, scope, rawSession string) string {
	return codexBindingDigest(fmt.Sprintf("v4:session-binding:%s:%d", attribution, generation), scope, rawSession)
}

func codexThreadExactKey(scope, rawThread, entityRef string) string {
	return codexBindingDigest("v4:thread-exact", scope, rawThread, entityRef)
}

func codexThreadLatestKey(scope, rawThread string) string {
	return codexBindingDigest("v4:thread-latest", scope, rawThread)
}

// ---------------------------------------------------------------------------
// 编解码（版本校验是必须的：不支持的版本要明确失败，不得静默降级）
// ---------------------------------------------------------------------------

func encodeCodexBindingRecord(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode Codex session binding record: %w", err)
	}
	return string(encoded), nil
}

func decodeCodexPeriodSessionEntity(raw string) (codexPeriodSessionEntity, error) {
	var entity codexPeriodSessionEntity
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &entity); err != nil {
		return entity, ErrCodexBindingInvalidValue
	}
	if entity.Version != codexSessionBindingVersion {
		return entity, ErrCodexBindingUnsupportedVersion
	}
	if !isCodexUUIDv7(entity.IdentitySessionID) {
		return entity, ErrCodexBindingInvalidValue
	}
	return entity, nil
}

func decodeCodexSideEntity(raw string) (codexSideEntity, error) {
	var entity codexSideEntity
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &entity); err != nil {
		return entity, ErrCodexBindingInvalidValue
	}
	if entity.Version != codexSessionBindingVersion {
		return entity, ErrCodexBindingUnsupportedVersion
	}
	if !isCodexUUIDv7(entity.IdentitySessionID) {
		return entity, ErrCodexBindingInvalidValue
	}
	return entity, nil
}

func decodeCodexSessionCurrent(raw string) (codexSessionCurrent, error) {
	var current codexSessionCurrent
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &current); err != nil {
		return current, ErrCodexBindingInvalidValue
	}
	if current.Version != codexSessionBindingVersion {
		return current, ErrCodexBindingUnsupportedVersion
	}
	if !validCodexAttribution(current.Attribution) || current.EntityRef == "" {
		return current, ErrCodexBindingInvalidValue
	}
	return current, nil
}

func decodeCodexSessionBinding(raw string) (codexSessionBinding, error) {
	var binding codexSessionBinding
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &binding); err != nil {
		return binding, ErrCodexBindingInvalidValue
	}
	if binding.Version != codexSessionBindingVersion {
		return binding, ErrCodexBindingUnsupportedVersion
	}
	if !validCodexAttribution(binding.Attribution) || binding.EntityRef == "" {
		return binding, ErrCodexBindingInvalidValue
	}
	return binding, nil
}

func decodeCodexThreadExact(raw string) (codexThreadExact, error) {
	var exact codexThreadExact
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &exact); err != nil {
		return exact, ErrCodexBindingInvalidValue
	}
	if exact.Version != codexSessionBindingVersion {
		return exact, ErrCodexBindingUnsupportedVersion
	}
	if !isCodexUUID(exact.SessionID) || !isCodexUUID(exact.ThreadID) || exact.EntityRef == "" {
		return exact, ErrCodexBindingInvalidValue
	}
	return exact, nil
}

func decodeCodexThreadLatest(raw string) (codexThreadLatest, error) {
	var latest codexThreadLatest
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &latest); err != nil {
		return latest, ErrCodexBindingInvalidValue
	}
	if latest.Version != codexSessionBindingVersion {
		return latest, ErrCodexBindingUnsupportedVersion
	}
	if !validCodexAttribution(latest.Attribution) || latest.EntityRef == "" {
		return latest, ErrCodexBindingInvalidValue
	}
	return latest, nil
}

func validCodexAttribution(value codexSessionAttribution) bool {
	switch value {
	case codexAttributionPeriod, codexAttributionFlat, codexAttributionSide:
		return true
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// 身份实体：period 共享分配 / side per-raw-session
// ---------------------------------------------------------------------------

// getOrCreateCodexPeriodSessionEntity 用 SETNX 分配共享 period 实体。
//
// "共享"是这里的核心语义：键只含 (scope, generation)，不含 raw session，因此
// 同一 user+account+epoch 下的所有 raw session 收敛到同一个 IdentitySessionID。
// 并发调用只会有一个赢家，其余读到赢家的值。
func getOrCreateCodexPeriodSessionEntity(
	ctx context.Context, store codexSessionIdentityStore,
	scope string, generation int64, rule codexFingerprintMode, nowMs int64,
) (codexPeriodSessionEntity, error) {
	key := codexPeriodSessionEntityKey(scope, generation)
	if raw, err := store.GetCodexSessionIdentity(ctx, key); err == nil {
		return decodeCodexPeriodSessionEntity(raw)
	} else if !errors.Is(err, ErrCodexSessionIdentityNotFound) {
		return codexPeriodSessionEntity{}, fmt.Errorf("read Codex period session entity: %w", err)
	}
	sessionID, err := uuid.NewV7()
	if err != nil {
		return codexPeriodSessionEntity{}, fmt.Errorf("generate Codex period session identity: %w", err)
	}
	candidate := codexPeriodSessionEntity{
		Version:           codexSessionBindingVersion,
		IdentitySessionID: sessionID.String(),
		Rule:              rule,
		CreatedAtMs:       nowMs,
	}
	encoded, err := encodeCodexBindingRecord(candidate)
	if err != nil {
		return codexPeriodSessionEntity{}, err
	}
	created, err := store.SetCodexSessionIdentityIfAbsent(ctx, key, encoded, 0)
	if err != nil {
		return codexPeriodSessionEntity{}, fmt.Errorf("create Codex period session entity: %w", err)
	}
	if created {
		return candidate, nil
	}
	// 输家必须读回赢家的值，否则同一 period 会出现两个 session id。
	raw, err := store.GetCodexSessionIdentity(ctx, key)
	if err != nil {
		return codexPeriodSessionEntity{}, fmt.Errorf("read Codex period session entity: %w", err)
	}
	return decodeCodexPeriodSessionEntity(raw)
}

// getOrCreateCodexSideEntity 分配 per-raw-session 的 side 实体。
// forkSource 为空表示来源尚未确证——此时**不写入**，由调用方明确失败（不变量：不合成来源）。
func getOrCreateCodexSideEntity(
	ctx context.Context, store codexSessionIdentityStore,
	scope, rawSession string, rule codexFingerprintMode, forkSource string,
	preserveV3Threads bool, nowMs int64,
) (codexSideEntity, error) {
	key := codexSideEntityKey(scope, rawSession)
	if raw, err := store.GetCodexSessionIdentity(ctx, key); err == nil {
		return decodeCodexSideEntity(raw)
	} else if !errors.Is(err, ErrCodexSessionIdentityNotFound) {
		return codexSideEntity{}, fmt.Errorf("read Codex side entity: %w", err)
	}
	if strings.TrimSpace(forkSource) == "" {
		return codexSideEntity{}, ErrCodexBindingUnresolvedSource
	}
	if !isCodexUUID(forkSource) {
		return codexSideEntity{}, ErrCodexBindingInvalidValue
	}
	sessionID, err := uuid.NewV7()
	if err != nil {
		return codexSideEntity{}, fmt.Errorf("generate Codex side identity: %w", err)
	}
	candidate := codexSideEntity{
		Version:           codexSessionBindingVersion,
		IdentitySessionID: sessionID.String(),
		Rule:              rule,
		ForkSource:        strings.TrimSpace(forkSource),
		PreserveV3Threads: preserveV3Threads,
		CreatedAtMs:       nowMs,
	}
	encoded, err := encodeCodexBindingRecord(candidate)
	if err != nil {
		return codexSideEntity{}, err
	}
	created, err := store.SetCodexSessionIdentityIfAbsent(ctx, key, encoded, 0)
	if err != nil {
		return codexSideEntity{}, fmt.Errorf("create Codex side entity: %w", err)
	}
	if created {
		return candidate, nil
	}
	raw, err := store.GetCodexSessionIdentity(ctx, key)
	if err != nil {
		return codexSideEntity{}, fmt.Errorf("read Codex side entity: %w", err)
	}
	return decodeCodexSideEntity(raw)
}

// codexFlatEntity 是 off/device/full 的规则载体。
//
// 这三类模式**没有自然周期**，因此代次恒为 0、永不换代；它们也不收敛
// per-session 的上游会话 id（device/off 只收敛 installation_id，full 的收敛
// 仍在请求侧按各自规则推导），所以这里不持有 IdentitySessionID——身份投影不
// 因本记录而改变，本记录只负责把"这条会话用哪种规则"钉死。
type codexFlatEntity struct {
	Version     int                  `json:"v"`
	Rule        codexFingerprintMode `json:"rule"`
	CreatedAtMs int64                `json:"created_at_ms"`
}

// codexFlatEntityKey 把**规则**纳入键：flat 没有自然代次，若只按 scope 定键并由
// 首个创建者固定规则，则 device→full 这类 flat 之间的切换对**新会话**也永不生效，
// 直接违背"新会话使用新规则"。按 (scope, rule) 定键后，新会话指向新实体取新规则，
// 旧绑定仍指向旧实体、继续用旧规则。
func codexFlatEntityKey(scope string, rule codexFingerprintMode) string {
	return codexBindingDigest(fmt.Sprintf("v4:flat-entity:%d:%s", codexConstantGeneration, rule), scope)
}

func decodeCodexFlatEntity(raw string) (codexFlatEntity, error) {
	var entity codexFlatEntity
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &entity); err != nil {
		return entity, ErrCodexBindingInvalidValue
	}
	if entity.Version != codexSessionBindingVersion {
		return entity, ErrCodexBindingUnsupportedVersion
	}
	return entity, nil
}

// getOrCreateCodexFlatEntity 分配 flat 规则实体。代次恒为 0，所以键里没有
// 可变分量——重复调用只会读到首个建立者的规则，永不换代。
func getOrCreateCodexFlatEntity(
	ctx context.Context, store codexSessionIdentityStore,
	scope string, rule codexFingerprintMode, nowMs int64,
) (codexFlatEntity, error) {
	key := codexFlatEntityKey(scope, rule)
	if raw, err := store.GetCodexSessionIdentity(ctx, key); err == nil {
		return decodeCodexFlatEntity(raw)
	} else if !errors.Is(err, ErrCodexSessionIdentityNotFound) {
		return codexFlatEntity{}, fmt.Errorf("read Codex flat entity: %w", err)
	}
	candidate := codexFlatEntity{Version: codexSessionBindingVersion, Rule: rule, CreatedAtMs: nowMs}
	encoded, err := encodeCodexBindingRecord(candidate)
	if err != nil {
		return codexFlatEntity{}, err
	}
	created, err := store.SetCodexSessionIdentityIfAbsent(ctx, key, encoded, 0)
	if err != nil {
		return codexFlatEntity{}, fmt.Errorf("create Codex flat entity: %w", err)
	}
	if created {
		return candidate, nil
	}
	raw, err := store.GetCodexSessionIdentity(ctx, key)
	if err != nil {
		return codexFlatEntity{}, fmt.Errorf("read Codex flat entity: %w", err)
	}
	return decodeCodexFlatEntity(raw)
}

// adoptLegacyCodexPeriodSessionEntity 在 legacy period 映射存在时，以**原值**
// 建立 period 实体。
//
// 存在的意义：legacy 把上游 session id 存在裸 period 键（period.key）里，而绑定
// 模型用 v4:period-session 实体。若直接新分配，gate 一打开，已在 session 模式下
// 的会话就会**静默换掉上游 session id**——那不是"沿用旧规则"。原值接管让 gate
// 切换对这类会话保持身份连续。
//
// 返回 adopted=false 表示 legacy 没有该 period 的记录（真正的新会话），调用方按
// 常规路径分配。
func adoptLegacyCodexPeriodSessionEntity(
	ctx context.Context, store codexSessionIdentityStore,
	scope string, generation int64, legacyKey string, rule codexFingerprintMode, nowMs int64,
) (codexPeriodSessionEntity, bool, error) {
	if strings.TrimSpace(legacyKey) == "" {
		return codexPeriodSessionEntity{}, false, nil
	}
	raw, err := store.GetCodexSessionIdentity(ctx, legacyKey)
	if errors.Is(err, ErrCodexSessionIdentityNotFound) {
		return codexPeriodSessionEntity{}, false, nil
	}
	if err != nil {
		return codexPeriodSessionEntity{}, false, fmt.Errorf("read Codex legacy period session: %w", err)
	}
	legacyIdentity := strings.TrimSpace(raw)
	if !isCodexUUIDv7(legacyIdentity) {
		// 旧值不是本模型认识的形态：不猜、不接管。
		return codexPeriodSessionEntity{}, false, nil
	}
	entity := codexPeriodSessionEntity{
		Version:           codexSessionBindingVersion,
		IdentitySessionID: legacyIdentity,
		Rule:              rule,
		CreatedAtMs:       nowMs,
	}
	encoded, err := encodeCodexBindingRecord(entity)
	if err != nil {
		return codexPeriodSessionEntity{}, false, err
	}
	key := codexPeriodSessionEntityKey(scope, generation)
	if _, err := store.SetCodexSessionIdentityIfAbsent(ctx, key, encoded, 0); err != nil {
		return codexPeriodSessionEntity{}, false, fmt.Errorf("adopt Codex period session entity: %w", err)
	}
	stored, err := store.GetCodexSessionIdentity(ctx, key)
	if err != nil {
		return codexPeriodSessionEntity{}, false, fmt.Errorf("read Codex period session entity: %w", err)
	}
	decoded, err := decodeCodexPeriodSessionEntity(stored)
	return decoded, true, err
}

func readCodexSessionEntity(ctx context.Context, store codexSessionIdentityStore, ref string) (identitySessionID string, rule codexFingerprintMode, err error) {
	raw, err := store.GetCodexSessionIdentity(ctx, ref)
	if err != nil {
		return "", "", err
	}
	switch {
	case strings.HasPrefix(ref, "v4:flat-entity:"):
		entity, decodeErr := decodeCodexFlatEntity(raw)
		if decodeErr != nil {
			return "", "", decodeErr
		}
		// flat 不持有会话身份：返回空 session id 是语义本身，不是缺失。
		return "", entity.Rule, nil
	case strings.HasPrefix(ref, "v4:side-session:v2:"):
		entity, decodeErr := decodeCodexSideEntity(raw)
		if decodeErr != nil {
			return "", "", decodeErr
		}
		return entity.IdentitySessionID, entity.Rule, nil
	case strings.HasPrefix(ref, "v4:period-session:"):
		entity, decodeErr := decodeCodexPeriodSessionEntity(raw)
		if decodeErr != nil {
			return "", "", decodeErr
		}
		return entity.IdentitySessionID, entity.Rule, nil
	default:
		return "", "", ErrCodexBindingInvalidValue
	}
}

// ---------------------------------------------------------------------------
// current ↔ binding 的提交与恢复协议
// ---------------------------------------------------------------------------

// commitCodexSessionBinding 实现写序：**先写 binding，后写 current**。
//
// 单 key SETNX 不构成整个关系的原子提交，所以顺序是有意义的：binding 是权威且
// 不可变，current 只是寻址缓存。中途失败留下的是"有 binding、无 current"，
// resolveCodexSessionBindingCurrent 能探测修复；反过来则不可能出现。
//
// 返回的 binding 是**驻留的那一份完整记录**（SETNX 之后重读得到），不是本地候选：
// 同键合法竞争时复用整份胜出绑定，绝不把候选字段与胜出字段混用。写入结果单独
// 返回，因为指针可能正被更新的代次占用——那是正常竞争，请求仍保持自己的代次。
func commitCodexSessionBinding(
	ctx context.Context, store codexSessionIdentityStore,
	scope, rawSession string, binding codexSessionBinding,
) (codexSessionBinding, codexSessionCurrentWrite, error) {
	if !validCodexAttribution(binding.Attribution) || binding.EntityRef == "" {
		return codexSessionBinding{}, codexSessionCurrentWrite{}, ErrCodexBindingInvalidValue
	}
	binding.Version = codexSessionBindingVersion
	handle := codexSessionBindingKey(binding.Attribution, binding.Generation, scope, rawSession)
	encoded, err := encodeCodexBindingRecord(binding)
	if err != nil {
		return codexSessionBinding{}, codexSessionCurrentWrite{}, err
	}
	if _, err := store.SetCodexSessionIdentityIfAbsent(ctx, handle, encoded, 0); err != nil {
		return codexSessionBinding{}, codexSessionCurrentWrite{}, fmt.Errorf("create Codex session binding: %w", err)
	}
	committed, err := readCodexSessionBinding(ctx, store, handle)
	if err != nil {
		return codexSessionBinding{}, codexSessionCurrentWrite{}, err
	}
	write, err := writeCodexSessionCurrent(ctx, store, scope, rawSession, codexSessionCurrent{
		Version:     codexSessionBindingVersion,
		Attribution: committed.Attribution,
		Generation:  committed.Generation,
		EntityRef:   committed.EntityRef,
	})
	if err != nil {
		return codexSessionBinding{}, codexSessionCurrentWrite{}, err
	}
	return committed, write, nil
}

func readCodexSessionBinding(ctx context.Context, store codexSessionIdentityStore, handle string) (codexSessionBinding, error) {
	raw, err := store.GetCodexSessionIdentity(ctx, handle)
	if err != nil {
		return codexSessionBinding{}, err
	}
	return decodeCodexSessionBinding(raw)
}

// codexSessionCurrentWrite 描述一次指针写入的结果。
type codexSessionCurrentWrite struct {
	// Current 是指针当前驻留的值：可能是本次写入的，也可能是并发胜出者的。
	Current codexSessionCurrent
	// Applied 表示驻留值就是本次请求的目标值。
	Applied bool
}

// codexSessionCurrentWriteAttempts 是 CAS 竞争的重读次数上限。竞争是常态而非
// 异常，重试耗尽也不报错——返回驻留值即可，请求本就保持自己的代次。
const codexSessionCurrentWriteAttempts = 4

// writeCodexSessionCurrent 把指针向目标值推进，遵循两条并发规则：
//
//   - **指针只向前推进**：驻留代次更高时绝不回退，也**不报错**——那是正常竞争，
//     不是记录异常。只有无法解码、或同代次却指向不同实体，才算自相矛盾。
//   - **请求保持已选代次**：本函数返回驻留指针，但调用方已选定的代次不受影响；
//     是否跟随驻留值由调用方按语义决定（提交路径不跟随，解析路径才跟随）。
func writeCodexSessionCurrent(
	ctx context.Context, store codexSessionIdentityStore,
	scope, rawSession string, wanted codexSessionCurrent,
) (codexSessionCurrentWrite, error) {
	key := codexSessionCurrentKey(scope, rawSession)
	cas, ok := store.(codexHTTPIdentityCASStore)
	if !ok {
		return codexSessionCurrentWrite{}, ErrCodexSessionIdentityStoreUnavailable
	}
	encoded, err := encodeCodexBindingRecord(wanted)
	if err != nil {
		return codexSessionCurrentWrite{}, err
	}
	for attempt := 0; attempt < codexSessionCurrentWriteAttempts; attempt++ {
		raw, readErr := store.GetCodexSessionIdentity(ctx, key)
		switch {
		case readErr == nil:
			resident, decodeErr := decodeCodexSessionCurrent(raw)
			if decodeErr != nil {
				return codexSessionCurrentWrite{}, decodeErr
			}
			if strings.TrimSpace(raw) == encoded {
				return codexSessionCurrentWrite{Current: resident, Applied: true}, nil
			}
			if resident.Attribution != wanted.Attribution {
				// 归属先于代次判定：side 与 flat 的代次**都是 0**，跨归属比较代次
				// 大小没有意义；而 flat(0) 与 period(G) 之间更不存在"向前推进"。
				// 同一 raw session 上出现不同归属即归属漂移，必须明确失败。
				return codexSessionCurrentWrite{}, ErrCodexBindingAttributionConflict
			}
			if resident.Generation > wanted.Generation {
				// 同归属下指针已在更新的代次上：本次不写，也不回退。
				return codexSessionCurrentWrite{Current: resident, Applied: false}, nil
			}
			if resident.Generation == wanted.Generation {
				// 同归属同代次必然指向同一实体（period 由 scope+代次决定，
				// side/flat 由 raw session 决定），指向不同实体即为记录自相矛盾。
				return codexSessionCurrentWrite{}, ErrCodexBindingRecordAnomaly
			}
			updated, casErr := cas.CompareAndSwapCodexSessionIdentity(ctx, key, strings.TrimSpace(raw), encoded, 0)
			if casErr != nil {
				return codexSessionCurrentWrite{}, fmt.Errorf("write Codex session current: %w", casErr)
			}
			if updated {
				return codexSessionCurrentWrite{Current: wanted, Applied: true}, nil
			}
		case errors.Is(readErr, ErrCodexSessionIdentityNotFound):
			updated, casErr := cas.CompareAndSwapCodexSessionIdentity(ctx, key, "", encoded, 0)
			if casErr != nil {
				return codexSessionCurrentWrite{}, fmt.Errorf("write Codex session current: %w", casErr)
			}
			if updated {
				return codexSessionCurrentWrite{Current: wanted, Applied: true}, nil
			}
		default:
			return codexSessionCurrentWrite{}, fmt.Errorf("read Codex session current: %w", readErr)
		}
	}
	raw, readErr := store.GetCodexSessionIdentity(ctx, key)
	if readErr != nil {
		return codexSessionCurrentWrite{}, fmt.Errorf("read Codex session current: %w", readErr)
	}
	resident, decodeErr := decodeCodexSessionCurrent(raw)
	if decodeErr != nil {
		return codexSessionCurrentWrite{}, decodeErr
	}
	return codexSessionCurrentWrite{Current: resident, Applied: strings.TrimSpace(raw) == encoded}, nil
}

// resolveCodexSessionBindingCurrent 读取 current，并在缺失时按不变量 2 探测 binding。
//
// 返回 (nil, nil) 只表示"该 scope+raw session 没有任何已提交绑定"，**不**等于
// 新会话——归属仍须由调用方按证据判定（见阶段 B 的归属优先顺序）。
func resolveCodexSessionBindingCurrent(
	ctx context.Context, store codexSessionIdentityStore,
	scope, rawSession string, periodGeneration int64,
) (*codexSessionCurrent, error) {
	key := codexSessionCurrentKey(scope, rawSession)
	raw, err := store.GetCodexSessionIdentity(ctx, key)
	switch {
	case err == nil:
		current, decodeErr := decodeCodexSessionCurrent(raw)
		if decodeErr != nil {
			return nil, decodeErr
		}
		// current 存在但 binding 缺失 = 记录自相矛盾，必须明确失败，不得静默重建。
		if _, bindingErr := readCodexSessionBinding(ctx, store,
			codexSessionBindingKey(current.Attribution, current.Generation, scope, rawSession)); bindingErr != nil {
			if errors.Is(bindingErr, ErrCodexSessionIdentityNotFound) {
				return nil, ErrCodexBindingRecordAnomaly
			}
			return nil, bindingErr
		}
		return &current, nil
	case !errors.Is(err, ErrCodexSessionIdentityNotFound):
		return nil, fmt.Errorf("read Codex session current: %w", err)
	}

	// current 缺失：探测候选 binding（覆盖"写完 binding 后中断"）。
	//
	// 必须先**收集全部**命中再决定：若同时存在多个归属的绑定，说明归属发生过
	// 漂移或处于撕裂状态，此时挑任意一个都会让请求跟随一个未经确认的归属——
	// 宁可明确失败，也不静默选边。
	var found []codexSessionBinding
	for _, candidate := range []struct {
		attribution codexSessionAttribution
		generation  int64
	}{
		{codexAttributionSide, codexConstantGeneration},
		{codexAttributionFlat, codexConstantGeneration},
		{codexAttributionPeriod, periodGeneration},
	} {
		binding, probeErr := readCodexSessionBinding(ctx, store,
			codexSessionBindingKey(candidate.attribution, candidate.generation, scope, rawSession))
		if errors.Is(probeErr, ErrCodexSessionIdentityNotFound) {
			continue
		}
		if probeErr != nil {
			return nil, probeErr
		}
		found = append(found, binding)
	}
	switch len(found) {
	case 0:
		return nil, nil
	case 1:
	default:
		return nil, ErrCodexBindingAttributionConflict
	}
	binding := found[0]
	repair, repairErr := writeCodexSessionCurrent(ctx, store, scope, rawSession, codexSessionCurrent{
		Version:     codexSessionBindingVersion,
		Attribution: binding.Attribution,
		Generation:  binding.Generation,
		EntityRef:   binding.EntityRef,
	})
	if repairErr != nil {
		return nil, repairErr
	}
	// 解析路径采用**驻留**指针：并发若已把指针推进到更新的代次，就跟随它，
	// 而不是返回本地候选——避免把候选字段与驻留字段混用。
	settled := repair.Current
	return &settled, nil
}

// ---------------------------------------------------------------------------
// thread 两级寻址
// ---------------------------------------------------------------------------

// registerCodexThreadExact 写入权威且不可变的 thread 归属。
func registerCodexThreadExact(
	ctx context.Context, store codexSessionIdentityStore,
	scope, rawThread string, exact codexThreadExact,
) (codexThreadExact, error) {
	if exact.EntityRef == "" {
		return codexThreadExact{}, ErrCodexBindingInvalidValue
	}
	exact.Version = codexSessionBindingVersion
	key := codexThreadExactKey(scope, rawThread, exact.EntityRef)
	encoded, err := encodeCodexBindingRecord(exact)
	if err != nil {
		return codexThreadExact{}, err
	}
	if _, err := store.SetCodexSessionIdentityIfAbsent(ctx, key, encoded, 0); err != nil {
		return codexThreadExact{}, fmt.Errorf("create Codex thread exact record: %w", err)
	}
	raw, err := store.GetCodexSessionIdentity(ctx, key)
	if err != nil {
		return codexThreadExact{}, fmt.Errorf("read Codex thread exact record: %w", err)
	}
	return decodeCodexThreadExact(raw)
}

// recordCodexThreadLatest 推进 latest 索引（latest-wins）。
// 它只是候选线索，任何把命中当作代次结论的用法都是不变量 1 的回归。
func recordCodexThreadLatest(
	ctx context.Context, store codexSessionIdentityStore,
	scope, rawThread string, latest codexThreadLatest,
) error {
	latest.Version = codexSessionBindingVersion
	key := codexThreadLatestKey(scope, rawThread)
	cas, ok := store.(codexHTTPIdentityCASStore)
	if !ok {
		return ErrCodexSessionIdentityStoreUnavailable
	}
	encoded, err := encodeCodexBindingRecord(latest)
	if err != nil {
		return err
	}
	previous, err := store.GetCodexSessionIdentity(ctx, key)
	if err != nil && !errors.Is(err, ErrCodexSessionIdentityNotFound) {
		return fmt.Errorf("read Codex thread latest index: %w", err)
	}
	if err == nil {
		current, decodeErr := decodeCodexThreadLatest(previous)
		if decodeErr != nil {
			return decodeErr
		}
		// 旧 observation 不得回退索引（沿用既有 thread-history 的 stale 语义）。
		if current.ObservedAtMs > latest.ObservedAtMs {
			return nil
		}
	}
	expected := previous
	if errors.Is(err, ErrCodexSessionIdentityNotFound) {
		expected = ""
	}
	if _, err := cas.CompareAndSwapCodexSessionIdentity(ctx, key, expected, encoded, 0); err != nil {
		return fmt.Errorf("write Codex thread latest index: %w", err)
	}
	return nil
}

// resolveCodexThreadForkSource 解析 fork 源，只认 exact 结论。
//
// 顺序：latest → 候选 entity_ref → 读该实体下的 exact。exact 缺失即"暂未解析"，
// 返回 ErrCodexBindingUnresolvedSource：不回退、不猜测、不合成。
// 命中 exact 时**允许**它属于较早代次——真实的旧来源必须被引用。
func resolveCodexThreadForkSource(
	ctx context.Context, store codexSessionIdentityStore,
	scope, rawThread string,
) (codexThreadExact, error) {
	latestRaw, err := store.GetCodexSessionIdentity(ctx, codexThreadLatestKey(scope, rawThread))
	if errors.Is(err, ErrCodexSessionIdentityNotFound) {
		return codexThreadExact{}, ErrCodexBindingUnresolvedSource
	}
	if err != nil {
		return codexThreadExact{}, fmt.Errorf("read Codex thread latest index: %w", err)
	}
	latest, err := decodeCodexThreadLatest(latestRaw)
	if err != nil {
		return codexThreadExact{}, err
	}
	exactRaw, err := store.GetCodexSessionIdentity(ctx, codexThreadExactKey(scope, rawThread, latest.EntityRef))
	if errors.Is(err, ErrCodexSessionIdentityNotFound) {
		// latest 说有、exact 说没有 —— latest 只是线索，不能当作来源已确定。
		return codexThreadExact{}, ErrCodexBindingUnresolvedSource
	}
	if err != nil {
		return codexThreadExact{}, fmt.Errorf("read Codex thread exact record: %w", err)
	}
	return decodeCodexThreadExact(exactRaw)
}

// ---------------------------------------------------------------------------
// 旧 side 的完整投影接管
// ---------------------------------------------------------------------------

// codexLegacySideProjection 是旧模型里一条 side 的全部投影证据。
// 接管必须整套搬运，只接 session id 与 fork 源是不够的。
type codexLegacySideProjection struct {
	SideSessionID     string
	ForkSource        string
	PreserveV3Threads bool
	SourceKey         string
}

// readCodexLegacySideProjection 只读既有键，构造旧投影。**不写任何键**。
func readCodexLegacySideProjection(
	ctx context.Context, store codexSessionIdentityStore,
	userScope, accountScope, rawSession, rawFork string,
) (codexLegacySideProjection, error) {
	projection := codexLegacySideProjection{}
	sideRaw, err := store.GetCodexSessionIdentity(ctx, codexHTTPIdentityMappingKey("side-session", userScope, accountScope, rawSession))
	switch {
	case err == nil:
		projection.SideSessionID = strings.TrimSpace(sideRaw)
		if !isCodexUUIDv7(projection.SideSessionID) {
			return projection, ErrCodexBindingInvalidValue
		}
	case errors.Is(err, ErrCodexSessionIdentityNotFound):
	default:
		return projection, fmt.Errorf("read Codex legacy side session: %w", err)
	}
	sideForkKey := codexHTTPThreadKey("side-fork", userScope, accountScope, "", rawSession)
	fork, forkErr := readCodexHTTPSideFork(ctx, store, sideForkKey)
	switch {
	case forkErr == nil:
		projection.ForkSource = strings.TrimSpace(fork.ThreadID)
		projection.PreserveV3Threads = fork.PreserveV3Threads
		projection.SourceKey = fork.SourceKey
	case errors.Is(forkErr, ErrCodexSessionIdentityNotFound):
	default:
		return projection, fmt.Errorf("read Codex legacy side fork: %w", forkErr)
	}
	return projection, nil
}

// adoptCodexLegacySideEntity 以**原值**把旧投影接管成 side 实体。
//
// 不重算、不改指向：IdentitySessionID 直接用旧存的 side-session 值（连新分配
// 的 v7 都不用），ForkSource 直接用旧存的固定目标。若旧记录里没有 side-session，
// 本函数返回 adopted=false —— 此时不算接管，走正常建立流程。
func adoptCodexLegacySideEntity(
	ctx context.Context, store codexSessionIdentityStore,
	scope, rawSession string, projection codexLegacySideProjection,
	rule codexFingerprintMode, nowMs int64,
) (codexSideEntity, bool, error) {
	if projection.SideSessionID == "" {
		return codexSideEntity{}, false, nil
	}
	key := codexSideEntityKey(scope, rawSession)
	if raw, err := store.GetCodexSessionIdentity(ctx, key); err == nil {
		existing, decodeErr := decodeCodexSideEntity(raw)
		return existing, true, decodeErr
	} else if !errors.Is(err, ErrCodexSessionIdentityNotFound) {
		return codexSideEntity{}, false, fmt.Errorf("read Codex side entity: %w", err)
	}
	if projection.ForkSource != "" && !isCodexUUID(projection.ForkSource) {
		return codexSideEntity{}, false, ErrCodexBindingInvalidValue
	}
	entity := codexSideEntity{
		Version:           codexSessionBindingVersion,
		IdentitySessionID: projection.SideSessionID,
		Rule:              rule,
		ForkSource:        projection.ForkSource,
		PreserveV3Threads: projection.PreserveV3Threads,
		Adopted:           true,
		CreatedAtMs:       nowMs,
	}
	encoded, err := encodeCodexBindingRecord(entity)
	if err != nil {
		return codexSideEntity{}, false, err
	}
	if _, err := store.SetCodexSessionIdentityIfAbsent(ctx, key, encoded, 0); err != nil {
		return codexSideEntity{}, false, fmt.Errorf("adopt Codex side entity: %w", err)
	}
	raw, err := store.GetCodexSessionIdentity(ctx, key)
	if err != nil {
		return codexSideEntity{}, false, fmt.Errorf("read Codex side entity: %w", err)
	}
	adopted, err := decodeCodexSideEntity(raw)
	return adopted, true, err
}
