package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// These fixtures reproduce the root / spawn_agent / side metadata shapes in
// the reported requests. IDs and workspace are synthetic; their relationships
// (including side's self-rooted first turn) are the protocol under test.
const (
	codexRootTopology = `{
		"session_id":"01950000-0000-7000-8000-000000000001",
		"thread_id":"01950000-0000-7000-8000-000000000001",
		"turn_id":"01950000-0000-7000-8000-000000000011",
		"root_turn_id":"01950000-0000-7000-8000-000000000011",
		"turn_started_at_unix_ms":1740000000011,
		"thread_source":"user", "request_kind":"regular",
		"installation_id":"client-install", "sandbox":"seatbelt",
		"sandbox_mode":"workspace-write", "workspace":{"cwd":"/workspace/project"}
	}`
	codexChildTopology = `{
		"session_id":"01950000-0000-7000-8000-000000000001",
		"thread_id":"01950000-0000-7000-8000-000000000002",
		"parent_thread_id":"01950000-0000-7000-8000-000000000001",
		"turn_id":"01950000-0000-7000-8000-000000000012",
		"parent_turn_id":"01950000-0000-7000-8000-000000000011",
		"root_turn_id":"01950000-0000-7000-8000-000000000011",
		"turn_started_at_unix_ms":1740000000012,
		"thread_source":"subagent", "subagent_kind":"thread_spawn", "agent_name":"test-writer",
		"request_kind":"regular", "installation_id":"client-install", "sandbox":"seatbelt",
		"sandbox_mode":"workspace-write", "workspace":{"cwd":"/workspace/project"}
	}`
	codexSideTopology = `{
		"session_id":"01950000-0000-7000-8000-000000000003",
		"thread_id":"01950000-0000-7000-8000-000000000003",
		"forked_from_thread_id":"01950000-0000-7000-8000-000000000001",
		"forked_from_ordinal_exclusive":33,
		"turn_id":"01950000-0000-7000-8000-000000000013",
		"root_turn_id":"01950000-0000-7000-8000-000000000013",
		"turn_started_at_unix_ms":1740000000013,
		"thread_source":"user", "request_kind":"regular",
		"installation_id":"client-install", "sandbox":"seatbelt",
		"sandbox_mode":"workspace-write", "workspace":{"cwd":"/workspace/project"}
	}`
)

type codexTopologyOutbound struct {
	headers http.Header
	body    gjson.Result
}

func (out codexTopologyOutbound) session() string { return out.headers.Get("session-id") }
func (out codexTopologyOutbound) thread() string  { return out.headers.Get("thread-id") }
func (out codexTopologyOutbound) cache() string   { return out.body.Get("prompt_cache_key").String() }

func (out codexTopologyOutbound) carriers() []gjson.Result {
	cm := out.body.Get("client_metadata")
	return []gjson.Result{cm, gjson.Parse(cm.Get(openAIWSTurnMetadataHeader).String()), gjson.Parse(out.headers.Get(openAIWSTurnMetadataHeader))}
}

func TestCodexSessionPeriodHTTPRootChildSideTopology(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, source := range []string{"body", "header", "both", "flat", "split"} {
			t.Run(fmt.Sprintf("passthrough=%v/%s", passthrough, source), func(t *testing.T) {
				svc := newCodexPeriodRedisService(t, miniredis.RunT(t))
				account := newTestOAuthAccount(7610, map[string]any{codexFingerprintModeExtraKey: "session", "openai_passthrough": passthrough})
				account.Credentials = map[string]any{"access_token": "test", "chatgpt_account_id": "topology-account"}
				forward := func(fixture, explicitCache string, user, apiKey int64) codexTopologyOutbound {
					t.Helper()
					var metadata map[string]any
					require.NoError(t, json.Unmarshal([]byte(fixture), &metadata))
					c := newCodexSessionIdentityV2Context(t, user, apiKey)
					c.Request.Header.Set("User-Agent", "codex_cli_rs/0.144.1")
					c.Request.Header.Set("originator", "codex_cli_rs")
					c.Request.Header.Set("session-id", metadata["session_id"].(string))
					c.Request.Header.Set("thread-id", metadata["thread_id"].(string))
					c.Request.Header.Set("x-client-request-id", metadata["thread_id"].(string))
					cm := map[string]any{"session_id": metadata["session_id"], "thread_id": metadata["thread_id"]}
					switch source {
					case "body", "both":
						cm[openAIWSTurnMetadataHeader] = fixture
					case "flat":
						cm = maps.Clone(metadata)
					case "split":
						// References can occur only in the compatibility header while
						// turn_id is in the body; all still belong to one turn graph.
						cm["turn_id"] = metadata["turn_id"]
						cm[openAIWSTurnMetadataHeader] = `{"body_extension":"preserved"}`
					}
					if source == "header" || source == "both" || source == "split" {
						c.Request.Header.Set(openAIWSTurnMetadataHeader, fixture)
					}
					cache := codexFirstIdentityValue(explicitCache, metadata["session_id"].(string))
					body := mustJSONForSessionIdentityTest(t, map[string]any{
						"model": "gpt-5.2", "stream": true, "instructions": "Help with this task.",
						"input":            []any{map[string]any{"role": "user", "content": "Review this change."}},
						"prompt_cache_key": cache, "client_metadata": cm,
					})
					upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200,
						Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: [DONE]\n\n"))}}
					svc.httpUpstream = upstream
					_, err := svc.Forward(context.Background(), c, account, body)
					require.NoError(t, err)
					require.Equal(t, passthrough, c.GetBool("openai_passthrough"))
					out := codexTopologyOutbound{upstream.lastReq.Header, gjson.ParseBytes(upstream.lastBody)}
					require.True(t, isCodexUUIDv7(out.session()))
					require.NotEqual(t, metadata["session_id"], out.session())
					require.NotEqual(t, metadata["thread_id"], out.thread())
					require.NotEqual(t, cache, out.cache())
					require.Equal(t, out.session(), out.headers.Get("session_id"))
					require.Equal(t, out.thread(), out.headers.Get("thread_id"))
					require.Equal(t, out.thread(), out.headers.Get("x-client-request-id"))
					for _, carrier := range out.carriers() {
						require.Equal(t, out.session(), carrier.Get("session_id").String())
						require.Equal(t, out.thread(), carrier.Get("thread_id").String())
						require.Equal(t, out.thread()+":0", carrier.Get("window_id").String())
						for _, field := range []string{"turn_id", "parent_turn_id", "root_turn_id"} {
							require.Equal(t, gjson.Get(fixture, field).String(), carrier.Get(field).String(), field)
						}
						require.Equal(t, gjson.Get(fixture, "turn_started_at_unix_ms").Int(), carrier.Get("turn_started_at_unix_ms").Int())
					}
					preserved := out.carriers()[2]
					if source == "flat" {
						preserved = out.carriers()[0]
					}
					for _, field := range []string{"thread_source", "subagent_kind", "agent_name", "request_kind", "sandbox_mode", "workspace.cwd", "forked_from_ordinal_exclusive"} {
						require.Equal(t, gjson.Get(fixture, field).Value(), preserved.Get(field).Value(), field)
					}
					return out
				}
				root := forward(codexRootTopology, "", 1, 11)
				child := forward(codexChildTopology, "", 1, 12)
				side := forward(codexSideTopology, "", 1, 13)
				require.Equal(t, root.session(), child.session())
				require.Equal(t, root.session(), side.session())
				require.NotEqual(t, root.thread(), child.thread())
				require.NotEqual(t, root.thread(), side.thread())
				require.NotEqual(t, child.thread(), side.thread())
				require.Equal(t, root.cache(), child.cache())
				require.NotEqual(t, root.cache(), side.cache())
				require.Equal(t, root.thread(), child.headers.Get("x-codex-parent-thread-id"))
				require.Equal(t, root.thread(), child.body.Get("client_metadata.x-codex-parent-thread-id").String())
				for _, carrier := range child.carriers() {
					require.Equal(t, root.thread(), carrier.Get("parent_thread_id").String())
				}
				for _, carrier := range side.carriers() {
					require.Equal(t, root.thread(), carrier.Get("forked_from_thread_id").String())
				}
				rootTurn := root.carriers()[0].Get("turn_id").String()
				require.Equal(t, rootTurn, child.carriers()[0].Get("parent_turn_id").String())
				require.Equal(t, rootTurn, child.carriers()[0].Get("root_turn_id").String())
				require.Equal(t, side.carriers()[0].Get("turn_id").String(), side.carriers()[0].Get("root_turn_id").String())
				for _, pair := range []struct {
					fixture  string
					previous codexTopologyOutbound
				}{{codexRootTopology, root}, {codexChildTopology, child}, {codexSideTopology, side}} {
					again := forward(pair.fixture, "", 1, 99)
					require.Equal(t, pair.previous.body.Raw, again.body.Raw, "retry and API key changes preserve the entire client turn graph")
				}
				explicitRoot := forward(codexRootTopology, "partition-1", 1, 11)
				require.Equal(t, explicitRoot.cache(), forward(codexRootTopology, "partition-1", 1, 99).cache())
				require.Equal(t, explicitRoot.cache(), forward(codexChildTopology, "partition-1", 1, 11).cache())
				require.NotEqual(t, root.cache(), explicitRoot.cache())
				require.NotEqual(t, explicitRoot.cache(), forward(codexSideTopology, "partition-1", 1, 11).cache())
				require.NotEqual(t, explicitRoot.cache(), forward(codexRootTopology, "partition-2", 1, 11).cache())
				otherUser := forward(codexRootTopology, "partition-1", 2, 22)
				require.NotEqual(t, root.session(), otherUser.session())
				require.NotEqual(t, root.thread(), otherUser.thread())
				require.NotEqual(t, explicitRoot.cache(), otherUser.cache())
				require.Equal(t, root.headers.Get("x-codex-installation-id"), otherUser.headers.Get("x-codex-installation-id"))
			})
		}
	}
}

func TestCodexSessionPeriodTurnFallback(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		t.Run(fmt.Sprintf("passthrough=%v", passthrough), func(t *testing.T) {
			svc := newCodexPeriodRedisService(t, miniredis.RunT(t))
			account := newTestOAuthAccount(7611, map[string]any{codexFingerprintModeExtraKey: "session"})
			now := time.Now()
			var previousTurn string
			for _, startedAt := range []any{nil, int64(0), int64(1740000000000)} {
				c := newCodexSessionIdentityV2Context(t, 1, 11)
				cm := map[string]any{"session_id": "root", "thread_id": "child", "parent_turn_id": "parent-turn", "root_turn_id": "root-turn"}
				if startedAt != nil {
					cm["turn_started_at_unix_ms"] = startedAt
				}
				body := map[string]any{"client_metadata": cm}
				stageCodexSessionIdentityInputMap(c, body)
				ids, err := svc.resolveCodexHTTPFingerprintIDs(context.Background(), c, account, now)
				require.NoError(t, err)
				stageCodexFingerprintIDs(c, ids)
				headers := make(http.Header)
				var out []byte
				if passthrough {
					out, _, _, err = svc.normalizeCodexOutboundIdentityRaw(context.Background(), c, account, headers, mustJSONForSessionIdentityTest(t, body), "")
				} else {
					_, _, err = svc.normalizeCodexOutboundIdentityMap(context.Background(), c, account, headers, body, "")
					out = mustJSONForSessionIdentityTest(t, body)
				}
				require.NoError(t, err)
				result := codexTopologyOutbound{headers, gjson.ParseBytes(out)}
				require.True(t, isCodexUUIDv7(ids.turnID))
				require.NotEqual(t, previousTurn, ids.turnID)
				previousTurn = ids.turnID
				wantTime := now.UnixMilli()
				if startedAt != nil {
					wantTime = startedAt.(int64)
				}
				for _, carrier := range result.carriers() {
					require.Equal(t, ids.turnID, carrier.Get("turn_id").String())
					require.Equal(t, "parent-turn", carrier.Get("parent_turn_id").String())
					require.Equal(t, "root-turn", carrier.Get("root_turn_id").String())
					require.Equal(t, wantTime, carrier.Get("turn_started_at_unix_ms").Int())
				}
			}
		})
	}
}

func TestCodexSessionPeriodTopologyAcrossEpoch(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		t.Run(fmt.Sprintf("passthrough=%v", passthrough), func(t *testing.T) {
			svc := newCodexPeriodRedisService(t, miniredis.RunT(t))
			account := newTestOAuthAccount(7612, map[string]any{codexFingerprintModeExtraKey: "session"})
			seed, _ := codexFingerprintSeed(account.Extra)
			period := resolveCodexSessionPeriod(seed, "user:1", codexSessionIdentityUpstreamScope(account), time.Now())
			boundary := period.expiresAt.Add(-codexSessionPeriodGrace)
			previous := make([]codexTopologyOutbound, 3)
			for epoch, now := range []time.Time{boundary.Add(-time.Millisecond), boundary} {
				current := make([]codexTopologyOutbound, 3)
				for i, fixture := range []string{codexRootTopology, codexChildTopology, codexSideTopology} {
					c := newCodexSessionIdentityV2Context(t, 1, 11)
					body := mustJSONForSessionIdentityTest(t, map[string]any{
						"model": "gpt-5.2", "input": []any{},
						"prompt_cache_key": gjson.Get(fixture, "session_id").String(),
						"client_metadata":  map[string]any{openAIWSTurnMetadataHeader: fixture},
					})
					stageCodexSessionIdentityInputRaw(c, body)
					ids, err := svc.resolveCodexHTTPFingerprintIDs(context.Background(), c, account, now)
					require.NoError(t, err)
					stageCodexFingerprintIDs(c, ids)
					var req *http.Request
					if passthrough {
						req, err = svc.buildUpstreamRequestOpenAIPassthrough(context.Background(), c, account, body, "test")
					} else {
						req, err = svc.buildUpstreamRequest(context.Background(), c, account, body, "test", true, "", true)
					}
					require.NoError(t, err)
					out, err := io.ReadAll(req.Body)
					require.NoError(t, err)
					require.NoError(t, req.Body.Close())
					current[i] = codexTopologyOutbound{req.Header, gjson.ParseBytes(out)}
					for _, carrier := range current[i].carriers() {
						for _, field := range []string{"turn_id", "parent_turn_id", "root_turn_id"} {
							require.Equal(t, gjson.Get(fixture, field).String(), carrier.Get(field).String())
						}
					}
					if epoch > 0 {
						require.NotEqual(t, previous[i].session(), current[i].session())
						require.NotEqual(t, previous[i].thread(), current[i].thread())
						require.NotEqual(t, previous[i].cache(), current[i].cache())
					}
				}
				root, child, side := current[0], current[1], current[2]
				require.Equal(t, root.session(), child.session())
				require.Equal(t, root.session(), side.session())
				require.Equal(t, root.cache(), child.cache())
				require.NotEqual(t, root.cache(), side.cache())
				require.Equal(t, root.thread(), child.headers.Get("x-codex-parent-thread-id"))
				require.Equal(t, root.thread(), side.body.Get("client_metadata.forked_from_thread_id").String())
				previous = current
			}
		})
	}
}
