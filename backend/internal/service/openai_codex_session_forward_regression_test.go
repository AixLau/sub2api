package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCodexSessionFullForwardRetainsOriginalCacheBinding(t *testing.T) {
	for _, transport := range []string{"http", "passthrough", "ws"} {
		for _, mode := range []string{CodexSessionIdentityMappingV2, CodexSessionIdentityMappingLegacy} {
			for _, source := range []string{"header_only", "body_nested_only", "header_nested_only", "explicit_flat_cache", "explicit_header_cache"} {
				t.Run(transport+"/"+mode+"/"+source, func(t *testing.T) {
					cfg := &config.Config{Gateway: config.GatewayConfig{CodexSessionIdentityMapping: mode}}
					cfg.Gateway.OpenAIWS.Enabled = transport == "ws"
					cfg.Gateway.OpenAIWS.OAuthEnabled = true
					cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
					cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
					cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
					upstream := &httpUpstreamRecorder{}
					store := &codexSessionIdentityTestStore{GatewayCache: &stubGatewayCache{}, values: map[string]string{}}
					svc := &OpenAIGatewayService{cfg: cfg, cache: store, httpUpstream: upstream, toolCorrector: NewCodexToolCorrector()}
					account := newTestOAuthAccount(9511, map[string]any{
						"openai_passthrough":              transport == "passthrough",
						"responses_websockets_v2_enabled": transport == "ws",
						codexFingerprintModeExtraKey:      "off",
					})
					account.Credentials = map[string]any{"access_token": "test-token", "chatgpt_account_id": "cache-binding-account"}
					capture := &openAIWSCaptureConn{}
					dialer := &openAIWSCaptureDialer{conn: capture}
					if transport == "ws" {
						pool := newOpenAIWSConnPool(cfg)
						pool.setClientDialerForTest(dialer)
						t.Cleanup(pool.Close)
						svc.openaiWSPool = pool
					}
					rawSession := newCodexUUIDv7ForTest(t)
					var firstSession string
					for turn := 0; turn < 2; turn++ {
						c := newCodexSessionIdentityTestContext(t, 91, 92)
						c.Request.Header.Set("User-Agent", "codex_cli_rs/0.146.0")
						c.Request.Header.Set("originator", "codex_cli_rs")
						body := map[string]any{"model": "gpt-5.2", "stream": true, "instructions": "test", "prompt_cache_key": rawSession, "input": []any{map[string]any{"role": "user", "content": "hello"}}}
						nested, err := json.Marshal(map[string]any{"session_id": rawSession, "turn_started_at_unix_ms": 1000 + turn})
						require.NoError(t, err)
						switch source {
						case "body_nested_only":
							body["client_metadata"] = map[string]any{openAIWSTurnMetadataHeader: string(nested)}
						case "header_nested_only":
							c.Request.Header.Set(openAIWSTurnMetadataHeader, string(nested))
						default:
							c.Request.Header.Set("session-id", rawSession)
						}
						independent := strings.HasPrefix(source, "explicit_")
						if independent {
							body["prompt_cache_key"] = "independent-cache"
							if source == "explicit_flat_cache" {
								body["client_metadata"] = map[string]any{"session_id": rawSession}
							}
						}
						encoded, err := json.Marshal(body)
						require.NoError(t, err)
						completed := `{"type":"response.completed","response":{"id":"resp_binding","model":"gpt-5.2","usage":{"input_tokens":1,"output_tokens":1}}}`
						upstream.resp = &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + completed + "\n\n"))}
						capture.mu.Lock()
						capture.events = append(capture.events, []byte(completed))
						capture.mu.Unlock()
						_, err = svc.Forward(context.Background(), c, account, encoded)
						require.NoError(t, err)
						var headers http.Header
						var output []byte
						if transport == "ws" {
							require.Nil(t, upstream.lastReq, "must reach forwardOpenAIWSV2")
							require.Len(t, capture.writes, turn+1)
							headers = dialer.lastHeaders
							output, err = json.Marshal(capture.writes[turn])
							require.NoError(t, err)
						} else {
							require.NotNil(t, upstream.lastReq)
							require.Equal(t, transport == "passthrough", c.GetBool("openai_passthrough"), "exercise the intended full forwarding branch")
							headers, output = upstream.lastReq.Header, upstream.lastBody
						}
						mapped := headers.Get("session-id")
						require.NotEmpty(t, mapped)
						require.NotEqual(t, rawSession, mapped)
						if mode == CodexSessionIdentityMappingLegacy {
							require.Equal(t, isolateOpenAIUpstreamSessionID(92, account, rawSession), mapped)
						} else {
							require.True(t, isCodexUUIDv7(mapped))
						}
						if turn == 0 {
							firstSession = mapped
						}
						require.Equal(t, firstSession, mapped)
						require.Equal(t, mapped, headers.Get("session_id"))
						require.Equal(t, mapped, gjson.GetBytes(output, "client_metadata.session_id").String())
						require.Equal(t, mapped, gjson.Get(gjson.GetBytes(output, "client_metadata."+openAIWSTurnMetadataHeader).String(), "session_id").String())
						require.Equal(t, mapped, gjson.Get(headers.Get(openAIWSTurnMetadataHeader), "session_id").String())
						wantCacheKey := mapped
						if independent {
							wantCacheKey = scopeCodexAccountIdentityValue(account, 92, "prompt-cache", "independent-cache")
							require.NotEqual(t, mapped, wantCacheKey)
						}
						require.Equal(t, wantCacheKey, gjson.GetBytes(output, "prompt_cache_key").String())
					}
				})
			}
		}
	}
}

func TestCodexSessionStrategyCutoverAndRollbackAtHTTPBuilders(t *testing.T) {
	// An old UUIDv7 is still a UUIDv7: creation time cannot prove whether it
	// was active before upgrade. A strategy change is an explicit boundary.
	const raw = "01950000-0000-7000-8000-000000000001"
	for _, passthrough := range []bool{false, true} {
		t.Run(map[bool]string{false: "http", true: "passthrough"}[passthrough], func(t *testing.T) {
			store := &codexSessionIdentityTestStore{values: map[string]string{}}
			account := codexSessionIdentityV2Account("strategy-cutover")
			cfg := &config.Config{}
			svc := &OpenAIGatewayService{cache: store, cfg: cfg}
			var v2Session string
			for _, mode := range []string{"legacy", "legacy", "v2", "v2", "legacy", "legacy", "v2"} {
				cfg.Gateway.CodexSessionIdentityMapping = mode
				c := newCodexSessionIdentityTestContext(t, 91, 92)
				c.Request.Header.Set("session-id", raw)
				body := []byte(`{"model":"gpt-5.2","prompt_cache_key":"` + raw + `","client_metadata":{"session_id":"` + raw + `"}}`)
				var req *http.Request
				var err error
				if passthrough {
					req, err = svc.buildUpstreamRequestOpenAIPassthrough(context.Background(), c, account, body, "token")
				} else {
					req, err = svc.buildUpstreamRequest(context.Background(), c, account, body, "token", true, raw, true)
				}
				require.NoError(t, err)
				mapped := req.Header.Get("session-id")
				legacy := isolateOpenAIUpstreamSessionID(92, account, raw)
				if mode == "legacy" {
					require.Equal(t, legacy, mapped)
				} else {
					if v2Session == "" {
						v2Session = mapped
					}
					require.True(t, isCodexUUIDv7(mapped))
					require.Equal(t, v2Session, mapped, "re-enabling v2 recovers its durable mapping")
					require.NotEqual(t, legacy, mapped)
				}
				require.NotEqual(t, raw, mapped)
				output, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				require.Equal(t, mapped, gjson.GetBytes(output, "client_metadata.session_id").String())
				require.Equal(t, mapped, gjson.GetBytes(output, "prompt_cache_key").String())
				require.Equal(t, mapped, gjson.Get(req.Header.Get(openAIWSTurnMetadataHeader), "session_id").String())
			}
			require.Equal(t, 1, store.sets)
		})
	}
}

func TestCodexSessionLegacyHTTPBuildersIsolateDownstreamAndUpstream(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		t.Run(map[bool]string{false: "http", true: "passthrough"}[passthrough], func(t *testing.T) {
			svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{CodexSessionIdentityMapping: "legacy"}}}
			raw := newCodexUUIDv7ForTest(t)
			body := []byte(`{"model":"gpt-5.2","prompt_cache_key":"` + raw + `"}`)
			var sessions []string
			for _, scope := range []struct {
				userID, keyID int64
				accountID     string
			}{{91, 92, "account-a"}, {91, 92, "account-a"}, {93, 94, "account-a"}, {91, 92, "account-b"}} {
				c := newCodexSessionIdentityTestContext(t, scope.userID, scope.keyID)
				c.Request.Header.Set("session-id", raw)
				account := codexSessionIdentityV2Account(scope.accountID)
				var req *http.Request
				var err error
				if passthrough {
					req, err = svc.buildUpstreamRequestOpenAIPassthrough(context.Background(), c, account, body, "token")
				} else {
					req, err = svc.buildUpstreamRequest(context.Background(), c, account, body, "token", true, raw, true)
				}
				require.NoError(t, err, "legacy must not require the v2 store")
				mapped := req.Header.Get("session-id")
				require.Equal(t, isolateOpenAIUpstreamSessionID(scope.keyID, account, raw), mapped)
				require.NotEqual(t, raw, mapped)
				sessions = append(sessions, mapped)
			}
			require.Equal(t, sessions[0], sessions[1])
			require.NotEqual(t, sessions[0], sessions[2])
			require.NotEqual(t, sessions[0], sessions[3])
		})
	}
}

func TestCodexCompactSessionHeadersSynchronizeNestedMetadata(t *testing.T) {
	for _, mode := range []string{"v2", "legacy"} {
		for _, accountType := range []string{AccountTypeOAuth, AccountTypeSetupToken} {
			for _, passthrough := range []bool{false, true} {
				t.Run(mode+"/"+accountType+"/"+map[bool]string{false: "http", true: "passthrough"}[passthrough], func(t *testing.T) {
					account := codexSessionIdentityV2Account("compact-parity")
					account.Type = accountType
					svc := &OpenAIGatewayService{cache: &codexSessionIdentityTestStore{values: map[string]string{}}, cfg: &config.Config{Gateway: config.GatewayConfig{CodexSessionIdentityMapping: mode}}}
					c := newCodexSessionIdentityTestContext(t, 91, 92)
					c.Request.URL.Path = "/v1/responses/compact"
					c.Request.Header.Set("version", "0.146.0")
					raw := newCodexUUIDv7ForTest(t)
					c.Request.Header.Set("session-id", raw)
					c.Request.Header.Set(openAIWSTurnMetadataHeader, `{"session_id":"`+raw+`","sandbox":"seatbelt","tool_namespaces_info":{"private":"body-only"}}`)
					body := []byte(`{"model":"gpt-5.2", "input":[],"instructions":"keep exact body"}`)
					var req *http.Request
					var err error
					if passthrough {
						req, err = svc.buildUpstreamRequestOpenAIPassthrough(context.Background(), c, account, body, "token")
					} else {
						req, err = svc.buildUpstreamRequest(context.Background(), c, account, body, "token", false, "", true)
					}
					require.NoError(t, err)
					mapped := req.Header.Get("session-id")
					require.NotEqual(t, raw, mapped)
					require.Equal(t, mapped, req.Header.Get("session_id"))
					nested := gjson.Parse(req.Header.Get(openAIWSTurnMetadataHeader))
					require.Equal(t, mapped, nested.Get("session_id").String())
					require.Equal(t, "seatbelt", nested.Get("sandbox").String())
					require.False(t, nested.Get("tool_namespaces_info").Exists())
					output, err := io.ReadAll(req.Body)
					require.NoError(t, err)
					require.Equal(t, body, output)
					require.Equal(t, int64(len(body)), req.ContentLength)
					replayed, err := req.GetBody()
					require.NoError(t, err)
					defer replayed.Close()
					output, err = io.ReadAll(replayed)
					require.NoError(t, err)
					require.Equal(t, body, output)
				})
			}
		}
	}
}
