package bridge

import _ "embed"

//go:embed native_plan.js
var nativePlanScript string

// Code-mode clients declare functions.exec rather than every nested tool.
// The fixed client program resolves update_plan from the runtime's actual
// enabled tools and invokes the native handler; it never fabricates plan state.
func (r *Request) nativePlan(item object) (object, bool, error) {
	if !r.catalog.hasDiscoveryRuntime() {
		return nil, false, nil
	}
	args := item["arguments"]
	if isTextValue(args) {
		args = []byte(stringValue(args))
	}
	params, err := parseToolObject(args, "plan_arguments")
	if err != nil {
		return nil, true, err
	}
	program := nativePlanScript + "\nawait bpsClientUpdatePlan(" + string(encoded(params)) + ");\n"
	return object{"tool": encoded("functions.exec"), "args": encoded(program)}, true, nil
}
