package clientmsg

import "strings"

// Localize translates common client-facing English messages emitted by the
// gateway itself. It intentionally leaves unknown messages unchanged so
// upstream passthrough bodies and detailed provider messages are preserved.
func Localize(message string) string {
	msg := strings.TrimSpace(message)
	if msg == "" {
		return message
	}
	if localized, ok := exactMessages[msg]; ok {
		return localized
	}
	if localized, ok := localizePattern(msg); ok {
		return localized
	}
	for _, rule := range prefixMessages {
		if strings.HasPrefix(msg, rule.prefix) {
			return rule.replacement + strings.TrimSpace(strings.TrimPrefix(msg, rule.prefix))
		}
	}
	return message
}

type prefixRule struct {
	prefix      string
	replacement string
}

var exactMessages = map[string]string{
	"accepted": "已接受",

	"Authorization required":                                 "需要登录或提供授权信息",
	"Authorization header is required":                       "缺少 Authorization 请求头",
	"Authorization header format must be 'Bearer {token}'":   "Authorization 请求头格式必须为 Bearer {token}",
	"Token cannot be empty":                                  "Token 不能为空",
	"Token has expired":                                      "Token 已过期",
	"Invalid token":                                          "Token 无效",
	"Token has been revoked (password changed)":              "Token 已失效（密码已变更）",
	"User not found":                                         "用户不存在",
	"User not found in context":                              "用户未登录或上下文已失效",
	"User context not found":                                 "用户上下文不存在",
	"User not authenticated":                                 "用户未登录",
	"User account is not active":                             "用户账户未启用",
	"Admin access required":                                  "需要管理员权限",
	"Invalid admin API key":                                  "管理员 API Key 无效",
	"No admin user found":                                    "未找到管理员用户",
	"Internal server error":                                  "服务器内部错误",
	"Service temporarily unavailable":                        "服务暂时不可用",
	"Backend mode is active. User self-service is disabled.": "后端托管模式已启用，用户自助服务已关闭。",
	"Backend mode is active. Registration and self-service auth flows are disabled.": "后端托管模式已启用，注册和自助认证流程已关闭。",
	"Backend mode is active. Only admin login is allowed.":                           "后端托管模式已启用，仅允许管理员登录。",

	"Invalid API key":                      "API Key 无效",
	"API key is required":                  "缺少 API Key",
	"API key is disabled":                  "API Key 已停用",
	"API key group platform is not gemini": "API Key 所属分组平台不是 Gemini",
	"Missing model in URL":                 "URL 中缺少模型名称",

	"No available accounts":                                   "暂无可用账号",
	"No available OpenAI accounts":                            "暂无可用 OpenAI 账号",
	"No available OpenAI accounts support /responses/compact": "暂无可用 OpenAI 账号支持 /responses/compact",
	"No available Gemini accounts":                            "暂无可用 Gemini 账号",
	"All available accounts exhausted":                        "所有可用账号均已耗尽",
	"Gemini compatibility service is not configured":          "Gemini 兼容服务未配置",
	"Too many pending requests, please retry later":           "待处理请求过多，请稍后重试",
	"Too many concurrent requests, please retry later":        "并发请求过多，请稍后重试",
	"Request memory budget exhausted":                         "当前请求所需资源超过可用准入配额，请稍后重试",
	"Service temporarily unavailable, please retry later":     "服务暂时不可用，请稍后重试",
	"context canceled":                                        "请求已取消",

	"Upstream request failed":                             "请求处理失败，请稍后重试",
	"Request failed":                                      "请求失败",
	"Upstream gateway error":                              "服务网关错误",
	"Upstream returned no supported models":               "上游未返回支持的模型",
	"Upstream returned an invalid non-streaming response": "上游返回了无效的非流式响应",
	"Upstream returned an empty completion without usage; no fallback account was available": "上游返回空内容且无用量信息，并且没有可用的备用账号",
	"OpenAI upstream response failed":                                  "OpenAI 上游响应失败",
	"OpenAI stream disconnected before completion":                     "OpenAI 流式响应在完成前中断",
	"Upstream compact response failed":                                 "上游 compact 响应失败",
	"Upstream authentication failed, please contact administrator":     "服务认证失败，请联系管理员",
	"Upstream authentication failed":                                   "服务认证失败",
	"Upstream access forbidden, please contact administrator":          "服务访问被拒绝，请联系管理员",
	"Upstream access forbidden":                                        "服务访问被拒绝",
	"Upstream rate limit exceeded, please retry later":                 "服务请求过于频繁，请稍后重试",
	"Upstream rate limit exceeded":                                     "服务请求过于频繁",
	"Upstream service overloaded":                                      "服务繁忙，请稍后重试",
	"Upstream service overloaded, please retry later":                  "服务繁忙，请稍后重试",
	"Upstream service temporarily unavailable":                         "服务暂时不可用，请稍后重试",
	"Upstream payment required: insufficient balance or billing issue": "服务计费失败：余额不足或存在计费问题",
	"Empty upstream response":                                          "上游响应为空",
	"Failed to read upstream stream":                                   "读取上游流失败",
	"Failed to read upstream response":                                 "读取上游响应失败",
	"Failed to parse upstream response":                                "解析上游响应失败",
	"Upstream stream ended without a response":                         "上游流结束但没有返回响应",
	"Upstream stream ended without a terminal response event":          "上游流结束但没有返回终止响应事件",

	"Failed to read request body":                                  "读取请求体失败",
	"Request body is empty":                                        "请求体为空",
	"Failed to parse request body":                                 "解析请求体失败",
	"Failed to normalize compact request body":                     "规范化 compact 请求体失败",
	"Failed to build request":                                      "构建请求失败",
	"Failed to build upstream request":                             "构建上游请求失败",
	"Failed to get access token":                                   "获取 access token 失败",
	"Failed to read response":                                      "读取响应失败",
	"model is required":                                            "缺少 model 参数",
	"Missing model":                                                "缺少模型名称",
	"Missing action in URL":                                        "URL 中缺少 action",
	"Invalid request":                                              "请求无效",
	"Invalid request body":                                         "请求体无效",
	"invalid stream field type":                                    "stream 字段类型无效",
	"Invalid or expired 2FA session":                               "两步验证会话无效或已过期",
	"Invalid payment type":                                         "支付类型无效",
	"Invalid stream value, use true or false":                      "stream 值无效，请使用 true 或 false",
	"Invalid billing_type":                                         "billing_type 无效",
	"Invalid billing_mode":                                         "billing_mode 无效",
	"Invalid model_source, user usage only supports requested":     "model_source 无效，用户用量仅支持 requested",
	"Too many API key IDs (maximum 100 allowed)":                   "API Key ID 过多（最多 100 个）",
	"Billing error":                                                "计费校验失败，请稍后重试或联系管理员",
	"Billing service temporarily unavailable. Please retry later.": "计费服务暂时不可用，请稍后重试",
	"insufficient balance":                                         "当前账户余额不足，请充值后重试，充值地址：https://aixlau.me/purchase",
	"group requests-per-minute limit exceeded":                     "当前分组请求频率过高，请稍后重试",
	"user requests-per-minute limit exceeded":                      "当前用户请求频率过高，请稍后重试",
	"Daily usage quota exhausted for this platform.":               "当前平台今日用量额度已用完，请在额度重置后重试",
	"Weekly usage quota exhausted for this platform.":              "当前平台本周用量额度已用完，请在额度重置后重试",
	"Monthly usage quota exhausted for this platform.":             "当前平台本月用量额度已用完，请在额度重置后重试",
	"subscription is invalid or expired":                           "当前套餐无效或已过期，请续费或联系管理员",
	"daily usage limit exceeded":                                   "当前套餐今日用量额度已用完，请在额度重置后重试",
	"weekly usage limit exceeded":                                  "当前套餐本周用量额度已用完，请在额度重置后重试",
	"monthly usage limit exceeded":                                 "当前套餐本月用量额度已用完，请在额度重置后重试",

	"Invalid id":                                   "ID 无效",
	"Invalid usage ID":                             "用量记录 ID 无效",
	"Invalid API key ID":                           "API Key ID 无效",
	"Invalid api_key_id":                           "api_key_id 无效",
	"Invalid group_id":                             "group_id 无效",
	"Invalid account_id":                           "account_id 无效",
	"Invalid account ID":                           "账号 ID 无效",
	"Invalid group ID":                             "分组 ID 无效",
	"Invalid limit":                                "limit 无效",
	"Account not found":                            "账号不存在",
	"Rule not found":                               "规则不存在",
	"Profile not found":                            "配置不存在",
	"Error requests view is disabled":              "错误请求查看功能已关闭",
	"Rate limit service unavailable":               "限流服务不可用",
	"Failed to list request details":               "获取请求详情失败",
	"Failed to sync upstream models from upstream": "同步上游模型失败",
	"Failed to query upstream usage":               "查询上游用量失败",

	"Not authorized to access this API key's usage records": "无权访问此 API Key 的用量记录",
	"Not authorized to access this API key's usage":         "无权访问此 API Key 的用量",
	"Not authorized to access this record":                  "无权访问此记录",

	"Image generation concurrency limit exceeded, please retry later": "图片生成并发已达上限，请稍后重试",
	"WebSocket upgrade required (Upgrade: websocket)":                 "需要 WebSocket 升级请求（Upgrade: websocket）",
	"Client disconnected before upstream response":                    "客户端在上游响应前已断开连接",
	"This group is restricted to Claude Code clients":                 "该分组仅允许 Claude Code 客户端使用",
	"This group does not allow /v1/messages dispatch":                 "该分组不允许调度 /v1/messages 请求",

	"Token counting is not supported by upstream":                           "上游不支持 token 计数",
	"Upstream response missing input_tokens":                                "上游响应缺少 input_tokens",
	"Antigravity token provider not configured":                             "Antigravity token 提供器未配置",
	"Antigravity token provider is not configured":                          "Antigravity token 提供器未配置",
	"Gemini token provider is not configured":                               "Gemini token 提供器未配置",
	"Claude token provider not configured":                                  "Claude token 提供器未配置",
	"Grok token provider not configured":                                    "Grok token 提供器未配置",
	"HTTP upstream not configured":                                          "HTTP 上游未配置",
	"Account test service is not configured":                                "账号测试服务未配置",
	"Account is required":                                                   "缺少账号",
	"Upstream HTTP client is not configured":                                "上游 HTTP 客户端未配置",
	"Antigravity API-key base URL is required for upstream model sync":      "同步上游模型需要配置 Antigravity API Key base URL",
	"Gemini Code Assist model listing is not supported by this sync button": "此同步按钮不支持 Gemini Code Assist 模型列表",

	"Only OAuth accounts support privacy setting":                        "仅 OAuth 账号支持隐私设置",
	"Only OpenAI and Antigravity OAuth accounts support privacy setting": "仅 OpenAI 和 Antigravity OAuth 账号支持隐私设置",
	"Cannot set privacy: missing access_token":                           "无法设置隐私：缺少 access_token",
	"Only Gemini OAuth accounts support tier refresh":                    "仅 Gemini OAuth 账号支持刷新套餐层级",
	"Only google_one OAuth accounts support tier refresh":                "仅 google_one OAuth 账号支持刷新套餐层级",
}

var prefixMessages = []prefixRule{
	{prefix: "Invalid request: ", replacement: "请求无效："},
	{prefix: "No available Gemini accounts: ", replacement: "暂无可用 Gemini 账号："},
	{prefix: "Upstream request failed after retries: ", replacement: "请求多次重试后失败："},
	{prefix: "Upstream request failed (status ", replacement: "请求处理失败（状态码 "},
	{prefix: "Unsupported action: ", replacement: "不支持的 action："},
	{prefix: "Request failed: ", replacement: "请求失败："},
	{prefix: "Authentication failed (401): ", replacement: "认证失败 (401)："},
	{prefix: "Unsupported account type: ", replacement: "不支持的账号类型："},
	{prefix: "Invalid ", replacement: "无效的 "},
	{prefix: "Failed to ", replacement: "操作失败："},
}

func localizePattern(msg string) (string, bool) {
	if strings.HasPrefix(msg, "Request body too large, limit is ") {
		limit := strings.TrimSpace(strings.TrimPrefix(msg, "Request body too large, limit is "))
		return "请求体过大，最大允许 " + limit, true
	}
	if strings.HasPrefix(msg, "Model ") && strings.HasSuffix(msg, " is not supported by any configured account in this group") {
		model := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(msg, "Model "), " is not supported by any configured account in this group"))
		return "该分组中没有任何已配置账号支持模型 " + model, true
	}
	if strings.HasPrefix(msg, "model ") && strings.HasSuffix(msg, " not in whitelist") {
		model := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(msg, "model "), " not in whitelist"))
		return "模型 " + model + " 不在白名单中", true
	}
	if strings.HasPrefix(msg, "body size ") && strings.Contains(msg, " exceeds limit ") {
		return strings.Replace(strings.Replace(msg, "body size ", "请求体大小 ", 1), " exceeds limit ", " 超过限制 ", 1), true
	}
	if strings.HasPrefix(msg, "Concurrency limit exceeded for ") && strings.HasSuffix(msg, ", please retry later") {
		slot := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(msg, "Concurrency limit exceeded for "), ", please retry later"))
		switch slot {
		case "user":
			slot = "用户"
		case "account":
			slot = "账号"
		case "global":
			slot = "全局"
		}
		if slot == "" {
			slot = "当前"
		}
		if slot == "当前" {
			return "当前并发请求已达上限，请稍后重试", true
		}
		return "当前" + slot + "并发请求已达上限，请稍后重试", true
	}
	return "", false
}
