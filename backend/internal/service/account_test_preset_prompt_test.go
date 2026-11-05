//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

func TestCreateOpenAITestPayloadUsesModelAwareInstructions(t *testing.T) {
	t.Parallel()

	for _, model := range []string{"gpt-5.4", "gpt-5.2", "gpt-5.3-codex", "gpt-6-astra", "gpt-6.1-sol"} {
		payload := createOpenAITestPayload(model, true, "", true)
		require.Equal(t, openai.CodexBaseInstructionsForModel(model), payload["instructions"], model)
		require.NotEmpty(t, payload["instructions"], model)
	}
	require.NotEqual(t,
		createOpenAITestPayload("gpt-5.3-codex", true, "", true)["instructions"],
		createOpenAITestPayload("gpt-6.1-sol", true, "", true)["instructions"],
	)
}

func TestCreateOpenAITestPayloadWithoutPresetPrompt(t *testing.T) {
	t.Parallel()

	oauth := createOpenAITestPayload("gpt-5.4", true, "ping", false)
	instructions, ok := oauth["instructions"].(string)
	require.True(t, ok, "Codex OAuth endpoint requires the instructions field")
	require.Empty(t, instructions)

	apiKey := createOpenAITestPayload("gpt-5.4", false, "ping", false)
	_, exists := apiKey["instructions"]
	require.False(t, exists)
}

func TestCreateTestPayloadPresetPromptToggle(t *testing.T) {
	t.Parallel()

	withPreset, err := createTestPayload("claude-sonnet-4-5", "", true)
	require.NoError(t, err)
	system, ok := withPreset["system"].([]map[string]any)
	require.True(t, ok)
	require.Equal(t, claudeCodeSystemPrompt, system[0]["text"])

	withoutPreset, err := createTestPayload("claude-sonnet-4-5", "", false)
	require.NoError(t, err)
	_, exists := withoutPreset["system"]
	require.False(t, exists)
	require.NotNil(t, withoutPreset["messages"])
}

func TestCreateGeminiTestPayloadPresetPromptToggle(t *testing.T) {
	t.Parallel()

	var withPreset map[string]any
	require.NoError(t, json.Unmarshal(createGeminiTestPayload("gemini-2.5-flash", "", true), &withPreset))
	require.Contains(t, withPreset, "systemInstruction")

	var withoutPreset map[string]any
	require.NoError(t, json.Unmarshal(createGeminiTestPayload("gemini-2.5-flash", "", false), &withoutPreset))
	require.NotContains(t, withoutPreset, "systemInstruction")
}

func TestAccountTestPresetPromptEnabledDefaultsAndProbe(t *testing.T) {
	t.Parallel()

	var nilSvc *AccountTestService
	require.True(t, nilSvc.accountTestPresetPromptEnabled(context.Background()))
	require.True(t, (&AccountTestService{}).accountTestPresetPromptEnabled(context.Background()))

	probeCtx := context.WithValue(context.Background(), openAIWindowStartProbeContextKey{}, true)
	require.True(t, (&AccountTestService{}).accountTestPresetPromptEnabled(probeCtx))
}

func TestAccountTestPresetPromptEnabledFollowsSetting(t *testing.T) {
	t.Parallel()

	repo := &codexPolicyMigrationRepoStub{values: map[string]string{}}
	svc := &AccountTestService{settingService: NewSettingService(repo, &config.Config{})}
	ctx := context.Background()

	require.True(t, svc.accountTestPresetPromptEnabled(ctx), "missing setting keeps preset enabled")

	repo.values[SettingKeyAccountTestPresetPromptEnabled] = "false"
	require.False(t, svc.accountTestPresetPromptEnabled(ctx))

	probeCtx := context.WithValue(ctx, openAIWindowStartProbeContextKey{}, true)
	require.True(t, svc.accountTestPresetPromptEnabled(probeCtx), "window-start probes always keep preset")

	repo.values[SettingKeyAccountTestPresetPromptEnabled] = "true"
	require.True(t, svc.accountTestPresetPromptEnabled(ctx))
}
