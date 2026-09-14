package service

import "testing"

func TestNormalizeModerationEngineModeMigratesLegacyValues(t *testing.T) {
	cases := map[string]string{
		"rule_only":       ContentModerationEngineModeRulesOnly,
		"api_only":        ContentModerationEngineModeModelOnly,
		"hybrid":          ContentModerationEngineModeRulesAndModel,
		"candidate_only":  ContentModerationEngineModeRulesAndModel,
		"rules_only":      ContentModerationEngineModeRulesOnly,
		"model_only":      ContentModerationEngineModeModelOnly,
		"rules_and_model": ContentModerationEngineModeRulesAndModel,
	}
	for input, want := range cases {
		if got := normalizeModerationEngineMode(input); got != want {
			t.Fatalf("normalizeModerationEngineMode(%q) = %q, want %q", input, got, want)
		}
	}
}
