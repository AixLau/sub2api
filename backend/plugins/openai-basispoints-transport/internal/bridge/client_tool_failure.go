package bridge

import (
	"context"
	"encoding/json"
)

// Code-mode clients can carry rejected calls through their ordinary tool loop.
// The fixed program only emits bridge-owned failure data: it never evaluates
// the rejected payload, calls a client handler, or fabricates execution success.
func (r *Request) canDeliverToolFailures() bool {
	return r.catalog.hasDiscoveryRuntime() && r.catalog.choice != "none" &&
		(r.catalog.forced == "" || r.catalog.forced == "functions.exec")
}

func (r *Request) clientToolFailures(ctx context.Context, output []json.RawMessage, failures map[int]*ToolCallError) ([]json.RawMessage, error) {
	converted := make([]json.RawMessage, len(output))
	for i, raw := range output {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		item, _ := parseObject(raw)
		if !isToolCall(item) {
			converted[i] = raw
			continue
		}
		// Keep the whole rejected batch unexecuted, including valid siblings.
		// Decode as the declared client tool to retain tool_choice checks, item
		// IDs and aliases. Never invoke a client tool also named run_officejs.
		// saveCall still stores the ORIGINAL rejected item for upstream replay.
		relay := object{"type": encoded("custom_tool_call"), "name": encoded("functions.exec"),
			"id": item["id"], "call_id": item["call_id"],
			"input": encoded("text(" + string(encoded(toolFailureResult(failures[i]))) + ");\n")}
		if status := item["status"]; len(status) > 0 {
			relay["status"] = status
		}
		var err error
		converted[i], err = r.decodeCall(ctx, encoded(relay))
		if err != nil {
			return nil, err
		}
	}
	return converted, nil
}

// Share the same honest, non-executable result between client delivery and
// server feedback for clients without a declared result runtime.
func toolFailureResult(failure *ToolCallError) object {
	detail := object{"code": encoded("TOOL_BATCH_NOT_EXECUTED"), "message": encoded("This call was not executed because another call in the same batch could not be converted. Submit any still-needed calls again with references=[\"client-tool:FULL_CATALOG_NAME\"] and the raw payload in code.")}
	if failure != nil {
		detail = object{"code": encoded("TOOL_BRIDGE_CONVERSION_FAILED"), "stage": encoded(failure.stage), "reason": encoded(failure.reason), "message": encoded(failure.Error())}
		if failure.stage == "upstream_tool_capability" {
			detail["code"] = encoded(FailureCode(failure))
			detail["message"] = encoded("This tool is unavailable in the current client catalog and was not executed. Continue using only declared client tools through run_officejs with references=[\"client-tool:FULL_CATALOG_NAME\"] and the catalog's payload format, or explain the missing capability. Do not repeat the unavailable call or claim it succeeded.")
		}
		if failure.diagnostics != nil {
			detail["diagnostics"] = encoded(failure.diagnostics)
		}
	}
	detail["payload_format"] = encoded("FUNCTION code must be a YAML argument mapping ({} for no arguments), never JavaScript. CUSTOM code must be the exact raw input. Use only the current client catalog; no tool was executed by this result.")
	return object{"success": encoded(false), "executed": encoded(false), "error": encoded(detail)}
}
