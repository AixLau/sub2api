// Regression cases for AixLau/sub2api commit 52d0ed822.
// Run from backend with:
// go test ./internal/service -run '^TestCodexIdentityAudit_' -count=1
// These tests assert the final outbound identity invariants.
package service

import (
	"encoding/json"
	"net/http"
	"testing"
)

func auditCodexJSON(t *testing.T, raw string) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	return result
}

func auditCodexNested(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	cm, ok := body["client_metadata"].(map[string]any)
	if !ok {
		t.Fatal("client_metadata is not an object")
	}
	raw, ok := cm["x-codex-turn-metadata"].(string)
	if !ok {
		t.Fatal("nested turn metadata is not a JSON string")
	}
	return auditCodexJSON(t, raw)
}

func TestCodexIdentityAudit_DirectTurnHeaderMatchesFinalSession(t *testing.T) {
	headers := make(http.Header)
	headers.Set("session-id", "session-final")
	headers.Set("x-codex-turn-metadata", `{"session_id":"session-stale","sandbox":"seatbelt"}`)
	body := map[string]any{"client_metadata": map[string]any{"session_id": "body-stale"}}
	if _, _, err := normalizeCodexOutboundIdentityMap(headers, body, ""); err != nil {
		t.Fatal(err)
	}
	direct := auditCodexJSON(t, headers.Get("x-codex-turn-metadata"))
	nested := auditCodexNested(t, body)
	if direct["session_id"] != nested["session_id"] {
		t.Fatalf("direct turn header session=%v, final body session=%v", direct["session_id"], nested["session_id"])
	}
}

func TestCodexIdentityAudit_CompatibilityHeaderPreservesBodyOnlyMetadata(t *testing.T) {
	headers := make(http.Header)
	headers.Set("session-id", "session-1")
	headers.Set("x-codex-turn-metadata", `{"session_id":"session-1"}`)
	body := map[string]any{"client_metadata": map[string]any{
		"session_id": "session-1",
		// Synthetic extension tests transport preservation, not an upstream schema.
		"x-codex-turn-metadata": `{"session_id":"session-1","audit_body_only":{"marker":"keep"}}`,
	}}
	if _, _, err := normalizeCodexOutboundIdentityMap(headers, body, ""); err != nil {
		t.Fatal(err)
	}
	nested := auditCodexNested(t, body)
	if _, exists := nested["audit_body_only"]; !exists {
		t.Fatal("body-only nested metadata was removed by the compatibility header")
	}
}

func TestCodexIdentityAudit_ParentReferenceEqualsMappedParentThread(t *testing.T) {
	account := &Account{
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Credentials: map[string]any{"chatgpt_account_id": "audit-account"},
	}
	const apiKeyID int64 = 7
	parentCM := map[string]any{"thread_id": "parent-original"}
	childCM := map[string]any{
		"thread_id":                "child-original",
		"x-codex-parent-thread-id": "parent-original",
	}
	applyCodexAccountIdentityFields(parentCM, account, apiKeyID)
	applyCodexAccountIdentityFields(childCM, account, apiKeyID)
	parentBody := map[string]any{"client_metadata": parentCM}
	childBody := map[string]any{"client_metadata": childCM}
	parentHeaders, childHeaders := make(http.Header), make(http.Header)
	if _, _, err := normalizeCodexOutboundIdentityMap(parentHeaders, parentBody, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := normalizeCodexOutboundIdentityMap(childHeaders, childBody, ""); err != nil {
		t.Fatal(err)
	}
	want := parentHeaders.Get("thread-id")
	got := childHeaders.Get("x-codex-parent-thread-id")
	if got != want {
		t.Fatalf("child parent reference=%q, actual parent outbound thread=%q", got, want)
	}
}

func TestCodexIdentityAudit_NextTurnMetadataNotTakenFromHandshake(t *testing.T) {
	// Models the adapter's reuse of its initial header map. This is not a socket
	// integration test; add an actual two-frame upstream test before deployment.
	headers := make(http.Header)
	headers.Set("session-id", "session-1")
	headers.Set("thread-id", "thread-1")
	headers.Set("x-codex-turn-metadata", `{"session_id":"session-1","thread_id":"thread-1","turn_id":"turn-1","turn_started_at_unix_ms":1000}`)
	first := map[string]any{"client_metadata": map[string]any{
		"session_id": "session-1", "thread_id": "thread-1", "turn_id": "turn-1",
		"x-codex-turn-metadata": `{"session_id":"session-1","thread_id":"thread-1","turn_id":"turn-1","turn_started_at_unix_ms":1000}`,
	}}
	if _, _, err := normalizeCodexOutboundIdentityMap(headers, first, ""); err != nil {
		t.Fatal(err)
	}
	second := map[string]any{"client_metadata": map[string]any{
		"session_id": "session-1", "thread_id": "thread-1", "turn_id": "turn-2",
		"x-codex-turn-metadata": `{"session_id":"session-1","thread_id":"thread-1","turn_id":"turn-2","turn_started_at_unix_ms":2000}`,
	}}
	if _, _, err := normalizeCodexOutboundIdentityMap(headers, second, ""); err != nil {
		t.Fatal(err)
	}
	nested := auditCodexNested(t, second)
	if nested["turn_id"] != "turn-2" || nested["turn_started_at_unix_ms"] != float64(2000) {
		t.Fatalf("second frame inherited old turn metadata: turn_id=%v, start=%v", nested["turn_id"], nested["turn_started_at_unix_ms"])
	}
}
