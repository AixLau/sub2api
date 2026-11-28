package openai

import "strings"

// AllowedClientEntry 描述一个被额外放行的非官方 Codex 客户端签名。
// 默认要求 Originator 精确匹配且 UAContains 中每项都出现在 User-Agent 中。
// MatchUAOnly 显式启用时忽略 Originator，并将 UAContains 的唯一一项用作 UA 前缀。
// SkipEngineFingerprint 命中条目后跳过引擎指纹门。黑名单忽略这两个布尔字段。
type AllowedClientEntry struct {
	Originator            string   `json:"originator"`
	UAContains            []string `json:"ua_contains"`
	MatchUAOnly           bool     `json:"match_ua_only,omitempty"`
	SkipEngineFingerprint bool     `json:"skip_engine_fingerprint"`
}

// IsWhitelistable 检查白名单条目的结构，避免保存永不命中或缺少匹配条件的规则。
// 黑名单使用独立的 OR 匹配规则，不受此校验约束。
func (e AllowedClientEntry) IsWhitelistable() bool {
	if e.MatchUAOnly {
		return e.uaOnlyPrefix() != ""
	}
	if normalizeCodexClientHeader(e.Originator) == "" {
		return false
	}
	if len(e.UAContains) == 0 {
		return false
	}
	for _, marker := range e.UAContains {
		if normalizeCodexClientHeader(marker) == "" {
			return false
		}
	}
	return true
}

func (e AllowedClientEntry) uaOnlyPrefix() string {
	if len(e.UAContains) != 1 {
		return ""
	}
	return normalizeCodexClientHeader(e.UAContains[0])
}

// IsAllowedClientMatch 判断请求头是否命中给定的额外客户端签名。
// 仅在条目显式启用 MatchUAOnly 时使用 UA 前缀匹配，否则使用原有双条件规则。
func IsAllowedClientMatch(userAgent, originator string, entry AllowedClientEntry) bool {
	if entry.MatchUAOnly {
		prefix := entry.uaOnlyPrefix()
		return prefix != "" && strings.HasPrefix(normalizeCodexClientHeader(userAgent), prefix)
	}
	wantOriginator := normalizeCodexClientHeader(entry.Originator)
	if wantOriginator == "" {
		return false
	}
	if normalizeCodexClientHeader(originator) != wantOriginator {
		return false
	}
	// 预设必须声明 UA 特征：否则将退化为仅凭可伪造的 originator 单因子匹配。
	if len(entry.UAContains) == 0 {
		return false
	}
	ua := normalizeCodexClientHeader(userAgent)
	for _, marker := range entry.UAContains {
		normalizedMarker := normalizeCodexClientHeader(marker)
		if normalizedMarker == "" {
			// 空白 marker 让该项失去校验能力，会让双因子退化为仅 originator
			// 单因子；视为非法配置，安全失败。
			return false
		}
		if !strings.Contains(ua, normalizedMarker) {
			return false
		}
	}
	return true
}

// MatchClientEntry 返回命中的首条白名单规则，供调用方读取条目级配置。
func MatchClientEntry(userAgent, originator string, entries []AllowedClientEntry) (AllowedClientEntry, bool) {
	for _, e := range entries {
		if IsAllowedClientMatch(userAgent, originator, e) {
			return e, true
		}
	}
	return AllowedClientEntry{}, false
}

// MatchClientEntries 判断请求头是否命中任一白名单条目。
// 用于 codex_cli_only 全局白名单：放行官方集未覆盖的 app-server 新 client。
func MatchClientEntries(userAgent, originator string, entries []AllowedClientEntry) bool {
	_, ok := MatchClientEntry(userAgent, originator, entries)
	return ok
}

// IsDeniedClientMatch 黑名单单条 OR 语义：已声明字段中任一命中即 deny。
// originator 精确等值命中，或任一非空 ua_contains marker 出现在 UA 中。
// 全空字段（originator 与 ua_contains 均空）→ 不 deny（安全忽略）。
// 与白名单 AND 非对称：deny 应宽（挡可疑），allow 应严（防伪造）。
func IsDeniedClientMatch(userAgent, originator string, entry AllowedClientEntry) bool {
	if want := normalizeCodexClientHeader(entry.Originator); want != "" {
		if normalizeCodexClientHeader(originator) == want {
			return true
		}
	}
	ua := normalizeCodexClientHeader(userAgent)
	for _, marker := range entry.UAContains {
		if m := normalizeCodexClientHeader(marker); m != "" && strings.Contains(ua, m) {
			return true
		}
	}
	return false
}

// MatchDenyEntries 判断请求头是否命中任一黑名单条目（OR）。
func MatchDenyEntries(userAgent, originator string, entries []AllowedClientEntry) bool {
	for _, e := range entries {
		if IsDeniedClientMatch(userAgent, originator, e) {
			return true
		}
	}
	return false
}
