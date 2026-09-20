package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestResolveCodexSandboxForUserAgent(t *testing.T) {
	for _, tt := range []struct{ ua, sandbox string }{
		{"codex-tui/0.200.1 (Linux 6.8.0; x86_64) terminal", "seccomp"},
		{"codex-tui/0.200.1 (Ubuntu 24.04; x86_64) WindowsTerminal", "seccomp"},
		{"codex-tui/0.200.1 (macOS 15.1; arm64) terminal", "seatbelt"},
		{"codex-tui/0.200.1 (Mac OS X 15.1; arm64) terminal", "seatbelt"},
		{"codex-tui/0.200.1 (Mac OS 15.1; arm64) terminal", "seatbelt"},
		{"Codex Desktop/0.200.1 (Darwin 24.1.0; arm64) unknown", "seatbelt"},
		{"codex-tui/0.200.1 (Macintosh; arm64) terminal", "seatbelt"},
		{"codex-tui/0.200.1 (Windows NT 10.0; x86_64) terminal", "windows_sandbox"},
		{"codex-tui/0.200.1 (Windows 11; arm64) terminal", "windows_sandbox"},
		{"codex-tui/0.200.1 (WINDOWS; x86_64) terminal", "windows_sandbox"},
		{"codex-tui/0.200.1 (FreeBSD 14.0; x86_64) WindowsTerminal", ""},
		{"codex-tui/0.200.1 (unknown; arm64) Linux (codex-tui; 0.200.1)", ""},
		{"codex-linux/0.200.1 (unknown; arm64) terminal", ""},
		{"codex-tui/0.200.1 (Linux Windows; x86_64) terminal", ""},
		{"codex-tui/0.200.1 (Linux/macOS; x86_64) terminal", ""},
		{"codex-tui/0.200.1 (Linux-like; x86_64) terminal", ""},
		{"codex-tui/0.200.1 (Linux 6.8.0; x86_64", ""},
		{"codex-tui/0.200.1 terminal (Linux)", ""},
		{"codex-tui/0.200.1", ""},
		{"", ""},
	} {
		t.Run(tt.ua, func(t *testing.T) {
			require.Equal(t, tt.sandbox, resolveCodexSandboxForUserAgent(tt.ua))
		})
	}
}

func TestApplyCodexFingerprintSandboxMetadataRawPreservesOtherFields(t *testing.T) {
	original := `{"installation_id":"account-install","sandbox":"seatbelt","sandbox_mode":"workspace-write","session_id":"S","thread_id":"T","turn_id":"U","workspace":{"cwd":"/workspace/client"},"large":9007199254740993,"extra":[1e400,1.234567890123456789]}`
	encodedMetadata, err := json.Marshal(original)
	require.NoError(t, err)
	body := []byte(`{"input": [ 9007199254740993, 1e400 ], "client_metadata":{"extra":1e400,"x-codex-turn-metadata":` + string(encodedMetadata) + `}}`)
	headers := make(http.Header)
	headers.Set("User-Agent", "codex-tui/0.200.1 (Linux 6.8.0; x86_64) terminal")
	headers.Set("x-codex-turn-metadata", strings.Replace(original, "seatbelt", "windows_sandbox", 1))
	ids := &codexFingerprintIDs{mode: codexFingerprintDevice}

	out, changed, err := applyCodexFingerprintSandboxMetadataRaw(headers, body, ids)
	require.NoError(t, err)
	require.True(t, changed)
	require.Contains(t, string(out), `"input": [ 9007199254740993, 1e400 ]`)
	require.Contains(t, string(out), `"extra":1e400`)
	var want map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(original), &want))
	want["sandbox"] = json.RawMessage(`"seccomp"`)
	for _, raw := range []string{headers.Get("x-codex-turn-metadata"), gjson.GetBytes(out, "client_metadata.x-codex-turn-metadata").String()} {
		var got map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(raw), &got))
		require.Equal(t, want, got)
	}
	again, changed, err := applyCodexFingerprintSandboxMetadataRaw(headers, out, ids)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, out, again)
}

func TestApplyCodexFingerprintSandboxMetadataRawNoop(t *testing.T) {
	for _, raw := range []string{
		"", `{}`, `{"sandbox_mode":"read-only"}`, `{malformed`, `[]`, `null`,
		`{ "sandbox" : "\u0073eccomp", "sandbox_mode" : "workspace-write" }`,
	} {
		t.Run(raw, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"client_metadata": map[string]any{"x-codex-turn-metadata": raw}})
			require.NoError(t, err)
			headers := make(http.Header)
			headers.Set("User-Agent", "codex-tui/0.200.1 (Ubuntu 24.04; x86_64) terminal")
			headers.Set("x-codex-turn-metadata", raw)
			out, changed, err := applyCodexFingerprintSandboxMetadataRaw(headers, body, &codexFingerprintIDs{mode: codexFingerprintDevice})
			require.NoError(t, err)
			require.False(t, changed)
			require.Equal(t, body, out)
			require.Equal(t, raw, headers.Get("x-codex-turn-metadata"))
		})
	}
}

func TestOpenAIGatewayService_CodexFingerprintSandboxUsesFinalUserAgent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const (
		linuxUA   = "codex-tui/0.200.1 (Linux 6.8.0; x86_64) WindowsTerminal"
		macUA     = "codex-tui/0.200.1 (Mac OS X 15.1; arm64) iTerm"
		darwinUA  = "codex-tui/0.200.1 (Darwin 24.1.0; arm64) terminal"
		windowsUA = "codex-tui/0.200.1 (Windows NT 10.0; x86_64) terminal"
		unknownUA = "codex-tui/0.200.1 (FreeBSD 14.0; x86_64) WindowsTerminal"
	)
	for _, transport := range []string{"http", "passthrough"} {
		for _, tt := range []struct {
			name, canonicalUA, accountUA, clientUA, inputSandbox, wantUA, wantSandbox string
			forceCanonical                                                            bool
		}{
			{"linux", linuxUA, "", macUA, "seatbelt", linuxUA, "seccomp", false},
			{"macos", macUA, "", linuxUA, "seccomp", macUA, "seatbelt", false},
			{"darwin", darwinUA, "", windowsUA, "windows_sandbox", darwinUA, "seatbelt", false},
			{"windows", windowsUA, "", macUA, "seatbelt", windowsUA, "windows_sandbox", false},
			{"unknown", unknownUA, "", linuxUA, "seatbelt", unknownUA, "seatbelt", false},
			{"global_overrides_account", linuxUA, macUA, windowsUA, "seatbelt", linuxUA, "seccomp", false},
			{"enforcement_overrides_account", windowsUA, macUA, linuxUA, "seatbelt", windowsUA, "windows_sandbox", true},
			{"absent_sandbox", macUA, "", windowsUA, "", macUA, "", false},
		} {
			t.Run(transport+"/"+tt.name, func(t *testing.T) {
				turnMetadata := map[string]any{
					"installation_id": "client-install", "session_id": "client-session",
					"thread_id": "client-thread", "turn_id": "client-turn", "window_id": "client-window",
					"sandbox_mode": "workspace-write", "workspace": map[string]any{"cwd": "/workspace/client"},
				}
				if tt.inputSandbox != "" {
					turnMetadata["sandbox"] = tt.inputSandbox
				}
				rawMetadata, err := json.Marshal(turnMetadata)
				require.NoError(t, err)
				body, err := json.Marshal(map[string]any{
					"model": "gpt-5.2", "stream": false,
					"input":           []any{map[string]any{"type": "message", "role": "user", "content": "hi"}},
					"client_metadata": map[string]any{"session_id": "client-session", "x-codex-turn-metadata": string(rawMetadata)},
				})
				require.NoError(t, err)

				forward := func(mode codexFingerprintMode) *httpUpstreamRecorder {
					t.Helper()
					account := newTestOAuthAccount(4406, map[string]any{
						codexFingerprintModeExtraKey: string(mode), "openai_passthrough": transport == "passthrough",
					})
					account.Concurrency = 1
					account.Credentials = map[string]any{"access_token": "oauth-token", "chatgpt_account_id": "chatgpt-acc", "user_agent": tt.accountUA}
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
					c.Request.Header.Set("User-Agent", tt.clientUA)
					c.Request.Header.Set("originator", "codex-tui")
					c.Request.Header.Set("session-id", "client-session")
					c.Request.Header.Set("x-codex-turn-metadata", string(rawMetadata))
					upstream := &httpUpstreamRecorder{resp: &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": {"text/event-stream"}},
						Body:       io.NopCloser(strings.NewReader("data: [DONE]\n\n")),
					}}
					svc := &OpenAIGatewayService{
						cfg:          &config.Config{Gateway: config.GatewayConfig{ForceCodexCLI: tt.forceCanonical}},
						httpUpstream: upstream, toolCorrector: NewCodexToolCorrector(),
						settingService: NewSettingService(&codexVersionSettingRepoStub{values: map[string]string{
							SettingKeyOpenAICodexUserAgent: tt.canonicalUA, SettingKeyOpenAICodexClientVersionSynced: "0.200.1",
						}}, nil),
					}
					_, err := svc.Forward(context.Background(), c, account, body)
					require.NoError(t, err)
					require.NotNil(t, upstream.lastReq)
					require.Equal(t, transport == "passthrough", c.GetBool("openai_passthrough"))
					require.Equal(t, tt.wantUA, upstream.lastReq.UserAgent())
					require.Equal(t, int64(len(upstream.lastBody)), upstream.lastReq.ContentLength)
					retryBody, err := upstream.lastReq.GetBody()
					require.NoError(t, err)
					defer retryBody.Close()
					retryBytes, err := io.ReadAll(retryBody)
					require.NoError(t, err)
					require.Equal(t, upstream.lastBody, retryBytes)
					return upstream
				}

				baseline := forward(codexFingerprintOff)
				converged := forward(codexFingerprintDevice)
				for _, carrier := range []struct{ name, baseline, converged string }{
					{"header", baseline.lastReq.Header.Get("x-codex-turn-metadata"), converged.lastReq.Header.Get("x-codex-turn-metadata")},
					{"body", gjson.GetBytes(baseline.lastBody, "client_metadata.x-codex-turn-metadata").String(), gjson.GetBytes(converged.lastBody, "client_metadata.x-codex-turn-metadata").String()},
				} {
					require.Equal(t, tt.wantSandbox, gjson.Get(carrier.converged, "sandbox").String(), carrier.name)
					if tt.inputSandbox == "" {
						require.False(t, gjson.Get(carrier.converged, "sandbox").Exists(), carrier.name)
					}
					var before, after map[string]any
					require.NoError(t, json.Unmarshal([]byte(carrier.baseline), &before))
					require.NoError(t, json.Unmarshal([]byte(carrier.converged), &after))
					for _, metadata := range []map[string]any{before, after} {
						delete(metadata, "installation_id")
						delete(metadata, "sandbox")
					}
					require.Equal(t, before, after, "%s: device convergence must preserve all other metadata", carrier.name)
				}
			})
		}
	}
}
