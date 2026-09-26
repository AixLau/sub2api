package bridge

import "strings"

// A name already qualified by its explicit namespace must not be prefixed
// again. The original item is kept intact for replay; this is a lookup key.
func qualifiedCallName(item object) string {
	name := stringValue(item["name"])
	ns := stringValue(item["namespace"])
	if ns == "" || name == "" || strings.HasPrefix(name, ns+".") {
		return name
	}
	return ns + "." + name
}

// Diagnostic metadata is deliberately separate from the executable item. Do
// not add arguments, input, summary, references, IDs or catalog contents here.
type toolCallDiagnostics struct {
	Stage         string `json:"stage"`
	CallType      string `json:"call_type"`
	Name          string `json:"name"`
	Namespace     string `json:"namespace,omitempty"`
	QualifiedName string `json:"qualified_name"`
	CatalogTools  int    `json:"catalog_tools"`
}

func toolIdentityError(item object, catalogTools int) error {
	return &ToolCallError{
		message: "上游调用不是客户端已声明的工具或 run_officejs 传输执行器",
		diagnostics: &toolCallDiagnostics{
			Stage:         "upstream_tool_identity",
			CallType:      diagnosticIdentifier(stringValue(item["type"])),
			Name:          diagnosticIdentifier(stringValue(item["name"])),
			Namespace:     diagnosticIdentifier(stringValue(item["namespace"])),
			QualifiedName: diagnosticIdentifier(qualifiedCallName(item)),
			CatalogTools:  catalogTools,
		},
	}
}

// Names are useful for distinguishing a genuine Office call from a naming
// mismatch. Bound their size and syntax instead of logging arbitrary text
// supplied in an identity field. Redact the whole value, never truncate it.
func diagnosticIdentifier(value string) string {
	if len(value) > 128 {
		return "<redacted>"
	}
	for _, c := range value {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.' {
			continue
		}
		return "<redacted>"
	}
	return value
}
