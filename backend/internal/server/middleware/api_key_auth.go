package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// NewAPIKeyAuthMiddleware 创建 API Key 认证中间件
func NewAPIKeyAuthMiddleware(apiKeyService *service.APIKeyService, subscriptionService *service.SubscriptionService, cfg *config.Config) APIKeyAuthMiddleware {
	return APIKeyAuthMiddleware(apiKeyAuthWithSubscription(apiKeyService, subscriptionService, cfg))
}

// apiKeyAuthWithSubscription API Key认证中间件（支持订阅验证）
//
// 中间件职责分为两层：
//   - 鉴权（Authentication）：验证 Key 有效性、用户状态、IP 限制 —— 始终执行
//   - 计费执行（Billing Enforcement）：过期/配额/订阅/余额检查 —— skipBilling 时整块跳过
//
// /v1/usage、/v1/sub2api/billing 端点与异步生图任务查询只需鉴权，不需要计费执行。
// usage 允许过期/配额耗尽的 Key 查询自身用量，billing 用于读取当前 Key 的倍率配置，
// 异步生图查询允许已耗尽额度的 Key 拉取自身任务结果。
func apiKeyAuthWithSubscription(apiKeyService *service.APIKeyService, subscriptionService *service.SubscriptionService, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// ── 1. 提取 API Key ──────────────────────────────────────────

		queryKey := strings.TrimSpace(c.Query("key"))
		queryApiKey := strings.TrimSpace(c.Query("api_key"))
		if queryKey != "" || queryApiKey != "" {
			markOpsAPIKeyAuthDiagnostic(c, "api_key_in_query_deprecated", "api_key_in_query", nil, map[string]string{
				"parameter": firstNonEmpty(queryKeyName(queryKey, "key"), queryKeyName(queryApiKey, "api_key")),
			})
			AbortWithError(c, 400, "api_key_in_query_deprecated", localizedAPIKeyAuthMessage("api_key_in_query_deprecated"))
			return
		}

		// 尝试从Authorization header中提取API key (Bearer scheme)
		authHeader := c.GetHeader("Authorization")
		var apiKeyString string

		if authHeader != "" {
			// 验证Bearer scheme
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
				apiKeyString = strings.TrimSpace(parts[1])
			}
		}

		// 如果Authorization header中没有，尝试从x-api-key header中提取
		if apiKeyString == "" {
			apiKeyString = c.GetHeader("x-api-key")
		}

		// 如果x-api-key header中没有，尝试从x-goog-api-key header中提取（Gemini CLI兼容）
		if apiKeyString == "" {
			apiKeyString = c.GetHeader("x-goog-api-key")
		}

		// 如果所有header都没有API key
		if apiKeyString == "" {
			markOpsAPIKeyAuthDiagnostic(c, "API_KEY_REQUIRED", "missing_api_key", nil, map[string]string{
				"accepted_headers": "Authorization Bearer,x-api-key,x-goog-api-key",
			})
			AbortWithError(c, 401, "API_KEY_REQUIRED", localizedAPIKeyAuthMessage("API_KEY_REQUIRED"))
			return
		}

		// ── 2. 验证 Key 存在 ─────────────────────────────────────────

		apiKey, err := apiKeyService.GetByKey(c.Request.Context(), apiKeyString)
		if err != nil {
			if errors.Is(err, service.ErrAPIKeyNotFound) {
				markOpsAPIKeyAuthDiagnostic(c, "INVALID_API_KEY", "api_key_not_found", nil, map[string]string{
					"attempted_key_prefix": apiKeyPrefixForOps(apiKeyString),
				})
				AbortWithError(c, 401, "INVALID_API_KEY", localizedAPIKeyAuthMessage("INVALID_API_KEY"))
				return
			}
			markOpsAPIKeyAuthDiagnostic(c, "INTERNAL_ERROR", "api_key_lookup_failed", nil, map[string]string{
				"raw_error": strings.TrimSpace(err.Error()),
			})
			AbortWithError(c, 500, "INTERNAL_ERROR", localizedAPIKeyAuthMessage("INTERNAL_ERROR"))
			return
		}

		// apiKey 已加载（含 User/Group）。即便后续因分组停用/Key 停用/用户停用/
		// IP 限制等早退中断，也让 Ops 错误日志能回退取到 user/group/platform。
		SetOpsFallbackAPIKey(c, apiKey)

		// ── 3. 基础鉴权（始终执行） ─────────────────────────────────

		// disabled / 未知状态 → 无条件拦截（expired 和 quota_exhausted 留给计费阶段）
		if !apiKey.IsActive() &&
			apiKey.Status != service.StatusAPIKeyExpired &&
			apiKey.Status != service.StatusAPIKeyQuotaExhausted {
			markOpsAPIKeyAuthDiagnostic(c, "API_KEY_DISABLED", "api_key_disabled", apiKey, nil)
			AbortWithError(c, 401, "API_KEY_DISABLED", localizedAPIKeyAuthMessage("API_KEY_DISABLED"))
			return
		}

		// 检查 IP 限制（白名单/黑名单）
		// 注意：错误信息故意模糊，避免暴露具体的 IP 限制机制
		if len(apiKey.IPWhitelist) > 0 || len(apiKey.IPBlacklist) > 0 {
			clientIP := ip.GetTrustedClientIP(c)
			if cfg.TrustForwardedIPForAPIKeyACL() {
				clientIP = ip.GetClientIP(c)
			}
			allowed, _ := ip.CheckIPRestrictionWithCompiledRules(clientIP, apiKey.CompiledIPWhitelist, apiKey.CompiledIPBlacklist)
			if !allowed {
				if clientIP == "" {
					clientIP = "unknown"
				}
				service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonIPRestriction)
				markOpsAPIKeyAuthDiagnostic(c, "ACCESS_DENIED", "api_key_ip_restriction", apiKey, map[string]string{
					"client_ip":            clientIP,
					"whitelist_configured": strconv.FormatBool(len(apiKey.IPWhitelist) > 0),
					"blacklist_configured": strconv.FormatBool(len(apiKey.IPBlacklist) > 0),
				})
				AbortWithError(c, 403, "ACCESS_DENIED", localizedAccessDeniedMessage(clientIP))
				return
			}
		}

		// 检查关联的用户
		if apiKey.User == nil {
			markOpsAPIKeyAuthDiagnostic(c, "USER_NOT_FOUND", "api_key_user_missing", apiKey, nil)
			AbortWithError(c, 401, "USER_NOT_FOUND", localizedAPIKeyAuthMessage("USER_NOT_FOUND"))
			return
		}

		// 检查用户状态
		if !apiKey.User.IsActive() {
			markOpsAPIKeyAuthDiagnostic(c, "USER_INACTIVE", "user_inactive", apiKey, nil)
			AbortWithError(c, 401, "USER_INACTIVE", localizedAPIKeyAuthMessage("USER_INACTIVE"))
			return
		}
		if abortIfAPIKeyGroupUnavailable(c, apiKey) {
			return
		}
		if abortIfAPIKeyGroupNotAllowed(c, apiKey) {
			return
		}
		ctx := context.WithValue(c.Request.Context(), ctxkey.UserID, apiKey.User.ID)
		c.Request = c.Request.WithContext(ctx)
		billingInfoRequest := c.Request.URL.Path == "/v1/sub2api/billing"
		// Async image task polling only reads data that already belongs to the
		// authenticated key and must remain available after the completed
		// generation consumes the key's remaining balance.
		skipBilling := c.Request.URL.Path == "/v1/usage" || billingInfoRequest || isAsyncImageTaskRead(c.Request.Method, c.Request.URL.Path)

		// ── 4. SimpleMode → early return ─────────────────────────────

		if cfg.RunMode == config.RunModeSimple {
			c.Set(string(ContextKeyAPIKey), apiKey)
			c.Set(string(ContextKeyUser), AuthSubject{
				UserID:      apiKey.User.ID,
				Concurrency: apiKey.User.Concurrency,
			})
			c.Set(string(ContextKeyUserRole), apiKey.User.Role)
			setGroupContext(c, apiKey.Group)
			if !billingInfoRequest {
				_ = apiKeyService.TouchLastUsed(c.Request.Context(), apiKey.ID)
			}
			c.Next()
			return
		}

		// ── 5. 按端点需要加载订阅 ───────────────────────────────────

		var subscription *service.UserSubscription
		isSubscriptionType := apiKey.Group != nil && apiKey.Group.IsSubscriptionType()
		var subscriptionLoadErr error

		// 倍率自省不需要订阅数据；/v1/usage 仍保留原有订阅读取行为。
		if isSubscriptionType && subscriptionService != nil && !billingInfoRequest {
			sub, subErr := subscriptionService.GetActiveSubscription(
				c.Request.Context(),
				apiKey.User.ID,
				apiKey.Group.ID,
			)
			if subErr != nil {
				subscriptionLoadErr = subErr
			} else {
				subscription = sub
			}
		}

		// ── 6. 计费执行（skipBilling 时整块跳过） ────────────────────

		if !skipBilling {
			// Key 状态检查
			switch apiKey.Status {
			case service.StatusAPIKeyQuotaExhausted:
				markOpsAPIKeyAuthDiagnostic(c, "API_KEY_QUOTA_EXHAUSTED", "api_key_quota_exhausted", apiKey, nil)
				AbortWithError(c, 429, "API_KEY_QUOTA_EXHAUSTED", "API key 额度已用完")
				return
			case service.StatusAPIKeyExpired:
				markOpsAPIKeyAuthDiagnostic(c, "API_KEY_EXPIRED", "api_key_expired", apiKey, nil)
				AbortWithError(c, 403, "API_KEY_EXPIRED", "API key 已过期")
				return
			}

			// 运行时过期/配额检查（即使状态是 active，也要检查时间和用量）
			if apiKey.IsExpired() {
				markOpsAPIKeyAuthDiagnostic(c, "API_KEY_EXPIRED", "api_key_expired", apiKey, nil)
				AbortWithError(c, 403, "API_KEY_EXPIRED", "API key 已过期")
				return
			}
			if apiKey.IsQuotaExhausted() {
				markOpsAPIKeyAuthDiagnostic(c, "API_KEY_QUOTA_EXHAUSTED", "api_key_quota_exhausted", apiKey, nil)
				AbortWithError(c, 429, "API_KEY_QUOTA_EXHAUSTED", "API key 额度已用完")
				return
			}

			var billingErr error
			if isSubscriptionType {
				if subscriptionService == nil {
					if apiKeyBalanceBelowAuthThreshold(apiKey.User.Balance, cfg) {
						billingErr = service.ErrInsufficientBalance
					}
				} else {
					billingErr = subscriptionLoadErr
				}
				if billingErr == nil && subscriptionService != nil {
					subscription, billingErr = validateRequestSubscription(c.Request.Context(), subscriptionService, subscription, apiKey.Group)
				}
			} else if apiKeyBalanceBelowAuthThreshold(apiKey.User.Balance, cfg) {
				billingErr = service.ErrInsufficientBalance
			}

			if billingErr != nil && (errors.Is(billingErr, service.ErrInsufficientBalance) || canFallbackFromSubscriptionError(billingErr)) {
				excludedGroupID := int64(0)
				if apiKey.Group != nil && apiKey.Group.IsSubscriptionType() {
					excludedGroupID = apiKey.Group.ID
				}
				fallbackKey, fallbackSub, fallbackErr := findFallbackSubscription(c.Request.Context(), subscriptionService, apiKey, excludedGroupID)
				if fallbackErr == nil {
					apiKey = fallbackKey
					subscription = fallbackSub
					isSubscriptionType = true
					billingErr = nil
					SetOpsFallbackAPIKey(c, apiKey)
				}
			}

			if billingErr != nil {
				if isSubscriptionType {
					validateErr := billingErr
					if errors.Is(validateErr, service.ErrSubscriptionNotFound) {
						markOpsAPIKeyAuthDiagnostic(c, "SUBSCRIPTION_NOT_FOUND", "subscription_not_found", apiKey, map[string]string{
							"raw_error": strings.TrimSpace(validateErr.Error()),
						})
						AbortWithError(c, 403, "SUBSCRIPTION_NOT_FOUND", localizedAPIKeyAuthMessage("SUBSCRIPTION_NOT_FOUND"))
						return
					}
					code := "SUBSCRIPTION_INVALID"
					status := 403
					if errors.Is(validateErr, service.ErrDailyLimitExceeded) ||
						errors.Is(validateErr, service.ErrWeeklyLimitExceeded) ||
						errors.Is(validateErr, service.ErrMonthlyLimitExceeded) {
						code = "USAGE_LIMIT_EXCEEDED"
						status = 429
					}
					markOpsAPIKeyAuthDiagnostic(c, code, strings.ToLower(code), apiKey, map[string]string{
						"raw_error": strings.TrimSpace(validateErr.Error()),
					})
					AbortWithError(c, status, code, localizedSubscriptionErrorMessage(validateErr))
					return
				}
				markOpsAPIKeyAuthDiagnostic(c, "INSUFFICIENT_BALANCE", "insufficient_balance", apiKey, nil)
				AbortWithError(c, 403, "INSUFFICIENT_BALANCE", localizedAPIKeyAuthMessage("INSUFFICIENT_BALANCE"))
				return
			}
		}

		// ── 7. 设置上下文 → Next ─────────────────────────────────────

		if subscription != nil {
			c.Set(string(ContextKeySubscription), subscription)
		}
		c.Set(string(ContextKeyAPIKey), apiKey)
		c.Set(string(ContextKeyUser), AuthSubject{
			UserID:      apiKey.User.ID,
			Concurrency: apiKey.User.Concurrency,
		})
		c.Set(string(ContextKeyUserRole), apiKey.User.Role)
		setGroupContext(c, apiKey.Group)
		if !billingInfoRequest {
			_ = apiKeyService.TouchLastUsed(c.Request.Context(), apiKey.ID)
		}

		c.Next()
	}
}

func isAsyncImageTaskRead(method, path string) bool {
	if method != http.MethodGet {
		return false
	}
	return strings.HasPrefix(path, "/v1/images/tasks/") || strings.HasPrefix(path, "/images/tasks/")
}

// GetAPIKeyFromContext 从上下文中获取API key
func GetAPIKeyFromContext(c *gin.Context) (*service.APIKey, bool) {
	value, exists := c.Get(string(ContextKeyAPIKey))
	if !exists {
		return nil, false
	}
	apiKey, ok := value.(*service.APIKey)
	return apiKey, ok
}

// SetOpsFallbackAPIKey 记录已加载的 API Key，供 Ops 错误日志在鉴权早退时回退使用。
// 与 ContextKeyAPIKey 区分：写入它不代表请求已通过鉴权，因此不影响 handler、
// 审计日志等对“已鉴权”的判断。
func SetOpsFallbackAPIKey(c *gin.Context, apiKey *service.APIKey) {
	if c == nil || apiKey == nil {
		return
	}
	c.Set(string(ContextKeyOpsFallbackAPIKey), apiKey)
}

// GetOpsFallbackAPIKey 读取 Ops 错误日志专用的回退 API Key。
func GetOpsFallbackAPIKey(c *gin.Context) (*service.APIKey, bool) {
	value, exists := c.Get(string(ContextKeyOpsFallbackAPIKey))
	if !exists {
		return nil, false
	}
	apiKey, ok := value.(*service.APIKey)
	return apiKey, ok
}

// GetSubscriptionFromContext 从上下文中获取订阅信息
func GetSubscriptionFromContext(c *gin.Context) (*service.UserSubscription, bool) {
	value, exists := c.Get(string(ContextKeySubscription))
	if !exists {
		return nil, false
	}
	subscription, ok := value.(*service.UserSubscription)
	return subscription, ok
}

func setGroupContext(c *gin.Context, group *service.Group) {
	if !service.IsGroupContextValid(group) {
		return
	}
	if existing, ok := c.Request.Context().Value(ctxkey.Group).(*service.Group); ok && existing != nil && existing.ID == group.ID && service.IsGroupContextValid(existing) {
		return
	}
	ctx := context.WithValue(c.Request.Context(), ctxkey.Group, group)
	c.Request = c.Request.WithContext(ctx)
}

// apiKeyBalanceBelowAuthThreshold 保持鉴权层的历史语义：仅在余额耗尽（<=0）时拒绝。
// MinimumBalanceReserve 只作为 billing-cache 预检的保守下限，不得复用为鉴权硬门槛，
// 否则已配置该值的存量部署升级后，0 < balance < reserve 的用户会在所有端点被静默 403。
func apiKeyBalanceBelowAuthThreshold(balance float64, _ *config.Config) bool {
	return balance <= 0
}

func abortIfAPIKeyGroupUnavailable(c *gin.Context, apiKey *service.APIKey) bool {
	code, message, ok := validateAPIKeyGroupAvailable(apiKey)
	if ok {
		return false
	}
	service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonAPIKeyGroupUnavailable)
	markOpsAPIKeyAuthDiagnostic(c, code, strings.ToLower(code), apiKey, nil)
	AbortWithError(c, 403, code, message)
	return true
}

func abortIfAPIKeyGroupNotAllowed(c *gin.Context, apiKey *service.APIKey) bool {
	if validateAPIKeyGroupAllowed(apiKey) {
		return false
	}
	service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonAPIKeyGroupUnavailable)
	markOpsAPIKeyAuthDiagnostic(c, "GROUP_NOT_ALLOWED", "group_not_allowed", apiKey, nil)
	AbortWithError(c, 403, "GROUP_NOT_ALLOWED", localizedAPIKeyAuthMessage("GROUP_NOT_ALLOWED"))
	return true
}

func markOpsAPIKeyAuthDiagnostic(c *gin.Context, code, reason string, apiKey *service.APIKey, extra map[string]string) {
	if c == nil {
		return
	}
	code = strings.TrimSpace(code)
	reason = strings.TrimSpace(reason)
	message := opsAPIKeyAuthDiagnosticMessage(code, reason, apiKey)
	detail := map[string]string{
		"source": "api_key_auth",
	}
	if code != "" {
		detail["code"] = code
	}
	if reason != "" {
		detail["reason"] = reason
	}
	if message != "" {
		detail["message"] = message
	}
	if apiKey != nil {
		detail["api_key_id"] = strconv.FormatInt(apiKey.ID, 10)
		detail["api_key_status"] = strings.TrimSpace(apiKey.Status)
		if prefix := apiKeyPrefixForOps(apiKey.Key); prefix != "" {
			detail["api_key_prefix"] = prefix
		}
		if apiKey.GroupID != nil {
			detail["group_id"] = strconv.FormatInt(*apiKey.GroupID, 10)
		}
		if apiKey.Group != nil {
			detail["group_status"] = strings.TrimSpace(apiKey.Group.Status)
			detail["group_platform"] = strings.TrimSpace(apiKey.Group.Platform)
		}
		if apiKey.User != nil {
			detail["user_id"] = strconv.FormatInt(apiKey.User.ID, 10)
			detail["user_status"] = strings.TrimSpace(apiKey.User.Status)
			detail["user_balance"] = fmt.Sprintf("%.4f", apiKey.User.Balance)
		}
	}
	for k, v := range extra {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k != "" && v != "" {
			detail[k] = v
		}
	}
	raw, _ := json.Marshal(detail)
	service.SetOpsDiagnostic(c, message, string(raw))
}

func opsAPIKeyAuthDiagnosticMessage(code, reason string, apiKey *service.APIKey) string {
	switch code {
	case "api_key_in_query_deprecated":
		return "API Key 传递方式已废弃：请求使用了 URL 查询参数，请改用请求头"
	case "API_KEY_REQUIRED":
		return "缺少 API Key：请求头未提供 Authorization Bearer、x-api-key 或 x-goog-api-key"
	case "INVALID_API_KEY":
		return "API Key 无效：未找到匹配的 Key，可能填错、已删除或使用了其它平台的 Key"
	case "INTERNAL_ERROR":
		return "验证 API Key 失败：Key 查询阶段出现内部错误"
	case "API_KEY_DISABLED":
		return fmt.Sprintf("API Key 已停用：当前 Key 状态为 %s", apiKeyStatusForMessage(apiKey))
	case "ACCESS_DENIED":
		return "访问被拒绝：当前客户端 IP 未通过 API Key 白名单/黑名单限制"
	case "USER_NOT_FOUND":
		return "API Key 关联用户不存在：Key 已加载但没有可用用户记录"
	case "USER_INACTIVE":
		return fmt.Sprintf("用户账户未启用：API Key 关联用户状态为 %s", userStatusForMessage(apiKey))
	case "SUBSCRIPTION_NOT_FOUND":
		return "订阅不可用：当前分组没有可用订阅"
	case "USAGE_LIMIT_EXCEEDED":
		return "套餐额度已用完：订阅窗口额度检查未通过"
	case "SUBSCRIPTION_INVALID":
		return "订阅状态不可用：订阅校验未通过"
	case "API_KEY_QUOTA_EXHAUSTED":
		return "API Key 额度已用完：Key 状态或累计用量已达到限额"
	case "API_KEY_EXPIRED":
		return "API Key 已过期：Key 状态或过期时间已失效"
	case "INSUFFICIENT_BALANCE":
		return "账户余额不足：用户余额 <= 0，网关在本地计费阶段拒绝请求"
	case "GROUP_DELETED":
		return "API Key 所属分组已删除：分组状态不可用于调度"
	case "GROUP_DISABLED":
		return "API Key 所属分组已停用：分组状态不可用于调度"
	case "GROUP_NOT_ALLOWED":
		return "API Key 所属专属分组不允许当前用户使用"
	default:
		if reason != "" {
			return "API Key 鉴权失败：" + reason
		}
		return "API Key 鉴权失败"
	}
}

func apiKeyStatusForMessage(apiKey *service.APIKey) string {
	if apiKey == nil || strings.TrimSpace(apiKey.Status) == "" {
		return "unknown"
	}
	return strings.TrimSpace(apiKey.Status)
}

func userStatusForMessage(apiKey *service.APIKey) string {
	if apiKey == nil || apiKey.User == nil || strings.TrimSpace(apiKey.User.Status) == "" {
		return "unknown"
	}
	return strings.TrimSpace(apiKey.User.Status)
}

func apiKeyPrefixForOps(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return key
	}
	return key[:8]
}

func queryKeyName(value, name string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return name
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func validateAPIKeyGroupAllowed(apiKey *service.APIKey) bool {
	if apiKey == nil || apiKey.GroupID == nil || apiKey.User == nil || apiKey.Group == nil {
		return true
	}
	group := apiKey.Group
	if group.IsSubscriptionType() {
		return true
	}
	return apiKey.User.CanBindGroup(group.ID, group.IsExclusive)
}

func localizedAPIKeyAuthMessage(code string) string {
	switch strings.TrimSpace(code) {
	case "api_key_in_query_deprecated":
		return "不再支持通过 URL 查询参数传递 API Key，请改用 Authorization 请求头"
	case "API_KEY_REQUIRED":
		return "缺少 API Key，请在 Authorization Bearer、x-api-key 或 x-goog-api-key 中提供"
	case "INVALID_API_KEY":
		return "API Key 无效"
	case "INTERNAL_ERROR":
		return "验证 API Key 失败，请稍后重试"
	case "API_KEY_DISABLED":
		return "API Key 已停用"
	case "USER_NOT_FOUND":
		return "API Key 关联的用户不存在"
	case "USER_INACTIVE":
		return "用户账户未启用"
	case "SUBSCRIPTION_NOT_FOUND":
		return "当前分组没有可用订阅，请联系管理员开通或续费"
	case "INSUFFICIENT_BALANCE":
		return "当前账户余额不足，请充值后重试，充值地址：https://aixlau.me/purchase"
	case "GROUP_NOT_ALLOWED":
		return "API Key 所属专属分组不再允许当前用户使用"
	default:
		return ""
	}
}

func localizedAccessDeniedMessage(clientIP string) string {
	clientIP = strings.TrimSpace(clientIP)
	if clientIP == "" {
		clientIP = "unknown"
	}
	return fmt.Sprintf("访问被拒绝，当前 IP 为 %s", clientIP)
}

func localizedSubscriptionErrorMessage(err error) string {
	switch infraerrors.Reason(err) {
	case "DAILY_LIMIT_EXCEEDED":
		return "套餐今日额度已用完，请稍后再试或切换账号"
	case "WEEKLY_LIMIT_EXCEEDED":
		return "套餐本周额度已用完，请稍后再试或切换账号"
	case "MONTHLY_LIMIT_EXCEEDED":
		return "套餐本月额度已用完，请续费或切换账号"
	case "SUBSCRIPTION_EXPIRED":
		return "当前订阅已过期，请续费或切换账号"
	case "SUBSCRIPTION_SUSPENDED":
		return "当前订阅已暂停，请联系管理员"
	default:
		msg := strings.TrimSpace(infraerrors.Message(err))
		if msg != "" {
			return msg
		}
		return "订阅状态不可用，请联系管理员"
	}
}

func validateAPIKeyGroupAvailable(apiKey *service.APIKey) (string, string, bool) {
	if apiKey == nil || apiKey.GroupID == nil {
		return "", "", true
	}
	group := apiKey.Group
	if group == nil || strings.EqualFold(group.Status, "deleted") {
		return "GROUP_DELETED", "API Key 所属分组已删除", false
	}
	if !group.IsActive() {
		return "GROUP_DISABLED", "API Key 所属分组已停用", false
	}
	return "", "", true
}
