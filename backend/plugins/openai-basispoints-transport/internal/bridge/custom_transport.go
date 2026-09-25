package bridge

import (
	"strings"
	"unicode"
)

const customTransportPrefix = "sub2api.custom/"
const legacyCustomSummaryPrefix = "Run client tool "

// An explicit marker distinguishes freeform input from a JSON function
// envelope. Never guess from the code's contents: even JSON-looking custom
// input must survive unchanged. convertCall still checks the catalog and kind.
func customTransportEnvelope(outer object) (object, bool, error) {
	summary := stringValue(outer["summary"])
	if !strings.HasPrefix(summary, customTransportPrefix) {
		return nil, false, nil
	}
	name := strings.TrimPrefix(summary, customTransportPrefix)
	if name == "" || strings.ContainsAny(name, "/\\") || strings.IndexFunc(name, unicode.IsSpace) >= 0 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return nil, true, toolCallError("上游 custom 工具 summary 标记必须包含完整工具名")
	}
	if !isTextValue(outer["code"]) {
		return nil, true, toolCallError("上游 custom 工具 code 必须是原文字符串")
	}
	return object{"tool": encoded(name), "args": outer["code"]}, true, nil
}

// The current Codex executor still emits this exact summary for custom calls
// even when its code field contains raw input. Accept it only as an explicit
// custom transport form; convertCall must still find the full name in the
// declared custom-tool catalog. Function tools therefore cannot use this path.
func legacyRawCustomTransportEnvelope(outer object) (object, bool, error) {
	summary := stringValue(outer["summary"])
	if !strings.HasPrefix(summary, legacyCustomSummaryPrefix) {
		return nil, false, nil
	}
	name := strings.TrimPrefix(summary, legacyCustomSummaryPrefix)
	if name == "" {
		return nil, true, toolCallError("上游 custom 工具 summary 缺少完整工具名")
	}
	if strings.IndexFunc(name, unicode.IsSpace) >= 0 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return nil, true, toolCallError("上游 custom 工具 summary 包含无效字符")
	}
	if !isTextValue(outer["code"]) {
		return nil, true, toolCallError("上游 custom 工具 code 必须是原文字符串")
	}
	return object{"tool": encoded(name), "args": outer["code"]}, true, nil
}
