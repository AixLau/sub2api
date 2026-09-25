package bridge

import (
	"strings"
	"unicode"
)

const customTransportPrefix = "sub2api.custom/"

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
