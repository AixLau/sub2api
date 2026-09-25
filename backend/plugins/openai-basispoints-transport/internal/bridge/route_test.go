package bridge

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// get_weather is a plain client function tool used across the non-trigger cases.
const routeClientTools = `[{"type":"function","name":"get_weather","parameters":{"type":"object"}}]`

func TestNeedsNativeUpstream(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		// ---- image_input triggers: an input_image part anywhere in input ----
		{
			name: "image_input data URL in message content",
			body: `{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"},{"type":"input_image","image_url":"data:image/png;base64,AAA"}]}]}`,
			want: RouteImageInput,
		},
		{
			name: "image_input https URL in message content",
			body: `{"input":[{"type":"message","role":"user","content":[{"type":"input_image","image_url":"https://example.test/a.png"}]}]}`,
			want: RouteImageInput,
		},
		{
			name: "image_input url dict form",
			body: `{"input":[{"type":"message","role":"user","content":[{"type":"input_image","image_url":{"url":"data:image/png;base64,AAA"}}]}]}`,
			want: RouteImageInput,
		},
		{
			name: "image_input file_id form",
			body: `{"input":[{"type":"message","role":"user","content":[{"type":"input_image","file_id":"file_1"}]}]}`,
			want: RouteImageInput,
		},
		{
			name: "image_input nested in function_call_output output array",
			body: `{"input":[{"type":"function_call_output","call_id":"call_1","output":[{"type":"input_image","image_url":"data:image/png;base64,AAA"}]}]}`,
			want: RouteImageInput,
		},
		{
			name: "image_input deep in nested content",
			body: `{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]},{"type":"message","role":"assistant","content":[{"type":"input_image","image_url":"https://example.test/b.png"}]}]}`,
			want: RouteImageInput,
		},

		// ---- image_generation triggers: tool declaration or history item ----
		{
			name: "image_generation tool declaration",
			body: `{"tools":[{"type":"image_generation"}],"input":[]}`,
			want: RouteImageGeneration,
		},
		{
			name: "image_generation tool nested in namespace",
			body: `{"tools":[{"type":"namespace","name":"media","tools":[{"type":"image_generation"}]}],"input":[]}`,
			want: RouteImageGeneration,
		},
		{
			name: "image_generation tool nested in namespace children spelling",
			body: `{"tools":[{"type":"namespace","name":"media","children":[{"type":"image_generation"}]}],"input":[]}`,
			want: RouteImageGeneration,
		},
		{
			name: "image_generation history item type",
			body: `{"input":[{"type":"image_generation","id":"ig_1","status":"completed"}]}`,
			want: RouteImageGeneration,
		},
		{
			name: "image_generation history item id prefix",
			body: `{"input":[{"type":"reasoning","id":"ig_only","encrypted_content":"x"}]}`,
			want: RouteImageGeneration,
		},

		// ---- hosted_tool_choice triggers: forced name not a declared client tool ----
		{
			name: "hosted_tool_choice forces web_search by name",
			body: `{"tools":` + routeClientTools + `,"tool_choice":{"type":"function","name":"web_search"},"input":[]}`,
			want: RouteHostedToolChoice,
		},
		{
			name: "hosted_tool_choice forces image_generation by name",
			body: `{"tools":` + routeClientTools + `,"tool_choice":{"type":"function","name":"image_generation"},"input":[]}`,
			want: RouteHostedToolChoice,
		},
		{
			name: "hosted_tool_choice forces hosted tool by type without name",
			body: `{"tools":` + routeClientTools + `,"tool_choice":{"type":"web_search"},"input":[]}`,
			want: RouteHostedToolChoice,
		},
		{
			name: "hosted_tool_choice forces undeclared function name",
			body: `{"tools":` + routeClientTools + `,"tool_choice":{"type":"function","name":"nope"},"input":[]}`,
			want: RouteHostedToolChoice,
		},
		{
			name: "hosted_tool_choice forced bare name matches only namespaced tool",
			body: `{"tools":[{"type":"namespace","name":"math","tools":[{"type":"function","name":"add"}]}],"tool_choice":{"type":"function","name":"add"},"input":[]}`,
			want: RouteHostedToolChoice,
		},
		{
			name: "hosted_tool_choice string forces undeclared tool name",
			body: `{"tools":` + routeClientTools + `,"tool_choice":"web_search","input":[]}`,
			want: RouteHostedToolChoice,
		},
		{
			name: "hosted_tool_choice forces with no tools declared",
			body: `{"tool_choice":{"type":"function","name":"web_search"},"input":[]}`,
			want: RouteHostedToolChoice,
		},

		// ---- structured_output triggers: text.format.type or response_format.type not text ----
		{
			name: "structured_output text.format json_schema",
			body: `{"text":{"format":{"type":"json_schema","name":"x","schema":{}}},"input":[]}`,
			want: RouteStructuredOutput,
		},
		{
			name: "structured_output text.format json_object",
			body: `{"text":{"format":{"type":"json_object"}},"input":[]}`,
			want: RouteStructuredOutput,
		},
		{
			name: "structured_output response_format json_object",
			body: `{"response_format":{"type":"json_object"},"input":[]}`,
			want: RouteStructuredOutput,
		},
		{
			name: "structured_output response_format json_schema",
			body: `{"response_format":{"type":"json_schema","schema":{}},"input":[]}`,
			want: RouteStructuredOutput,
		},

		// ---- explicit non-triggers: stay on BPS ----
		{
			name: "plain text with client tools and tool_choice auto",
			body: `{"tools":` + routeClientTools + `,"tool_choice":"auto","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`,
			want: "",
		},
		{
			name: "web_search declared but not forced does not route",
			body: `{"tools":[{"type":"web_search","external_web_access":false},{"type":"function","name":"get_weather","parameters":{"type":"object"}}],"tool_choice":"auto","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`,
			want: "",
		},
		{
			name: "web_search nested in namespace declared but not forced",
			body: `{"tools":[{"type":"namespace","name":"x","tools":[{"type":"web_search"}]},{"type":"function","name":"get_weather"}],"tool_choice":"auto","input":[]}`,
			want: "",
		},
		{
			name: "tool_choice forces a declared client function",
			body: `{"tools":` + routeClientTools + `,"tool_choice":{"type":"function","name":"get_weather"},"input":[]}`,
			want: "",
		},
		{
			name: "tool_choice forces a declared namespaced client function",
			body: `{"tools":[{"type":"namespace","name":"math","tools":[{"type":"function","name":"add"}]}],"tool_choice":{"type":"function","name":"add","namespace":"math"},"input":[]}`,
			want: "",
		},
		{
			name: "tool_choice forces a declared custom tool",
			body: `{"tools":[{"type":"custom","name":"exec","format":"text"}],"tool_choice":{"type":"function","name":"exec"},"input":[]}`,
			want: "",
		},
		{
			name: "tool_choice auto routes nowhere",
			body: `{"tools":` + routeClientTools + `,"tool_choice":"auto","input":[]}`,
			want: "",
		},
		{
			name: "tool_choice none routes nowhere",
			body: `{"tools":` + routeClientTools + `,"tool_choice":"none","input":[]}`,
			want: "",
		},
		{
			name: "tool_choice required routes nowhere",
			body: `{"tools":` + routeClientTools + `,"tool_choice":"required","input":[]}`,
			want: "",
		},
		{
			name: "text.format type text is not structured",
			body: `{"text":{"format":{"type":"text"}},"input":[]}`,
			want: "",
		},
		{
			name: "response_format type text is not structured",
			body: `{"response_format":{"type":"text"},"input":[]}`,
			want: "",
		},
		{
			name: "text without format is not structured",
			body: `{"text":{"verbosity":"low"},"input":[]}`,
			want: "",
		},
		{
			name: "empty object body",
			body: `{}`,
			want: "",
		},
		{
			name: "empty body",
			body: ``,
			want: "",
		},
		{
			name: "malformed JSON",
			body: `{not json`,
			want: "",
		},
		{
			name: "non-object JSON array",
			body: `[]`,
			want: "",
		},
		{
			name: "non-object JSON null",
			body: `null`,
			want: "",
		},
		{
			name: "non-object JSON string",
			body: `"just a string"`,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, NeedsNativeUpstream([]byte(tt.body)))
		})
	}
}

// TestNeedsNativeUpstreamPriority pins the reason when several triggers match.
// Routing itself only cares whether the reason is empty, so this documents the
// fixed reporting order rather than a behavioral requirement.
func TestNeedsNativeUpstreamPriority(t *testing.T) {
	body := `{
		"text": {"format": {"type": "json_object"}},
		"tool_choice": {"type": "function", "name": "web_search"},
		"tools": ` + routeClientTools + `,
		"input": [{"type": "message", "role": "user", "content": [{"type": "input_image", "image_url": "data:image/png;base64,AAA"}]}]
	}`
	require.Equal(t, RouteImageInput, NeedsNativeUpstream([]byte(body)))
}

func TestAdditionalToolsCarrierRouting(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "image_generation 声明在 additional_tools 中触发路由",
			body: `{"model":"gpt-5.5","input":[{"type":"additional_tools","tools":[{"type":"image_generation"}]},{"role":"user","content":"hi"}]}`,
			want: RouteImageGeneration,
		},
		{
			name: "additional_tools 声明的客户端工具被强制时不触发",
			body: `{"model":"gpt-5.5","tool_choice":{"type":"function","name":"exec_command"},"input":[{"type":"additional_tools","tools":[{"type":"function","name":"exec_command"}]},{"role":"user","content":"hi"}]}`,
			want: "",
		},
		{
			name: "additional_tools 未声明的强制托管工具触发路由",
			body: `{"model":"gpt-5.5","tool_choice":{"type":"web_search"},"input":[{"type":"additional_tools","tools":[{"type":"function","name":"exec_command"}]},{"role":"user","content":"hi"}]}`,
			want: RouteHostedToolChoice,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, NeedsNativeUpstream([]byte(tc.body)))
		})
	}
}

func TestRequestsClientTools(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"顶层 function 声明", `{"tools":[{"type":"function","name":"exec_command"}],"input":"hi"}`, true},
		{"additional_tools 载体声明", `{"input":[{"type":"additional_tools","tools":[{"type":"function","name":"exec_command"}]},{"role":"user","content":"hi"}]}`, true},
		{"命名空间内声明", `{"tools":[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"exec"}]}],"input":"hi"}`, true},
		{"只有托管工具", `{"tools":[{"type":"web_search"},{"type":"image_generation"}],"input":"hi"}`, false},
		{"无工具", `{"input":"hi"}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, RequestsClientTools([]byte(tc.body)))
		})
	}
}
