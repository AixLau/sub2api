package transport

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/stretchr/testify/require"
)

// The official contract: data-URL images in user messages are uploaded as a
// multipart "file" field with purpose=vision to /attachments next to /responses and then
// referenced by openai_file_id.
func TestForwardUploadsInlineImagesAsAttachments(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var uploads, responses atomic.Int32
	var uploaded []byte
	var adapted map[string]json.RawMessage
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/attachments":
			uploads.Add(1)
			require.Equal(t, "Bearer synthetic", req.Header.Get("Authorization"))
			require.Equal(t, "synthetic-account", req.Header.Get("X-OpenAI-Account-ID"))
			require.Equal(t, "chatgpt", req.Header.Get("X-Basispoints-Auth-Mode"))
			mediaType, params, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
			require.NoError(t, err)
			require.Equal(t, "multipart/form-data", mediaType)
			reader := multipart.NewReader(req.Body, params["boundary"])
			part, err := reader.NextPart()
			require.NoError(t, err)
			require.Equal(t, "file", part.FormName())
			require.Equal(t, "image.png", part.FileName())
			require.Equal(t, "image/png", part.Header.Get("Content-Type"))
			uploaded, err = io.ReadAll(part)
			require.NoError(t, err)
			part, err = reader.NextPart()
			require.NoError(t, err)
			require.Equal(t, "purpose", part.FormName())
			purpose, err := io.ReadAll(part)
			require.NoError(t, err)
			require.Equal(t, "vision", string(purpose))
			_, err = reader.NextPart()
			require.Equal(t, io.EOF, err)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"openai_file_id":"file-uploaded"}`))
		case "/responses":
			require.Equal(t, "true", req.Header.Get("Copilot-Vision-Request"))
			responses.Add(1)
			raw, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(raw, &adapted))
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":"resp1","status":"completed","output":[],"usage":{"total_tokens":1}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()

	store := &testHostKV{values: map[string][]byte{}}
	c := clientForTest(t, store, "")
	config, _ := json.Marshal(map[string]any{"upstream_base_url": upstream.URL, "proxy_mode": "disabled", "native_fallback": true})
	applied, err := c.ApplyConfig(ctx, &pluginv1.ApplyConfigRequest{ConfigJson: config})
	require.NoError(t, err)
	require.True(t, applied.Applied)

	image := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("png-bytes"))
	body, _ := json.Marshal(map[string]any{
		"model":  "gpt-5.6-luna",
		"stream": false,
		"input": []any{map[string]any{"type": "message", "role": "user", "content": []any{
			map[string]any{"type": "input_image", "image_url": image},
			map[string]any{"type": "input_text", "text": "what is this?"},
		}}},
	})
	start, output, failure := forwardForTest(t, c, body, ctx)
	require.Nil(t, failure)
	require.Equal(t, int32(200), start.StatusCode)
	require.Equal(t, int32(1), uploads.Load())
	require.Equal(t, int32(1), responses.Load())
	require.Equal(t, []byte("png-bytes"), uploaded)

	require.NotContains(t, string(adapted["input"]), "image_url")
	var input []json.RawMessage
	require.NoError(t, json.Unmarshal(adapted["input"], &input))
	item, err := parseTestObject(input[len(input)-1])
	require.NoError(t, err)
	var parts []json.RawMessage
	require.NoError(t, json.Unmarshal(item["content"], &parts))
	require.JSONEq(t, `{"type":"input_image","file_id":"file-uploaded","detail":"auto"}`, string(parts[0]))
	require.Contains(t, string(output), "resp1")
}

func TestForwardAttachmentFailureStopsBeforeResponses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var uploads, responses atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/attachments":
			uploads.Add(1)
			w.WriteHeader(http.StatusUnprocessableEntity)
			w.Write([]byte(`{"error":{"message":"422: Invalid file."}}`))
		case "/responses":
			responses.Add(1)
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer upstream.Close()

	c := clientForTest(t, &testHostKV{values: map[string][]byte{}}, "")
	config, _ := json.Marshal(map[string]any{"upstream_base_url": upstream.URL, "proxy_mode": "disabled", "native_fallback": true})
	applied, err := c.ApplyConfig(ctx, &pluginv1.ApplyConfigRequest{ConfigJson: config})
	require.NoError(t, err)
	require.True(t, applied.Applied)

	image := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("png-bytes"))
	body, _ := json.Marshal(map[string]any{
		"model":  "gpt-5.6-luna",
		"stream": false,
		"input": []any{map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "input_image", "image_url": image},
		}}},
	})
	_, _, failure := forwardForTest(t, c, body, ctx)
	require.NotNil(t, failure)
	require.Equal(t, "ATTACHMENT_UPLOAD_FAILED", failure.Code)
	require.False(t, failure.RequestSent)
	require.Contains(t, failure.Message, "422")
	require.Equal(t, int32(1), uploads.Load())
	require.Zero(t, responses.Load(), "responses must not be called when an attachment fails")
}

func parseTestObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var obj map[string]json.RawMessage
	err := json.Unmarshal(raw, &obj)
	return obj, err
}

// Exercise the default route through gRPC, including replayed tool media and
// requests that combine inline pictures with capabilities BPS cannot preserve.
func TestVisionRoutingPreservesCapabilities(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var uploads atomic.Int32
	type hit struct {
		body   []byte
		vision string
		native bool
	}
	hits := make(chan hit, 20)
	bps := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if req.URL.Path == "/attachments" {
			uploads.Add(1)
			io.WriteString(w, `{"openai_file_id":"file-vision"}`)
			return
		}
		raw, _ := io.ReadAll(req.Body)
		// Match the live BPS restriction: tool image arrays are rejected even
		// when data URLs have already become file IDs.
		var request struct {
			Input []map[string]json.RawMessage `json:"input"`
		}
		require.NoError(t, json.Unmarshal(raw, &request))
		for _, item := range request.Input {
			if string(item["type"]) != `"function_call_output"` {
				continue
			}
			var output string
			if json.Unmarshal(item["output"], &output) != nil {
				w.WriteHeader(http.StatusUnprocessableEntity)
				io.WriteString(w, `{"error":{"message":"422: Invalid request body."}}`)
				return
			}
			require.NotContains(t, output, "input_image")
		}
		hits <- hit{body: raw, vision: req.Header.Get("Copilot-Vision-Request")}
		io.WriteString(w, `{"id":"resp_bps","status":"completed","output":[]}`)
	}))
	defer bps.Close()
	native := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		raw, _ := io.ReadAll(req.Body)
		hits <- hit{body: raw, vision: req.Header.Get("Copilot-Vision-Request"), native: true}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"resp_native","status":"completed","output":[]}`)
	}))
	defer native.Close()
	c := clientForTest(t, &testHostKV{values: map[string][]byte{}}, "")
	cfg, _ := json.Marshal(map[string]any{"upstream_base_url": bps.URL, "native_upstream_base_url": native.URL, "proxy_mode": "disabled", "extra_headers": map[string]string{"Copilot-Vision-Request": "false"}})
	result, err := c.ApplyConfig(ctx, &pluginv1.ApplyConfigRequest{ConfigJson: cfg})
	require.NoError(t, err)
	require.True(t, result.Applied)
	inline := map[string]any{"type": "input_image", "image_url": "data:image/png;base64,c2NyZWVuc2hvdA=="}
	toolInput := []any{
		map[string]any{"type": "function_call", "name": "screenshot", "call_id": "call_screen", "arguments": "{}"},
		map[string]any{"type": "function_call_output", "call_id": "call_screen", "output": []any{inline}},
	}
	message := func(parts ...any) []any { return []any{map[string]any{"role": "user", "content": parts}} }
	cases := []struct {
		name   string
		input  any
		extra  map[string]any
		native bool
		reason string
	}{
		{name: "nested tool screenshot", input: toolInput},
		{name: "replayed tool screenshot", input: toolInput},
		{name: "plain text after picture", input: "hello"},
		{name: "schema with inline picture", input: message(inline), extra: map[string]any{"text": map[string]any{"format": map[string]any{"type": "json_schema", "name": "answer", "schema": map[string]any{"type": "object"}, "strict": true}}}, native: true, reason: "structured_output"},
		{name: "image generation with picture", input: message(inline), extra: map[string]any{"tools": []any{map[string]any{"type": "image_generation"}}}, native: true, reason: "image_generation"},
		{name: "forced hosted tool with picture", input: message(inline), extra: map[string]any{"tool_choice": map[string]any{"type": "web_search"}}, native: true, reason: "hosted_tool_choice"},
		{name: "mixed external file and inline", input: message(inline, map[string]any{"type": "input_image", "file_id": "file-external"}), native: true, reason: "image_input"},
		{name: "remote URL", input: message(map[string]any{"type": "input_image", "image_url": "https://example.test/screen.png"}), native: true, reason: "image_input"},
		{name: "same session returns to BPS", input: "continue without image references"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]any{"model": "gpt-6-astra", "input": tc.input}
			for k, v := range tc.extra {
				body[k] = v
			}
			raw, err := json.Marshal(body)
			require.NoError(t, err)
			start, _, failure := forwardForTest(t, c, raw, ctx)
			require.Nil(t, failure)
			require.Equal(t, int32(http.StatusOK), start.StatusCode)
			got := <-hits
			require.Equal(t, tc.native, got.native)
			if tc.native {
				require.Equal(t, raw, got.body)
				require.Empty(t, got.vision)
			} else if _, plainText := tc.input.(string); plainText {
				require.Empty(t, got.vision)
			} else {
				require.Equal(t, "true", got.vision)
				require.Contains(t, string(got.body), `"file_id":"file-vision"`)
				require.NotContains(t, string(got.body), "data:image")
				require.Contains(t, string(got.body), `"call_id":"call_screen"`)
			}
			entries := completedRequestsForTest(t, c, ctx, i+1)
			last := entries[len(entries)-1]
			if tc.native {
				require.Equal(t, tc.reason, last.Reason)
			} else {
				require.Equal(t, "selected", last.Reason)
			}
			require.Len(t, last.RouteHistory, 1)
		})
	}
	require.Equal(t, int32(1), uploads.Load(), "replays reuse attachments and native requests must not upload")
}
