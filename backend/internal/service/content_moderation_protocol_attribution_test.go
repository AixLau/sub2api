package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSemanticReviewResponsesToolOutputOnlyKeepsContextEvidence(t *testing.T) {
	body := []byte(`{"input":[{"type":"function_call_output","call_id":"call_1","output":{"result":"deployment complete"}}]}`)
	content := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Len(t, content.Sources, 1)
	require.Equal(t, "tool", content.Sources[0].Role)

	cfg := semanticReviewTestConfig()
	cfg.Trigger = ContentModerationSemanticReviewTriggerAll
	got := buildContentModerationSemanticReviewInput(cfg, content, "")

	require.NotEmpty(t, got)
	require.Contains(t, got, "role=tool evidence=context_only")
	require.Contains(t, got, "deployment complete")
	require.NotContains(t, got, "role=user")
}

func TestSemanticReviewOpenAIChatLegacyFunctionOutputOnlyKeepsContextEvidence(t *testing.T) {
	body := []byte(`{"messages":[{"role":"function","name":"deploy","content":"deployment complete"}]}`)
	content := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)

	require.Len(t, content.Sources, 1)
	require.Equal(t, "function", content.Sources[0].Role)

	cfg := semanticReviewTestConfig()
	cfg.Trigger = ContentModerationSemanticReviewTriggerAll
	got := buildContentModerationSemanticReviewInput(cfg, content, "")

	require.NotEmpty(t, got)
	require.Contains(t, got, "role=tool evidence=context_only")
	require.Contains(t, got, "deployment complete")
	require.NotContains(t, got, "role=user")
}

func TestSemanticReviewAnthropicToolResultIsNotUserIntent(t *testing.T) {
	body := []byte(`{"messages":[
		{"role":"assistant","content":[{"type":"tool_use","id":"tool_1","name":"deploy","input":{}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool_1","content":"deployment complete"}]}
	]}`)
	content := ExtractContentModerationInput(ContentModerationProtocolAnthropicMessages, body)

	require.Len(t, content.Sources, 2)
	require.Equal(t, "assistant", content.Sources[0].Role)
	require.Equal(t, "tool", content.Sources[1].Role)
	require.Contains(t, content.Sources[1].Source, ".tool_result")

	cfg := semanticReviewTestConfig()
	cfg.Trigger = ContentModerationSemanticReviewTriggerAll
	got := buildContentModerationSemanticReviewInput(cfg, content, "")

	require.Contains(t, got, "role=tool evidence=context_only")
	require.NotContains(t, got, "role=user")
}

func TestSemanticReviewGeminiFunctionResponseIsNotUserIntent(t *testing.T) {
	body := []byte(`{"contents":[
		{"role":"model","parts":[{"functionCall":{"name":"deploy","args":{}}}]},
		{"role":"user","parts":[{"functionResponse":{"name":"deploy","response":{"result":"deployment complete"}}}]}
	]}`)
	content := ExtractContentModerationInput(ContentModerationProtocolGemini, body)

	require.Len(t, content.Sources, 2)
	require.Equal(t, "model", content.Sources[0].Role)
	require.Equal(t, "tool", content.Sources[1].Role)
	require.Contains(t, content.Sources[1].Source, ".function_response")

	cfg := semanticReviewTestConfig()
	cfg.Trigger = ContentModerationSemanticReviewTriggerAll
	got := buildContentModerationSemanticReviewInput(cfg, content, "")

	require.Contains(t, got, "role=tool evidence=context_only")
	require.NotContains(t, got, "role=user")
}

func TestProtocolToolResultsRemainSeparateFromExplicitUserText(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		body     string
	}{
		{
			name:     "anthropic mixed content",
			protocol: ContentModerationProtocolAnthropicMessages,
			body:     `{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool_1","content":"tool evidence"},{"type":"text","text":"continue deployment"}]}]}`,
		},
		{
			name:     "gemini mixed parts",
			protocol: ContentModerationProtocolGemini,
			body:     `{"contents":[{"role":"user","parts":[{"functionResponse":{"name":"deploy","response":{"result":"tool evidence"}}},{"text":"continue deployment"}]}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := ExtractContentModerationInput(tt.protocol, []byte(tt.body))
			require.Len(t, content.Sources, 2)
			require.Equal(t, "tool", content.Sources[0].Role)
			require.Equal(t, "user", content.Sources[1].Role)

			cfg := semanticReviewTestConfig()
			cfg.Trigger = ContentModerationSemanticReviewTriggerAll
			got := buildContentModerationSemanticReviewInput(cfg, content, "")

			require.Contains(t, got, "continue deployment")
			require.NotContains(t, got, "tool evidence")
		})
	}
}

func TestProtocolToolResultAttributionRespectsAuditScope(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		body     string
	}{
		{
			name:     "anthropic",
			protocol: ContentModerationProtocolAnthropicMessages,
			body:     `{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool_1","content":"tool evidence"},{"type":"text","text":"continue deployment"}]}]}`,
		},
		{
			name:     "gemini",
			protocol: ContentModerationProtocolGemini,
			body:     `{"contents":[{"role":"user","parts":[{"functionResponse":{"name":"deploy","response":{"result":"tool evidence"}}},{"text":"continue deployment"}]}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userOnly := ExtractContentModerationInput(tt.protocol, []byte(tt.body), ContentModerationAuditScopeUserOnly)
			require.Len(t, userOnly.Sources, 1)
			require.Equal(t, "user", userOnly.Sources[0].Role)
			require.NotContains(t, userOnly.Text, "tool evidence")

			userAndTool := ExtractContentModerationInput(tt.protocol, []byte(tt.body), ContentModerationAuditScopeUserAndTool)
			require.Len(t, userAndTool.Sources, 2)
			require.Equal(t, "tool", userAndTool.Sources[0].Role)
			require.Contains(t, userAndTool.Text, "tool evidence")
		})
	}
}

func TestSemanticReviewKeepsAllExplicitTextFromLatestProtocolUserTurn(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		body     string
	}{
		{
			name:     "anthropic",
			protocol: ContentModerationProtocolAnthropicMessages,
			body: `{"messages":[
				{"role":"user","content":"older request"},
				{"role":"assistant","content":"older response"},
				{"role":"user","content":[{"type":"text","text":"deploy the service"},{"type":"tool_result","tool_use_id":"tool_1","content":"tool evidence"},{"type":"text","text":"and verify health"}]}
			]}`,
		},
		{
			name:     "gemini",
			protocol: ContentModerationProtocolGemini,
			body: `{"contents":[
				{"role":"user","parts":[{"text":"older request"}]},
				{"role":"model","parts":[{"text":"older response"}]},
				{"role":"user","parts":[{"text":"deploy the service"},{"functionResponse":{"name":"deploy","response":{"result":"tool evidence"}}},{"text":"and verify health"}]}
			]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := ExtractContentModerationInput(tt.protocol, []byte(tt.body))
			cfg := semanticReviewTestConfig()
			cfg.Trigger = ContentModerationSemanticReviewTriggerAll

			got := buildContentModerationSemanticReviewInput(cfg, content, "")

			require.Contains(t, got, "deploy the service")
			require.Contains(t, got, "and verify health")
			require.NotContains(t, got, "tool evidence")
			require.NotContains(t, got, "older request")
		})
	}
}
