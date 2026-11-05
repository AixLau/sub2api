package openai

import (
	"strings"
	"testing"
)

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// CodexBaseInstructionsForModel 应按模型返回对应的真实 Codex base prompt。
func TestCodexBaseInstructionsForModel(t *testing.T) {
	cases := []struct {
		model    string
		wantHead string
	}{
		{"gpt-6-astra", "You are Codex, an agent based on GPT-6"},
		{"gpt-6", "You are Codex, an agent based on GPT-6"},
		{"openai/gpt-6-astra", "You are Codex, an agent based on GPT-6"},
		{"OPENAI/GPT-6_ASTRA", "You are Codex, an agent based on GPT-6"},
		{"gpt-6-astra-2026-09-01", "You are Codex, an agent based on GPT-6"},
		{"gpt-5-codex", "You are Codex, based on GPT-5"},
		{"gpt-5.3-codex", "You are Codex, based on GPT-5"},
		{"gpt-5.3-codex-spark", "You are Codex, based on GPT-5"},
		{"gpt-5.1-codex-max", "You are Codex, based on GPT-5"},
		{"gpt-5.2-codex", "You are Codex, based on GPT-5"},
		{"gpt-5.5", "You are Codex, a coding agent based on GPT-5"},
		{" GPT-5.5 ", "You are Codex, a coding agent based on GPT-5"},
		{"gpt-5.2", "You are GPT-5.2 running in the Codex CLI"},
		{"gpt-5.1", "You are GPT-5.1 running in the Codex CLI"},
		{"gpt-5", "You are Codex, a coding agent based on GPT-5"},   // 回退到最新（GPT-5.5）
		{"gpt-5.4", "You are Codex, a coding agent based on GPT-5"}, // 未单独维护 → 最新
		{"gpt-5.3", "You are Codex, a coding agent based on GPT-5"}, // 未单独维护 → 最新
		{"some-unknown-model", "You are Codex, a coding agent based on GPT-5"},
		{"", "You are Codex, a coding agent based on GPT-5"}, // 回退到最新
	}
	for _, c := range cases {
		got := strings.TrimSpace(CodexBaseInstructionsForModel(c.model))
		if got == "" {
			t.Errorf("model %q: got empty instructions", c.model)
			continue
		}
		if !strings.HasPrefix(got, c.wantHead) {
			t.Errorf("model %q: got prefix %q, want %q", c.model, firstLine(got), c.wantHead)
		}
	}
}

// GPT-6 Sol / Luna 使用各自的官方 Codex 模板，不再回退到 GPT-5.5（"based on GPT-5"）。
func TestCodexBaseInstructionsForGPT6SolAndLuna(t *testing.T) {
	for _, tc := range []struct {
		models []string
		want   string
	}{
		{[]string{"gpt-6-sol", "openai/GPT-6-Sol", "gpt_6_sol", "gpt-6-sol-high", "gpt-6-sol-openai-compact"}, instructionsGPT6Sol},
		{[]string{"gpt-6-luna", "gpt-6-luna-max", "gpt-6-luna-openai-compact"}, instructionsGPT6Luna},
	} {
		for _, model := range tc.models {
			got := CodexBaseInstructionsForModel(model)
			if got != tc.want {
				t.Errorf("model %q: got prefix %q, want its dedicated GPT-6 template", model, firstLine(got))
			}
			if !strings.HasPrefix(got, "You are Codex, an agent based on GPT-6") {
				t.Errorf("model %q: got prefix %q, want GPT-6 identity", model, firstLine(got))
			}
		}
	}
	if instructionsGPT6Sol == instructionsGPT6Luna {
		t.Fatal("GPT-6 Sol and Luna templates must be distinct")
	}
	// 未知 gpt-6-sol 后缀不得误命中 Sol 模板。
	if got := CodexBaseInstructionsForModel("gpt-6-solar"); got == instructionsGPT6Sol {
		t.Error("gpt-6-solar must not resolve to the GPT-6 Sol template")
	}
}

// Codex 按字面发送 instructions_template（不做模板替换），因此 {{connector_id}}
// 这类说明性占位符应原样保留，与官方客户端一致。
func TestCodexGPT6InstructionsKeepLiteralConnectorPlaceholder(t *testing.T) {
	for _, model := range []string{"gpt-6.1-sol", "gpt-6-sol", "gpt-6-luna"} {
		if !strings.Contains(CodexBaseInstructionsForModel(model), "app://{{connector_id}}") {
			t.Errorf("model %q: Codex template text must be sent verbatim", model)
		}
	}
}
