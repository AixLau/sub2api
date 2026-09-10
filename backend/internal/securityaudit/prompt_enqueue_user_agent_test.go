package securityaudit

import "testing"

func TestShouldCaptureNonClient(t *testing.T) {
	for _, ua := range []string{"node/22", "Python/3.12 axios/1.8", "Go-http-client/2.0"} {
		if !shouldCaptureNonClient(ua) {
			t.Errorf("expected non-client UA %q", ua)
		}
	}
	for _, ua := range []string{
		"codex_cli_rs/0.1", "WorkBuddy/1.0", "zcode 小龙虾", "Claude-Code/1.0",
		"Cursor/0.48", "Windsurf/1.2", "Cline/3.0", "Roo-Code/1.0",
		"aider/0.86", "OpenCode/1.0", "Gemini-CLI/0.1", "Amazon-Q-Developer/1.0",
		"Kiro/0.1", "OpenHands/0.9", "SWE-agent/1.0", "Devin/1.0", "GitHub-Copilot/1.0",
	} {
		if shouldCaptureNonClient(ua) {
			t.Errorf("expected agent UA %q to be excluded", ua)
		}
	}
}
