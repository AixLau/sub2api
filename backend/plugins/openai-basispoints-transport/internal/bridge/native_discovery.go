package bridge

import (
	_ "embed"
	"strings"
)

//go:embed native_discovery.js
var nativeDiscoveryScript string

func (c catalog) hasDiscoveryRuntime() bool {
	runtime, ok := c.tools["functions.exec"]
	return ok && runtime.Custom && strings.Contains(runtime.Description, "ALL_TOOLS") && strings.Contains(runtime.Description, "text(") && strings.Contains(runtime.Description, "tools")
}

// Translate only explicit discovery operations into the declared client runtime.
// Client permissions still apply; no model-authored code enters this program.
func (r *Request) nativeDiscovery(item object) (object, bool, error) {
	if stringValue(item["type"]) != "function_call" {
		return nil, false, nil
	}
	name := strings.TrimPrefix(qualifiedCallName(item), "functions.")
	allowed := map[string]string{
		"list_skills":          "limit cursor",
		"read_skills":          "skill_ids mode offset file_paths",
		"list_connectors":      "cursor page_size summary",
		"run_connector_action": "action_ref params summary",
	}
	fields, recognized := allowed[name]
	if !recognized {
		return nil, false, nil
	}
	// A custom tool named exec alone is not evidence of the Codex runtime.
	if !r.catalog.hasDiscoveryRuntime() {
		return nil, false, nil
	}
	args := item["arguments"]
	if isTextValue(args) {
		args = []byte(stringValue(args))
	}
	params, err := parseToolObject(args, "discovery_arguments")
	if err != nil {
		return nil, true, err
	}
	for field := range params {
		if !strings.Contains(" "+fields+" ", " "+field+" ") {
			return nil, true, toolValidationError("upstream_tool_discovery", "unknown_argument", "原生发现工具包含未声明参数")
		}
	}
	config := map[string]any{"operation": name, "arguments": params, "skills_known": r.skillsKnown, "skills": r.skills}
	program := nativeDiscoveryScript + "\nawait bpsClientDiscovery(" + string(encoded(config)) + ");\n"
	return object{"tool": encoded("functions.exec"), "args": encoded(program)}, true, nil
}
