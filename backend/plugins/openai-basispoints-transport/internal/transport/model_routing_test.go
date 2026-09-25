package transport

import (
	"context"
	"encoding/json"
	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestModelRoutingSelectionAndConfigReload(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	type hit struct {
		path string
		body []byte
	}
	hits := make(chan hit, 20)
	server := func(path string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			hits <- hit{path, raw}
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"id":"resp_ok","status":"completed","output":[]}`)
		}))
	}
	bps, native := server("bps"), server("native")
	defer bps.Close()
	defer native.Close()
	client := clientForTest(t, &testHostKV{values: map[string][]byte{}}, "")
	cases := []struct {
		name, mode, model, want, upstreamModel string
		list                                   []string
		fallback, toolsNative, image           bool
	}{
		{name: "default all", model: "model-a", want: "bps", upstreamModel: "model-a"},
		{name: "unselected ignores disabled fallback", mode: "selected", model: "model-b-excel", list: []string{"model-a"}, want: "native"},
		{name: "selected before mapping", mode: "selected", model: "model-b-excel", list: []string{"model-b-excel"}, want: "bps", upstreamModel: "mapped-b"},
		{name: "suffix not normalized for selection", mode: "selected", model: "model-b-excel", list: []string{"model-b"}, want: "native"},
		{name: "case sensitive", mode: "selected", model: "Model-a", list: []string{"model-a"}, want: "native"},
		{name: "empty selected disables BPS", mode: "selected", model: "model-a", want: "native"},
		{name: "selected image still native", mode: "selected", model: "model-a", list: []string{"model-a"}, want: "native", fallback: true, image: true},
		{name: "selected tools obey native override", mode: "selected", model: "model-a", list: []string{"model-a"}, want: "native", toolsNative: true},
		{name: "reload all restores bridge", mode: "all", model: "model-a-excel", want: "bps", upstreamModel: "model-a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := map[string]any{"upstream_base_url": bps.URL, "native_upstream_base_url": native.URL, "proxy_mode": "disabled", "native_fallback": tc.fallback, "tools_via_native": tc.toolsNative, "bps_models": tc.list, "model_mapping": map[string]string{"model-b-excel": "mapped-b"}}
			if tc.mode != "" {
				cfg["bps_model_mode"] = tc.mode
			}
			config, _ := json.Marshal(cfg)
			applied, err := client.ApplyConfig(ctx, &pluginv1.ApplyConfigRequest{ConfigJson: config})
			require.NoError(t, err)
			require.True(t, applied.Applied)
			payload := map[string]any{"model": tc.model, "input": "hi", "stream": false, "tools": []any{map[string]any{"type": "function", "name": "test_tool", "parameters": map[string]any{"type": "object"}}}}
			if tc.image {
				payload["input"] = []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_image", "image_url": "https://example.test/image.png"}}}}
			}
			body, _ := json.Marshal(payload)
			_, _, failure := forwardForTest(t, client, body, ctx)
			require.Nil(t, failure)
			received := <-hits
			require.Equal(t, tc.want, received.path)
			if tc.want == "native" {
				require.Equal(t, body, received.body)
			} else {
				var decoded map[string]any
				require.NoError(t, json.Unmarshal(received.body, &decoded))
				require.Equal(t, tc.upstreamModel, decoded["model"])
				require.NotContains(t, decoded, "tools")
				require.Contains(t, decoded, "metadata")
			}
		})
	}
}
