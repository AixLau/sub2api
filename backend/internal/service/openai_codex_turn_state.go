package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// openAICodexTurnStateHeader 是 Codex 的回合状态头。上游在响应头中铸造该
// 不透明 blob，客户端在同一回合的后续请求中原样回带（codex-rs 侧从
// /responses SSE、/responses/compact JSON 与 WS 握手三种响应中捕获，见
// codex-api/src/sse/responses.rs 与 endpoint/compact.rs）。
const openAICodexTurnStateHeader = "x-codex-turn-state"

// turn-state blob 是上游在"出站身份"（含 #5553 指纹收敛改写后的
// installation/session/thread 标识）下铸造的，同账号同身份回放自洽；跨账号
// 回放（failover 换号后客户端仍回带旧账号的 blob）是代理链独有、真实 Codex
// 永远不会产生的矛盾信号。溯源表记录每个下游会话最近一次铸造该 blob 的
// **账号与出站身份**，出站守卫据此剥离已知不一致的回带值。
//
// 只记账号是不够的：device→session 模式切换、period 换代、main↔side 切换
// 都会在同一账号下换掉上游 session_id（见 resolveCodexSessionPeriod），此时
// 账号没变但 blob 所属的上游会话已经不同，仅凭账号判定会把它当作合法回带。
type openAICodexTurnStateOrigin struct {
	AccountID int64                        `json:"account_id"`
	Identity  openAICodexTurnStateIdentity `json:"identity"`
}

// openAICodexTurnStateIdentity 是铸造 blob 时的出站身份戳。它与上游实际看到的
// session 投影同源（codexFingerprintIDs 的 mode/sessionID），因此身份变化必然
// 反映在这里。
type openAICodexTurnStateIdentity struct {
	Mode      codexFingerprintMode `json:"mode,omitempty"`
	SessionID string               `json:"session_id,omitempty"`
}

// known 表示这份身份戳可用。模式为空说明记账时拿不到收敛 ID（off 模式、
// compact 形态或 WS/兼容桥以外的路径），此时只能退化为按账号判定。
func (i openAICodexTurnStateIdentity) known() bool {
	return strings.TrimSpace(string(i.Mode)) != ""
}

func codexTurnStateIdentityFromIDs(ids *codexFingerprintIDs) openAICodexTurnStateIdentity {
	if ids == nil {
		return openAICodexTurnStateIdentity{}
	}
	return openAICodexTurnStateIdentity{Mode: ids.mode, SessionID: ids.sessionID}
}

// ErrCodexTurnStateOriginNotFound 由 GatewayCache.GetCodexTurnStateOrigin 在
// 未命中时返回，使 service 层无需依赖具体缓存实现即可区分"无溯源记录"
// （守卫 fail-open 放行）与真实读取失败。
var ErrCodexTurnStateOriginNotFound = errors.New("Codex turn-state origin not found")

// CodexTurnStateOriginStore 由持久化网关缓存实现。它与 codexSessionIdentityStore
// 同属"可选接口"：未实现它的测试替身与无 Redis 部署继续编译，此时溯源退化为
// 不追踪（守卫放行），与本机制引入前的行为一致。
//
// 之所以要跨实例共享：溯源记录在服务重启或请求落到另一个实例后必须仍然可读，
// 否则守卫在身份已经变化时仍会放行旧 blob。
type CodexTurnStateOriginStore interface {
	SetCodexTurnStateOrigin(ctx context.Context, key, value string, ttl time.Duration) error
	GetCodexTurnStateOrigin(ctx context.Context, key string) (string, error)
}

func (s *OpenAIGatewayService) codexTurnStateOriginStore() (CodexTurnStateOriginStore, bool) {
	if s == nil || s.cache == nil {
		return nil, false
	}
	store, ok := s.cache.(CodexTurnStateOriginStore)
	if !ok || store == nil {
		return nil, false
	}
	return store, true
}

// codexTurnStateOriginKey 把下游会话 seed 单向哈希成存储键：raw 的 API Key ID
// 与客户端会话标识不出现在键名里（与 codexHTTPIdentityMappingKey 同风格）。
func codexTurnStateOriginKey(seed string) string {
	digest := sha256.Sum256([]byte("sub2api:codex-turn-state-origin:v1\x00" + seed))
	return fmt.Sprintf("%x", digest[:])
}

// openAICodexTurnStateSeed 返回溯源键：API Key + 客户端原始会话标识。
// 客户端会话标识取自请求头（与指纹收敛的 thread 派生同源，见
// extractClientSessionID），确保同一下游会话的记录/守卫两侧使用同一键。
// 无会话标识时返回空串，表示不做跟踪（保持透传现状）。
func openAICodexTurnStateSeed(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	sessionID := extractClientSessionID(c.Request.Header)
	if sessionID == "" {
		return ""
	}
	return strconv.FormatInt(getAPIKeyIDFromContext(c), 10) + "\x00" + sessionID
}

func codexTurnStateRequestContext(c *gin.Context) context.Context {
	if c != nil && c.Request != nil && c.Request.Context() != nil {
		return c.Request.Context()
	}
	return context.Background()
}

// relayOpenAICodexTurnState 将上游响应中的 turn-state 显式写入下游响应头，
// 并记录铸造账号。必须在响应头提交点调用（WriteHeader 之前、且确认本次
// 上游响应就是将要写回客户端的响应之后）。上游无该头时主动清除 writer 上
// 可能残留的上一 failover attempt 的值——否则换号后旧账号的 blob 会粘到
// 新账号的响应上，这正是本文件要防止的跨账号矛盾。
func (s *OpenAIGatewayService) relayOpenAICodexTurnState(c *gin.Context, account *Account, upstream http.Header) {
	if c == nil || c.Writer == nil {
		return
	}
	canonical := http.CanonicalHeaderKey(openAICodexTurnStateHeader)
	state := extractOpenAICodexTurnState(upstream)
	if state == "" {
		c.Writer.Header().Del(canonical)
		return
	}
	c.Writer.Header().Set(canonical, state)
	s.noteOpenAICodexTurnStateProvenance(c, account)
}

// stageOpenAICodexTurnState 将上游 turn-state 暂存到延迟提交的响应头集合
// （首输出守卫路径先缓存头、见到首个输出事件才提交）。此处**不**记录铸造
// 账号：该 attempt 仍可能在首输出超时后 failover，暂存头会被整体丢弃，
// 客户端从未收到该 blob。溯源必须在真正提交时记录，见
// noteStagedOpenAICodexTurnStateCommitted。
func stageOpenAICodexTurnState(dst *http.Header, upstream http.Header) {
	if dst == nil {
		return
	}
	canonical := http.CanonicalHeaderKey(openAICodexTurnStateHeader)
	state := extractOpenAICodexTurnState(upstream)
	if state == "" {
		if *dst != nil {
			dst.Del(canonical)
		}
		return
	}
	if *dst == nil {
		*dst = http.Header{}
	}
	dst.Set(canonical, state)
}

// noteStagedOpenAICodexTurnStateCommitted 在暂存响应头真正写入下游时记录
// 铸造账号——只有此刻客户端才确定收到了该 blob，溯源表才与客户端持有的
// 值一致（否则被 failover 丢弃的 attempt 会污染溯源，导致后续误剥离）。
func (s *OpenAIGatewayService) noteStagedOpenAICodexTurnStateCommitted(c *gin.Context, account *Account, staged http.Header) {
	if staged == nil || strings.TrimSpace(staged.Get(openAICodexTurnStateHeader)) == "" {
		return
	}
	s.noteOpenAICodexTurnStateProvenance(c, account)
}

// notePassthroughOpenAICodexTurnStateCommitted 在透传响应真正写给客户端之后
// 记录溯源。
//
// 透传路径不能在拿到上游 200 响应头时就记账：首个可见输出之前 pendingLines
// 全缓冲、连响应头都未提交，而首输出失败、上游断流、空终止事件都会触发
// failover 丢弃该 attempt——被丢弃的 attempt 一旦留下记录，客户端真正持有的
// blob 会在下一回合被误判成异账号回带而剥离。与非透传路径的
// noteStagedOpenAICodexTurnStateCommitted 同语义：只有确认交付才记账。
func (s *OpenAIGatewayService) notePassthroughOpenAICodexTurnStateCommitted(c *gin.Context, account *Account, resp *http.Response) {
	if s == nil || resp == nil || c == nil || c.Writer == nil {
		return
	}
	if extractOpenAICodexTurnState(resp.Header) == "" {
		return
	}
	if !c.Writer.Written() {
		return
	}
	s.noteOpenAICodexTurnStateProvenance(c, account)
}

func extractOpenAICodexTurnState(upstream http.Header) string {
	if upstream == nil {
		return ""
	}
	return strings.TrimSpace(upstream.Get(openAICodexTurnStateHeader))
}

// noteOpenAICodexTurnStateProvenance 记录（下游会话 → 铸造账号 + 出站身份）。
func (s *OpenAIGatewayService) noteOpenAICodexTurnStateProvenance(c *gin.Context, account *Account) {
	if s == nil || account == nil || account.ID <= 0 {
		return
	}
	seed := openAICodexTurnStateSeed(c)
	if seed == "" {
		return
	}
	store, ok := s.codexTurnStateOriginStore()
	if !ok {
		return
	}
	ttl := s.openAIWSSessionStickyTTL()
	if ttl <= 0 {
		return
	}
	encoded, err := json.Marshal(openAICodexTurnStateOrigin{
		AccountID: account.ID,
		Identity:  codexTurnStateIdentityFromIDs(stagedCodexFingerprintIDs(c, account)),
	})
	if err != nil {
		return
	}
	if err := store.SetCodexTurnStateOrigin(codexTurnStateRequestContext(c), codexTurnStateOriginKey(seed), string(encoded), ttl); err != nil {
		RecordCodexIdentityEvent("turn_state_origin", "write_error")
	}
}

// guardOpenAICodexTurnStateEcho 出站守卫：客户端回带的 turn-state 若已知由
// 其他账号、或同一账号下的另一个上游身份铸造则剥离；身份一致或无溯源记录时
// 保持原样。只剥离、不注入——/responses 路径的客户端是真实 Codex，会按自身
// 回合语义自行回带；服务端注入是 Claude 兼容桥（无法回带的客户端）的专属行为。
func (s *OpenAIGatewayService) guardOpenAICodexTurnStateEcho(c *gin.Context, account *Account, h http.Header) {
	if s == nil || h == nil || account == nil {
		return
	}
	if strings.TrimSpace(h.Get(openAICodexTurnStateHeader)) == "" {
		return
	}
	seed := openAICodexTurnStateSeed(c)
	if seed == "" {
		return
	}
	store, ok := s.codexTurnStateOriginStore()
	if !ok {
		return
	}
	raw, err := store.GetCodexTurnStateOrigin(codexTurnStateRequestContext(c), codexTurnStateOriginKey(seed))
	if err != nil {
		if !errors.Is(err, ErrCodexTurnStateOriginNotFound) {
			RecordCodexIdentityEvent("turn_state_origin", "read_error")
		}
		return
	}
	var origin openAICodexTurnStateOrigin
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &origin); err != nil {
		// 记录损坏：按"无溯源"处理并放行，与本机制引入前的行为一致。
		return
	}
	if origin.AccountID != account.ID {
		h.Del(openAICodexTurnStateHeader)
		return
	}
	// 同账号不足以证明可回放：device↔session 模式切换、period 换代、
	// main↔side 切换都会在同一账号下换掉上游 session_id，旧 blob 属于另一个
	// 上游会话，回带即矛盾信号。两侧身份有一侧未知时不判定（放行）。
	if current := codexTurnStateIdentityFromIDs(stagedCodexFingerprintIDs(c, account)); origin.Identity.known() && current.known() && origin.Identity != current {
		h.Del(openAICodexTurnStateHeader)
	}
}