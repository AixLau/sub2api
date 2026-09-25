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
// single multipart "file" field to /attachments next to /responses and then
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
			_, err = reader.NextPart()
			require.Equal(t, io.EOF, err)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"openai_file_id":"file-uploaded"}`))
		case "/responses":
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
	config, _ := json.Marshal(map[string]any{"upstream_base_url": upstream.URL, "proxy_mode": "disabled", "native_fallback": false})
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
	config, _ := json.Marshal(map[string]any{"upstream_base_url": upstream.URL, "proxy_mode": "disabled", "native_fallback": false})
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
