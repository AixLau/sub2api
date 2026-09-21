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
			for _, shape := range []struct {
				name     string
				sessions []string
			}{
				{"uuidv7", []string{newCodexUUIDv7ForTest(t), newCodexUUIDv7ForTest(t)}},
				{"uuidv4", []string{"550e8400-e29b-41d4-a716-446655440000", "550e8400-e29b-41d4-a716-446655440001"}},
				{"opaque", []string{"client-session-a", "client-session-b"}},
			} {
				for _, source := range []struct {
					name, location  string
					independentKeys []string
				}{
					{name: "header_only", location: "header"},
					{name: "underscore_header", location: "underscore"},
					{name: "cache_fallback", location: "cache"},
					{name: "flat_only", location: "flat"},
					{name: "body_nested_only", location: "body_nested"},
					{name: "header_nested_only", location: "header_nested"},
					{"explicit_header_cache", "header", []string{"independent-cache-a", "independent-cache-b"}},
					{"explicit_flat_cache", "flat", []string{"independent-cache-a", "independent-cache-b"}},
					{"explicit_v7_header_cache", "header", []string{"01950000-0000-7000-8000-000000000001", "01950000-0000-7000-8000-000000000002"}},
					{"explicit_v7_flat_cache", "flat", []string{"01950000-0000-7000-8000-000000000001", "01950000-0000-7000-8000-000000000002"}},
					{"explicit_v7_body_nested_cache", "body_nested", []string{"01950000-0000-7000-8000-000000000001", "01950000-0000-7000-8000-000000000002"}},
					{"explicit_v7_header_nested_cache", "header_nested", []string{"01950000-0000-7000-8000-000000000001", "01950000-0000-7000-8000-000000000002"}},
				} {
					t.Run(transport+"/"+mode+"/"+shape.name+"/"+source.name, func(t *testing.T) {
						cfg := &config.Config{Gateway: config.GatewayConfig{CodexSessionIdentityMapping: mode}}
						cfg.Gateway.OpenAIWS.Enabled = transport == "ws"
						cfg.Gateway.OpenAIWS.OAuthEnabled = true
						cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
						cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
						cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
						store := &codexSessionIdentityTestStore{GatewayCache: &stubGatewayCache{}, values: map[string]string{}}
						account := newTestOAuthAccount(9511, map[string]any{
							"openai_passthrough":              transport == "passthrough",
							"responses_websockets_v2_enabled": transport == "ws",
							codexFingerprintModeExtraKey:      "off",
						})
						account.Credentials = map[string]any{"access_token": "test-token", "chatgpt_account_id": "cache-binding-account"}
						var finalSessions []string
						for _, rawSession := range shape.sessions {
							// Each client session gets its own connection, while both use
							// the same downstream/upstream scope and durable mapping store.
							upstream := &httpUpstreamRecorder{}
							svc := &OpenAIGatewayService{cfg: cfg, cache: store, httpUpstream: upstream, toolCorrector: NewCodexToolCorrector()}
							capture := &openAIWSCaptureConn{}
							dialer := &openAIWSCaptureDialer{conn: capture}
							if transport == "ws" {
								pool := newOpenAIWSConnPool(cfg)
								pool.setClientDialerForTest(dialer)
								t.Cleanup(pool.Close)
								svc.openaiWSPool = pool
							}
							var firstSession string
							for turn := 0; turn < 2; turn++ {
								c := newCodexSessionIdentityTestContext(t, 91, 92)
								c.Request.Header.Set("User-Agent", "codex_cli_rs/0.146.0")
								c.Request.Header.Set("originator", "codex_cli_rs")
								cacheKey := rawSession
								if source.independentKeys != nil {
									cacheKey = source.independentKeys[turn]
								}
								body := map[string]any{"model": "gpt-5.2", "stream": true, "instructions": "test", "prompt_cache_key": cacheKey, "input": []any{map[string]any{"role": "user", "content": "hello"}}}
								nested, err := json.Marshal(map[string]any{"session_id": rawSession, "turn_started_at_unix_ms": 1000 + turn})
								require.NoError(t, err)
								switch source.location {
								case "header":
									c.Request.Header.Set("session-id", rawSession)
								case "underscore":
									c.Request.Header.Set("session_id", rawSession)
								case "flat":
									body["client_metadata"] = map[string]any{"session_id": rawSession}
								case "body_nested":
									body["client_metadata"] = map[string]any{openAIWSTurnMetadataHeader: string(nested)}
								case "header_nested":
									c.Request.Header.Set(openAIWSTurnMetadataHeader, string(nested))
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
								if mode == CodexSessionIdentityMappingLegacy || shape.name != "uuidv7" {
									require.Equal(t, isolateOpenAIUpstreamSessionID(92, account, rawSession), mapped, "map the explicit session, never its independent cache key")
								} else {
									require.True(t, isCodexUUIDv7(mapped))
								}
								if turn == 0 {
									firstSession = mapped
								}
								require.Equal(t, firstSession, mapped, "changing only an independent key must not change the session")
								require.Equal(t, mapped, headers.Get("session_id"))
								require.Equal(t, mapped, gjson.GetBytes(output, "client_metadata.session_id").String())
								require.Equal(t, mapped, gjson.Get(gjson.GetBytes(output, "client_metadata."+openAIWSTurnMetadataHeader).String(), "session_id").String())
								require.Equal(t, mapped, gjson.Get(headers.Get(openAIWSTurnMetadataHeader), "session_id").String())
								wantCacheKey := mapped
								if source.independentKeys != nil {
									wantCacheKey = scopeCodexAccountIdentityValue(account, 92, "prompt-cache", cacheKey)
									require.NotEqual(t, mapped, wantCacheKey)
								}
								require.Equal(t, wantCacheKey, gjson.GetBytes(output, "prompt_cache_key").String())
							}
							finalSessions = append(finalSessions, firstSession)
						}
						require.NotEqual(t, finalSessions[0], finalSessions[1], "different sessions sharing one independent key must remain isolated")
						if shape.name != "uuidv7" {
							require.Zero(t, store.sets, "independent UUIDv7 cache keys must never create session mappings")
						}
					})
				}
			}
		}
	}
}

func TestCodexSessionConflictingLowerPriorityBodyCacheKeyKeepsIndependentScope(t *testing.T) {
	for _, transport := range []string{"http", "passthrough", "ws"} {
		for _, mode := range []string{CodexSessionIdentityMappingV2, CodexSessionIdentityMappingLegacy} {
			for _, bodySession := range []string{
				"01950000-0000-7000-8000-000000000001",
				"550e8400-e29b-41d4-a716-446655440000",
				"body-session-b",
			} {
				t.Run(transport+"/"+mode+"/"+bodySession, func(t *testing.T) {
					run := func(withBodySession bool) (string, string) {
						cfg := &config.Config{Gateway: config.GatewayConfig{CodexSessionIdentityMapping: mode}}
						cfg.Gateway.OpenAIWS.Enabled = transport == "ws"
						cfg.Gateway.OpenAIWS.OAuthEnabled = true
						cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
						cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
						cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
						upstream := &httpUpstreamRecorder{}
						store := &codexSessionIdentityTestStore{GatewayCache: &stubGatewayCache{}, values: map[string]string{}}
						svc := &OpenAIGatewayService{cfg: cfg, cache: store, httpUpstream: upstream, toolCorrector: NewCodexToolCorrector()}
						account := newTestOAuthAccount(9521, map[string]any{
							"openai_passthrough":              transport == "passthrough",
							"responses_websockets_v2_enabled": transport == "ws",
							codexFingerprintModeExtraKey:      "off",
						})
						account.Credentials = map[string]any{"access_token": "test-token", "chatgpt_account_id": "conflict-account"}
						capture := &openAIWSCaptureConn{}
						if transport == "ws" {
							pool := newOpenAIWSConnPool(cfg)
							pool.setClientDialerForTest(&openAIWSCaptureDialer{conn: capture})
							t.Cleanup(pool.Close)
							svc.openaiWSPool = pool
						}
						c := newCodexSessionIdentityTestContext(t, 95, 952)
						c.Request.Header.Set("User-Agent", "codex_cli_rs/0.146.0")
						c.Request.Header.Set("originator", "codex_cli_rs")
						c.Request.Header.Set("session-id", "client-session-a")
						body := map[string]any{"model": "gpt-5.2", "stream": true, "instructions": "test", "prompt_cache_key": bodySession, "input": []any{map[string]any{"role": "user", "content": "hello"}}}
						if withBodySession {
							body["client_metadata"] = map[string]any{"session_id": bodySession}
						}
						encoded, err := json.Marshal(body)
						require.NoError(t, err)
						completed := `{"type":"response.completed","response":{"id":"resp_conflict","model":"gpt-5.2","usage":{"input_tokens":1,"output_tokens":1}}}`
						upstream.resp = &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + completed + "\n\n"))}
						capture.mu.Lock()
						capture.events = append(capture.events, []byte(completed))
						capture.mu.Unlock()
						_, err = svc.Forward(context.Background(), c, account, encoded)
						require.NoError(t, err)
						if transport == "ws" {
							require.Len(t, capture.writes, 1)
							encoded, err = json.Marshal(capture.writes[0])
							require.NoError(t, err)
						} else {
							require.NotNil(t, upstream.lastReq)
							encoded = upstream.lastBody
						}
						return gjson.GetBytes(encoded, "client_metadata.session_id").String(), gjson.GetBytes(encoded, "prompt_cache_key").String()
					}
					withoutBody, withoutBodyCache := run(false)
					withBody, withBodyCache := run(true)
					require.NotEmpty(t, withoutBody)
					require.Equal(t, withoutBody, withBody, "lower-priority body session must not replace the explicit header session")
					require.Equal(t, withoutBodyCache, withBodyCache, "cache-key mapping must use the captured source relationship")
					require.Equal(t, scopeCodexAccountIdentityValue(codexSessionIdentityV2Account("conflict-account"), 952, "prompt-cache", bodySession), withoutBodyCache)
				})
			}
		}
	}
}

func TestCodexSessionParentReferenceRetainsCacheOwnershipAcrossModes(t *testing.T) {
	for _, transport := range []string{"http", "passthrough"} {
		for _, mode := range []codexFingerprintMode{codexFingerprintSession, codexFingerprintFull} {
			for _, bodySession := range []string{
				"01950000-0000-7000-8000-000000000001",
				"550e8400-e29b-41d4-a716-446655440000",
				"body-session-b",
			} {
				t.Run(transport+"/"+string(mode)+"/"+bodySession, func(t *testing.T) {
					store := &codexSessionIdentityTestStore{GatewayCache: &stubGatewayCache{}, values: map[string]string{}}
					run := func(withBodySession bool) (string, string) {
						cfg := &config.Config{Gateway: config.GatewayConfig{CodexSessionIdentityMapping: CodexSessionIdentityMappingV2}}
						upstream := &httpUpstreamRecorder{resp: &http.Response{
							StatusCode: http.StatusOK,
							Header:     http.Header{"Content-Type": {"text/event-stream"}},
							Body:       io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_parent\",\"model\":\"gpt-5.2\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n")),
						}}
						svc := &OpenAIGatewayService{cfg: cfg, cache: store, httpUpstream: upstream, toolCorrector: NewCodexToolCorrector()}
						account := newTestOAuthAccount(9531, map[string]any{codexFingerprintModeExtraKey: string(mode), "openai_passthrough": transport == "passthrough"})
						account.Credentials = map[string]any{"access_token": "test-token", "chatgpt_account_id": "parent-downgrade-account"}
						// This cache-binding test starts with a parent already selected
						// by a previous request. Full graph forwarding is tested separately.
						if mode == codexFingerprintSession {
							store.values[codexHTTPIdentityMappingKey("thread", "user:95", codexSessionIdentityUpstreamScope(account), "parent-thread")] = deriveStableUUIDv4("previous-parent")
						}
						c := newCodexSessionIdentityTestContext(t, 95, 953)
						c.Request.Header.Set("User-Agent", "codex_cli_rs/0.146.0")
						c.Request.Header.Set("originator", "codex_cli_rs")
						c.Request.Header.Set("session-id", "client-session-a")
						body := map[string]any{"model": "gpt-5.2", "stream": true, "instructions": "test", "prompt_cache_key": bodySession, "input": []any{map[string]any{"role": "user", "content": "hello"}}, "client_metadata": map[string]any{"x-codex-parent-thread-id": "parent-thread"}}
						if withBodySession {
							body["client_metadata"].(map[string]any)["session_id"] = bodySession
						}
						encoded, err := json.Marshal(body)
						require.NoError(t, err)
						_, err = svc.Forward(context.Background(), c, account, encoded)
						require.NoError(t, err)
						require.Equal(t, mode == codexFingerprintSession, stagedCodexFingerprintIDs(c, account).httpSessionIdentity, "session parents retain the authoritative v3 snapshot")
						if transport == "passthrough" {
							require.NotNil(t, upstream.lastReq)
						}
						return gjson.GetBytes(upstream.lastBody, "client_metadata.session_id").String(), gjson.GetBytes(upstream.lastBody, "prompt_cache_key").String()
					}
					withoutBody, withoutBodyCache := run(false)
					withBody, withBodyCache := run(true)
					require.NotEmpty(t, withoutBody)
					require.Equal(t, withoutBody, withBody, "the lower-priority body session must not change the selected session")
					require.Equal(t, withoutBodyCache, withBodyCache, "cache ownership must use the original explicit session")
					if mode == codexFingerprintFull {
						require.Equal(t, scopeCodexAccountIdentityValue(codexSessionIdentityV2Account("parent-downgrade-account"), 953, "prompt-cache", bodySession), withoutBodyCache)
					} else {
						require.True(t, isCodexUUIDv7(withoutBody))
						require.NotEqual(t, withoutBody, withoutBodyCache, "session cache belongs to the task")
						require.NotEqual(t, bodySession, withoutBodyCache)
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
