package bridge

import (
	"context"
	"encoding/json"
	"errors"
)

// Diagnostic contains structural metadata only. Never add raw arguments,
// input/code, summaries, headers, schemas, or arbitrary error text here.
type Diagnostic struct {
	SourceEvent     string `json:"source_event"`
	ResponseID      string `json:"response_id,omitempty"`
	Stage           string `json:"stage"`
	Reason          string `json:"reason"`
	CallType        string `json:"call_type,omitempty"`
	Name            string `json:"name,omitempty"`
	Namespace       string `json:"namespace,omitempty"`
	QualifiedName   string `json:"qualified_name,omitempty"`
	CatalogTools    int    `json:"catalog_tools"`
	Field           string `json:"field,omitempty"`
	JSONOffset      int64  `json:"json_offset,omitempty"`
	ArgumentsType   string `json:"arguments_type,omitempty"`
	ArgumentsBytes  int    `json:"arguments_bytes,omitempty"`
	ReferencesType  string `json:"references_type,omitempty"`
	ReferencesCount int    `json:"references_count,omitempty"`
	TargetTool      string `json:"target_tool,omitempty"`
	TargetDeclared  bool   `json:"target_declared"`
	TargetIssue     string `json:"target_issue,omitempty"`
	SuggestedTool   string `json:"suggested_tool,omitempty"`
	CodeType        string `json:"code_type,omitempty"`
	CodeBytes       int    `json:"code_bytes,omitempty"`
	CodeJSONType    string `json:"code_json_type,omitempty"`
}

type diagnosticObserverKey struct{}

// WithDiagnosticObserver observes failures even when Stream converts them to
// response.failed and returns nil. The observer must not affect forwarding.
func WithDiagnosticObserver(ctx context.Context, observe func(Diagnostic)) context.Context {
	return context.WithValue(ctx, diagnosticObserverKey{}, observe)
}

func (r *Request) observeCallFailure(ctx context.Context, item object, err error) {
	observe, _ := ctx.Value(diagnosticObserverKey{}).(func(Diagnostic))
	if observe == nil {
		return
	}
	var callErr *ToolCallError
	if !errors.As(err, &callErr) {
		// I/O and KV failures are logged by the transport with their own error
		// code, never mislabeled as an invalid model-generated tool call.
		return
	}
	d := Diagnostic{Stage: "upstream_tool_validation", Reason: "validation_failed",
		SourceEvent: r.sourceEvent, ResponseID: diagnosticIdentifier(r.responseID),
		CallType:      diagnosticIdentifier(stringValue(item["type"])),
		Name:          diagnosticIdentifier(stringValue(item["name"])),
		Namespace:     diagnosticIdentifier(stringValue(item["namespace"])),
		QualifiedName: diagnosticIdentifier(qualifiedCallName(item)), CatalogTools: len(r.catalog.tools)}
	if d.SourceEvent == "" {
		d.SourceEvent = "json_response"
	}
	if callErr.stage != "" {
		d.Stage = callErr.stage
	}
	if callErr.reason != "" {
		d.Reason = callErr.reason
	}
	if callErr.diagnostics != nil {
		d.TargetIssue, d.SuggestedTool = callErr.diagnostics.TargetIssue, callErr.diagnostics.SuggestedTool
	}
	d.Field, d.JSONOffset = callErr.field, callErr.jsonOffset
	args := item["arguments"]
	d.ArgumentsType = jsonKind(args)
	if isTextValue(args) {
		args = []byte(stringValue(args))
	}
	d.ArgumentsBytes = len(args)
	// Only inspect transport fields for an actual relay. Direct function
	// arguments can have business fields named code/references.
	if isTransportName(qualifiedCallName(item)) {
		if outer, parseErr := parseObject(args); parseErr == nil {
			d.ReferencesType = jsonKind(outer["references"])
			var refs []json.RawMessage
			if json.Unmarshal(outer["references"], &refs) == nil {
				d.ReferencesCount = len(refs)
			}
			d.CodeType = jsonKind(outer["code"])
			if isTextValue(outer["code"]) {
				code := []byte(stringValue(outer["code"]))
				d.CodeBytes = len(code)
				if target, key, targetErr := r.catalog.transportTarget(item, outer); targetErr == nil {
					d.TargetDeclared = true
					d.TargetTool = diagnosticIdentifier(key)
					if target.Custom {
						d.CodeJSONType = "raw_custom_input"
					} else {
						d.CodeJSONType = jsonKind(code)
					}
				}
			}
		}
	}
	observe(d)
}

// jsonKind reports only a fixed label; invalid JSON never reaches an error
// formatter that could reveal a source fragment or secret.
func jsonKind(raw []byte) string {
	if len(raw) == 0 {
		return "missing"
	}
	if !json.Valid(raw) {
		return "invalid"
	}
	for _, c := range raw {
		switch c {
		case ' ', '\n', '\r', '\t':
			continue
		case '{':
			return "object"
		case '[':
			return "array"
		case '"':
			return "string"
		case 'n':
			return "null"
		case 't', 'f':
			return "boolean"
		default:
			return "number"
		}
	}
	return "invalid"
}
