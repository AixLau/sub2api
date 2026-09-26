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
	TargetIssue   string `json:"target_issue,omitempty"`
	SuggestedTool string `json:"suggested_tool,omitempty"`
}

// Classify invalid targets without echoing caller-controlled unknown text.
// A suggested name is copied only from the authoritative client catalog.
// Suggestions never select a tool or rewrite executable payloads.
func (c catalog) targetIssue(key string) (issue, suggested string) {
	if isTransportName(key) {
		return "transport_executor", ""
	}
	if canonical, exists := c.byBareName[key]; exists {
		if canonical == "" {
			return "ambiguous_bare_name", ""
		}
		return "missing_namespace", diagnosticIdentifier(canonical)
	}
	for _, t := range c.tools {
		if t.Namespace != "" && key == t.Namespace {
			return "namespace_only", ""
		}
	}
	return "undeclared_target", ""
}

func (c catalog) targetError(item object, key string) error {
	issue, suggested := c.targetIssue(key)
	message := "上游工具 信封 name 指定了未声明的工具；必须使用客户端目录中的完整名称（含命名空间），不能直接引用工具描述中的嵌套工具"
	if issue == "missing_namespace" && suggested != "<redacted>" {
		message = "上游工具 信封 name 缺少命名空间；应使用客户端已声明的完整名称 " + suggested
	}
	return &ToolCallError{message: message, stage: "upstream_tool_envelope", reason: "undeclared_target",
		diagnostics: &toolCallDiagnostics{Stage: "upstream_tool_envelope", CallType: diagnosticIdentifier(stringValue(item["type"])),
			Name: diagnosticIdentifier(stringValue(item["name"])), Namespace: diagnosticIdentifier(stringValue(item["namespace"])),
			QualifiedName: diagnosticIdentifier(qualifiedCallName(item)), CatalogTools: len(c.tools), TargetIssue: issue, SuggestedTool: suggested}}
}

func toolIdentityError(item object, catalogTools int) error {
	return &ToolCallError{
		message: "上游工具既不是传输执行器，也没有匹配本轮客户端工具或已支持的发现适配",
		stage:   "upstream_tool_capability", reason: "unsupported_native_tool",
		diagnostics: &toolCallDiagnostics{
			Stage:         "upstream_tool_capability",
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
