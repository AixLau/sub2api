package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractContentModerationInputLatestTurnOnlyKeepsLatestUserAndPriorOutput(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		body     string
		want     string
		omitted  []string
	}{
		{
			name:     "openai chat multipart user turn",
			protocol: ContentModerationProtocolOpenAIChat,
			body: `{"messages":[
				{"role":"system","content":"system instruction"},
				{"role":"user","content":"older user input"},
				{"role":"assistant","content":"older assistant output"},
				{"role":"tool","content":"tool payload"},
				{"role":"system","content":"turn boundary"},
				{"role":"assistant","content":"previous assistant output"},
				{"role":"user","content":[{"type":"text","text":"latest user first part"},{"type":"text","text":"latest user second part"}]}
			]}`,
			want:    "latest user first part latest user second part previous assistant output",
			omitted: []string{"system instruction", "older user input", "older assistant output", "tool payload", "turn boundary"},
		},
		{
			name:     "openai responses output text",
			protocol: ContentModerationProtocolOpenAIResponses,
			body: `{"input":[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"older user input"}]},
				{"type":"message","role":"assistant","content":[{"type":"output_text","text":"previous assistant output"}]},
				{"type":"message","role":"user","content":[{"type":"input_text","text":"latest user input"}]}
			]}`,
			want:    "latest user input previous assistant output",
			omitted: []string{"older user input"},
		},
		{
			name:     "gemini model output",
			protocol: ContentModerationProtocolGemini,
			body: `{"systemInstruction":{"parts":[{"text":"system instruction"}]},"contents":[
				{"role":"user","parts":[{"text":"older user input"}]},
				{"role":"model","parts":[{"text":"previous model output"}]},
				{"role":"user","parts":[{"text":"latest user input"}]}
			]}`,
			want:    "latest user input previous model output",
			omitted: []string{"system instruction", "older user input"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := ExtractContentModerationInput(tt.protocol, []byte(tt.body), ContentModerationAuditScopeLatestTurnOnly)
			require.False(t, input.Truncated, input.TruncateReasons)
			require.Equal(t, tt.want, input.Text)
			for _, omitted := range tt.omitted {
				require.NotContains(t, input.Text, omitted)
			}
			require.NotEmpty(t, input.Sources)
			require.Equal(t, len(input.Sources), len(input.Extraction.Sources))
		})
	}
}

func TestExtractContentModerationInputLatestTurnOnlyWithoutUserPreservesInput(t *testing.T) {
	input := ExtractContentModerationInput(
		ContentModerationProtocolOpenAIChat,
		[]byte(`{"messages":[{"role":"assistant","content":"assistant output"}]}`),
		ContentModerationAuditScopeLatestTurnOnly,
	)

	require.False(t, input.Truncated, input.TruncateReasons)
	require.Equal(t, "assistant output", input.Text)
	require.Len(t, input.Sources, 1)
	require.Equal(t, "assistant", input.Sources[0].Role)
}

func TestContentModerationLatestTurnOnlyChangesRuleScope(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.EngineMode = ContentModerationEngineModeRuleOnly
	cfg.KeywordBlockingMode = ContentModerationKeywordModeKeywordOnly
	cfg.AuditScope = ContentModerationAuditScopeAllContext
	cfg.LatestTurnOnly = true
	cfg.KeywordRules = []ContentModerationKeywordRule{{
		Keyword: "risk-marker", Category: ContentModerationKeywordCategoryCyber,
		Severity: ContentModerationKeywordSeverityHigh, Action: ContentModerationKeywordActionBlock, Enabled: true,
	}}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	repo := &contentModerationTestRepo{}
	settings := &contentModerationTestSettingRepo{values: map[string]string{
		SettingKeyRiskControlEnabled:      "true",
		SettingKeyContentModerationConfig: string(raw),
	}}
	svc := NewContentModerationService(settings, repo, nil, nil, nil, nil, nil)
	body := []byte(`{"messages":[
		{"role":"user","content":"older risk-marker"},
		{"role":"assistant","content":"previous output"},
		{"role":"user","content":"latest safe input"}
	]}`)

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     body,
	})
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.False(t, decision.Blocked)
	require.Empty(t, repo.snapshotLogs())
}

func TestContentModerationLatestTurnOnlyConfigRoundTrip(t *testing.T) {
	initial := defaultContentModerationConfig()
	initial.Enabled = true
	initial.LatestTurnOnly = false
	raw, err := json.Marshal(initial)
	require.NoError(t, err)
	settings := &contentModerationTestSettingRepo{values: map[string]string{
		SettingKeyContentModerationConfig: string(raw),
	}}
	svc := NewContentModerationService(settings, &contentModerationTestRepo{}, nil, nil, nil, nil, nil)
	enabled := true
	view, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{LatestTurnOnly: &enabled})
	require.NoError(t, err)
	require.True(t, view.LatestTurnOnly)

	savedRaw, err := settings.GetValue(context.Background(), SettingKeyContentModerationConfig)
	require.NoError(t, err)
	var saved ContentModerationConfig
	require.NoError(t, json.Unmarshal([]byte(savedRaw), &saved))
	require.True(t, saved.LatestTurnOnly)
}
