package securityaudit

import "testing"

func TestShouldCaptureNonClient(t *testing.T) {
	for _, ua := range []string{"node/22", "Python/3.12 axios/1.8", "Go-http-client/2.0"} {
		if !shouldCaptureNonClient(ua) {
			t.Errorf("expected non-client UA %q", ua)
		}
	}
	for _, ua := range []string{"codex_cli_rs/0.1", "WorkBuddy/1.0", "zcode 小龙虾"} {
		if shouldCaptureNonClient(ua) {
			t.Errorf("expected agent UA %q to be excluded", ua)
		}
	}
}
