package transport

import (
	"context"
	"encoding/json"
	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNativeResponseHeadersUseOverallRequestBudget(t *testing.T) {
	for _, tc := range []struct {
		name      string
		native    bool
		budget    int
		completed bool
	}{
		{"native delayed structured output", true, 3, true},
		{"BPS header limit remains enforced", false, 3, false},
		{"native overall limit remains enforced", true, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				timer := time.NewTimer(1200 * time.Millisecond)
				defer timer.Stop()
				select {
				case <-timer.C:
				case <-r.Context().Done():
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_delayed\",\"status\":\"completed\",\"output\":[]}}\n\n"))
			}))
			defer server.Close()
			c := clientForTest(t, &testHostKV{values: map[string][]byte{}}, "")
			cfg, err := json.Marshal(map[string]any{"upstream_base_url": server.URL, "native_upstream_base_url": server.URL, "proxy_mode": "disabled", "native_fallback": true, "response_header_timeout_seconds": 1, "request_timeout_seconds": tc.budget})
			require.NoError(t, err)
			applied, err := c.ApplyConfig(ctx, &pluginv1.ApplyConfigRequest{ConfigJson: cfg})
			require.NoError(t, err)
			require.True(t, applied.Applied)
			body := map[string]any{"model": "gpt-5.6-luna", "input": "hello", "stream": true}
			if tc.native {
				body["text"] = map[string]any{"format": map[string]any{"type": "json_schema", "name": "test", "schema": map[string]any{"type": "object"}}}
			}
			raw, err := json.Marshal(body)
			require.NoError(t, err)
			header, out, failure := forwardForTest(t, c, raw, ctx)
			if tc.completed {
				require.Nil(t, failure)
				require.EqualValues(t, 200, header.StatusCode)
				require.Contains(t, string(out), "response.completed")
			} else {
				require.NotNil(t, failure)
				require.True(t, failure.RequestSent)
				require.Nil(t, header, "the configured budget must expire before response headers")
				if tc.native {
					require.Equal(t, "UPSTREAM_REQUEST_FAILED", failure.Code)
				} else {
					// BPS transport failures use the existing empty-code contract;
					// the host normalizes it to UPSTREAM_REQUEST_FAILED.
					require.Empty(t, failure.Code)
				}
			}
		})
	}
}

func TestNativeSSECommentsDoNotResetSemanticIdleTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(": transport activity\n\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	c := clientForTest(t, &testHostKV{values: map[string][]byte{}}, "")
	cfg, err := json.Marshal(map[string]any{
		"upstream_base_url": server.URL, "native_upstream_base_url": server.URL,
		"proxy_mode": "disabled", "request_timeout_seconds": 10,
		"response_idle_timeout_seconds": 1,
	})
	require.NoError(t, err)
	applied, err := c.ApplyConfig(ctx, &pluginv1.ApplyConfigRequest{ConfigJson: cfg})
	require.NoError(t, err)
	require.True(t, applied.Applied)
	raw, err := json.Marshal(map[string]any{"model": "gpt-6-sol", "input": "hello", "stream": true, "text": map[string]any{"format": map[string]any{"type": "json_schema", "name": "test", "schema": map[string]any{"type": "object"}}}})
	require.NoError(t, err)
	_, output, failure := forwardForTest(t, c, raw, ctx)
	require.NotEmpty(t, output)
	require.NotNil(t, failure)
	require.Equal(t, "UPSTREAM_RESPONSE_TIMEOUT", failure.Code)
}
