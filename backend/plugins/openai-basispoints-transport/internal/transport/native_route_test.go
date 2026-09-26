package transport

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	pluginconfig "github.com/Wei-Shaw/sub2api/plugins/openai-basispoints-transport/internal/config"
	"github.com/stretchr/testify/require"
)

// A request the Basis Points bridge cannot serve (image_input, image_generation,
// hosted tool_choice, structured output) must bypass the tool bridge and be
// forwarded verbatim to the native Codex upstream. Requests the bridge can
// serve keep the existing BPS wire shape. The client view stays channel-agnostic
// so the same conversation can alternate paths per request.
func TestNativeRouteSelection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	store := &testHostKV{values: map[string][]byte{}}

	var mu sync.Mutex
	var bpsHits, nativeHits [][]byte

	bps := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		raw, _ := io.ReadAll(req.Body)
		mu.Lock()
		bpsHits = append(bpsHits, raw)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"resp_bps","status":"completed","output":[]}`))
	}))
	defer bps.Close()
	native := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		raw, _ := io.ReadAll(req.Body)
		mu.Lock()
		nativeHits = append(nativeHits, raw)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"resp_native","status":"completed","output":[]}`))
	}))
	defer native.Close()

	c := clientForTest(t, store, "")
	apply := func(nativeFallback, toolsViaNative any) {
		cfg := map[string]any{
			"upstream_base_url":        bps.URL,
			"native_upstream_base_url": native.URL,
			"proxy_mode":               "disabled",
		}
		if nativeFallback != nil {
			cfg["native_fallback"] = nativeFallback
		}
		if toolsViaNative != nil {
			cfg["tools_via_native"] = toolsViaNative
		}
		raw, _ := json.Marshal(cfg)
		result, err := c.ApplyConfig(ctx, &pluginv1.ApplyConfigRequest{ConfigJson: raw})
		require.NoError(t, err)
		require.True(t, result.Applied)
	}
	counts := func() (int, int) {
		mu.Lock()
		defer mu.Unlock()
		return len(bpsHits), len(nativeHits)
	}

	tools := json.RawMessage(`[{"type":"function","name":"get_weather","parameters":{"type":"object"}}]`)

	// Case A: an input_image part routes to NATIVE and is forwarded byte-identical
	// (no wire-shape rewrite, no tool bridging, no metadata injection).
	apply(true, false)
	imageBody, err := json.Marshal(map[string]any{
		"model": "gpt-6-astra",
		"input": []any{map[string]any{
			"type": "message", "role": "user",
			"content": []any{
				map[string]any{"type": "input_image", "image_url": "data:image/png;base64,iVBORw0KGgo="},
			},
		}},
		"tools": tools,
	})
	require.NoError(t, err)
	_, outputA, failureA := forwardForTest(t, c, imageBody, ctx)
	require.Nil(t, failureA)
	_ = outputA
	mu.Lock()
	require.Len(t, nativeHits, 1, "image request must hit the native upstream")
	require.Len(t, bpsHits, 0, "image request must not reach the BPS upstream")
	capturedA := nativeHits[0]
	mu.Unlock()
	require.Equal(t, imageBody, capturedA, "native path must forward the original body verbatim")
	var decodedA map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(capturedA, &decodedA))
	require.Contains(t, decodedA, "tools")
	require.Contains(t, string(decodedA["input"]), "input_image")
	require.NotContains(t, decodedA, "metadata", "native path must not inject turn metadata")
	require.NotContains(t, decodedA, "model_selection", "native path must not rewrite the model field")

	health, err := c.Health(ctx, &pluginv1.HealthRequest{})
	require.NoError(t, err)
	require.Contains(t, health.StatusJson, `"native_requests":1`)

	// Case B: a plain text + client tools request stays on BPS with the bridge
	// wire shape (no tools key, explicit model_selection, turn metadata).
	textBody, err := json.Marshal(map[string]any{
		"model":  "gpt-6-astra",
		"input":  "Weather?",
		"tools":  tools,
		"stream": false,
	})
	require.NoError(t, err)
	_, _, failureB := forwardForTest(t, c, textBody, ctx)
	require.Nil(t, failureB)
	mu.Lock()
	require.Len(t, bpsHits, 1, "plain text request must hit the BPS upstream")
	require.Len(t, nativeHits, 1, "plain text request must not reach native")
	capturedB := bpsHits[0]
	mu.Unlock()
	var decodedB map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(capturedB, &decodedB))
	require.NotContains(t, decodedB, "tools", "BPS wire shape strips client tools")
	require.Equal(t, `"explicit"`, string(decodedB["model_selection"]))
	var metaB map[string]string
	require.NoError(t, json.Unmarshal(decodedB["metadata"], &metaB))
	require.NotEmpty(t, metaB["turn_id"])

	// Case C: native_fallback explicitly disabled keeps the image request on BPS
	// (current behavior preserved).
	apply(false, false)
	imageBodyC, err := json.Marshal(map[string]any{
		"model": "gpt-6-astra",
		"input": []any{map[string]any{
			"type": "message", "role": "user",
			"content": []any{
				map[string]any{"type": "input_image", "image_url": "https://example.com/cat.png"},
			},
		}},
		"tools": tools,
	})
	require.NoError(t, err)
	_, _, failureC := forwardForTest(t, c, imageBodyC, ctx)
	require.Nil(t, failureC)
	bpsC, nativeC := counts()
	require.Equal(t, 2, bpsC, "disabled fallback must keep the image request on BPS")
	require.Equal(t, 1, nativeC, "disabled fallback must not route to native")

	// Case D: with tools_via_native explicitly enabled, a client-tool request
	// goes native even without any capability trigger. The default is OFF —
	// tool sessions belong on the BPS bridge.
	apply(nil, true)
	toolsBodyD, err := json.Marshal(map[string]any{
		"model":  "gpt-6-astra",
		"input":  "Weather?",
		"tools":  tools,
		"stream": false,
	})
	require.NoError(t, err)
	_, _, failureD := forwardForTest(t, c, toolsBodyD, ctx)
	require.Nil(t, failureD)
	bpsD, nativeD := counts()
	require.Equal(t, 2, bpsD, "tool request must not reach BPS under tools_via_native")
	require.Equal(t, 2, nativeD, "tool request must reach the native upstream")
}

func TestResolveNativeTarget(t *testing.T) {
	cases := []struct {
		name     string
		base     string
		incoming string
		want     string
	}{
		{
			name:     "host URL verbatim when override empty",
			base:     "",
			incoming: "https://chatgpt.com/backend-api/codex/responses?q=1",
			want:     "https://chatgpt.com/backend-api/codex/responses?q=1",
		},
		{
			name:     "override base appends responses suffix and keeps query",
			base:     "https://codex.example.com/v1",
			incoming: "https://chatgpt.com/backend-api/codex/responses?q=1",
			want:     "https://codex.example.com/v1/responses?q=1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := pluginconfig.Defaults()
			cfg.NativeUpstreamBaseURL = tc.base
			target, err := resolveNativeTarget(tc.incoming, cfg)
			require.NoError(t, err)
			require.Equal(t, tc.want, target.String())
		})
	}
}

func TestEnsureCodexIdentity(t *testing.T) {
	h := http.Header{}
	h.Set("User-Agent", "codex-tui/0.155.1 (Mac OS 26.6.2; arm64) ghostty/1.3.1")
	ensureCodexIdentity(h)
	require.Equal(t, "codex-tui", h.Get("originator"))
	require.Equal(t, "0.155.1", h.Get("version"))
	require.Equal(t, "responses=experimental", h.Get("OpenAI-Beta"))
	require.Contains(t, h.Get("User-Agent"), "codex-tui/0.155.1")

	// Legacy originator and versions below the floor are corrected together.
	h2 := http.Header{}
	h2.Set("User-Agent", "codex_cli_rs/0.100.0 (Ubuntu 22.4.0; x86_64)")
	ensureCodexIdentity(h2)
	require.Equal(t, "codex_cli_rs", h2.Get("originator"))
	require.Equal(t, "0.146.0", h2.Get("version"))
	require.Contains(t, h2.Get("User-Agent"), "codex_cli_rs/0.146.0")

	// Non-Codex clients adopt the canonical identity pair.
	h3 := http.Header{}
	h3.Set("User-Agent", "Go-http-client/2.0")
	ensureCodexIdentity(h3)
	require.Equal(t, "codex-tui", h3.Get("originator"))
	require.Equal(t, "0.146.0", h3.Get("version"))
	require.True(t, strings.HasPrefix(h3.Get("User-Agent"), "codex-tui/0.146.0"))
}

func TestEncryptedContentRetryOnInvalidEncrypted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var hits atomic.Int32
	var bodies []string
	var mu sync.Mutex
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		raw, _ := io.ReadAll(req.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		mu.Unlock()
		n := hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			w.WriteHeader(502)
			w.Write([]byte(`{"response":{"error":{"code":"invalid_encrypted_content","message":"Encrypted function output content could not be decrypted or decoded."}}}`))
			return
		}
		w.Write([]byte(`{"id":"resp_ok","status":"completed","output":[]}`))
	}))
	defer upstream.Close()

	c := clientForTest(t, &testHostKV{values: map[string][]byte{}}, "")
	cfg, _ := json.Marshal(map[string]any{"upstream_base_url": upstream.URL, "proxy_mode": "disabled"})
	applied, err := c.ApplyConfig(ctx, &pluginv1.ApplyConfigRequest{ConfigJson: cfg})
	require.NoError(t, err)
	require.True(t, applied.Applied)

	body, _ := json.Marshal(map[string]any{
		"model": "gpt-6-astra",
		"input": []any{
			map[string]any{"type": "reasoning", "summary": []any{}, "encrypted_content": "gAAA==foreign"},
			map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": "hi"}}},
		},
	})
	start, _, failure := forwardForTest(t, c, body, ctx)
	require.Nil(t, failure, "the stripped retry must succeed")
	require.Equal(t, int32(200), start.StatusCode)
	require.Equal(t, int32(2), hits.Load(), "first attempt plus one stripped retry")
	mu.Lock()
	defer mu.Unlock()
	require.Contains(t, bodies[0], "encrypted_content")
	require.NotContains(t, bodies[1], "encrypted_content", "retry must strip channel-scoped ciphertexts")
}

func TestRetryOnTransientServerError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if hits.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(502)
			w.Write([]byte(`{"error":{"code":"server_error","message":"An error occurred while processing your request. You can retry your request, or contact us through our help center at help.openai.com if the error persists. Please include the request ID d31fb5c8-c1d8-4aec-9e8b-dcd3df1f0aab in your message.","param":null,"type":"server_error"},"sequence_number":1,"type":"error"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"resp_ok","status":"completed","output":[]}`))
	}))
	defer upstream.Close()

	c := clientForTest(t, &testHostKV{values: map[string][]byte{}}, "")
	cfg, _ := json.Marshal(map[string]any{"upstream_base_url": upstream.URL, "proxy_mode": "disabled"})
	applied, err := c.ApplyConfig(ctx, &pluginv1.ApplyConfigRequest{ConfigJson: cfg})
	require.NoError(t, err)
	require.True(t, applied.Applied)

	body, _ := json.Marshal(map[string]any{"model": "gpt-6-astra", "input": "hi"})
	start, _, failure := forwardForTest(t, c, body, ctx)
	require.Nil(t, failure, "the invited retry must recover")
	require.Equal(t, int32(200), start.StatusCode)
	require.Equal(t, int32(2), hits.Load(), "one retry on the upstream's own retryable error")
}

func TestTransientRetriesAreBounded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(502)
		w.Write([]byte(`{"error":{"code":"server_error","message":"An error occurred while processing your request. You can retry your request.","type":"server_error"},"type":"error"}`))
	}))
	defer upstream.Close()

	c := clientForTest(t, &testHostKV{values: map[string][]byte{}}, "")
	cfg, _ := json.Marshal(map[string]any{"upstream_base_url": upstream.URL, "proxy_mode": "disabled"})
	applied, err := c.ApplyConfig(ctx, &pluginv1.ApplyConfigRequest{ConfigJson: cfg})
	require.NoError(t, err)
	require.True(t, applied.Applied)

	body, _ := json.Marshal(map[string]any{"model": "gpt-6-astra", "input": "hi"})
	start, _, failure := forwardForTest(t, c, body, ctx)
	require.Nil(t, failure)
	require.Equal(t, int32(502), start.StatusCode, "the error is surfaced after retries are exhausted")
	require.Equal(t, int32(11), hits.Load(), "initial attempt plus at most 10 retries")
}

func TestRecentCompletedRequestHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	bps := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"resp_bps","status":"completed","output":[]}`))
	}))
	defer bps.Close()
	native := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"resp_native","status":"completed","output":[]}`))
	}))
	defer native.Close()

	c := clientForTest(t, &testHostKV{values: map[string][]byte{}}, "")
	cfg, _ := json.Marshal(map[string]any{
		"upstream_base_url":        bps.URL,
		"native_upstream_base_url": native.URL,
		"proxy_mode":               "disabled",
	})
	applied, err := c.ApplyConfig(ctx, &pluginv1.ApplyConfigRequest{ConfigJson: cfg})
	require.NoError(t, err)
	require.True(t, applied.Applied)

	textBody, _ := json.Marshal(map[string]any{"model": "gpt-6-astra", "input": "hello"})
	_, _, failure := forwardForTest(t, c, textBody, ctx)
	require.Nil(t, failure)
	imageBody, _ := json.Marshal(map[string]any{
		"model": "gpt-6-astra",
		"input": []any{map[string]any{"type": "message", "role": "user", "content": []any{
			map[string]any{"type": "input_image", "image_url": "data:image/png;base64,iVBORw0KGgo="},
		}}},
	})
	_, _, failure = forwardForTest(t, c, imageBody, ctx)
	require.Nil(t, failure)

	entries := completedRequestsForTest(t, c, ctx, 2)
	require.Equal(t, "bps", entries[0].Route)
	require.Equal(t, "selected", entries[0].Reason)
	require.Equal(t, "gpt-6-astra", entries[0].Model)
	require.Equal(t, "resp_bps", entries[0].ResponseID)
	require.NotEmpty(t, entries[0].Time)
	require.Equal(t, "native", entries[1].Route)
	require.Equal(t, "image_input", entries[1].Reason)
	require.Equal(t, "resp_native", entries[1].ResponseID)
	require.Equal(t, entries[0].ConfigRevision, entries[1].ConfigRevision)
}
