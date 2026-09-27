package transport

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/stretchr/testify/require"
)

// Verify what the host actually receives through the public gRPC boundary:
// a BPS-only 403 must never enter its generic OAuth cooldown path.
func TestBPS403RoutesCurrentRequestToNative(t *testing.T) {
	for _, tc := range []struct {
		name         string
		enabled      bool
		firstStatus  int
		nativeStatus int
		contentType  string
		wantBPS      int32
	}{
		{"first JSON rejection", true, 403, 200, "application/json", 1},
		{"first SSE rejection", true, 403, 200, "text/event-stream", 1},
		{"transient retry rejection", true, 500, 200, "application/json", 2},
		{"encrypted retry rejection", true, 400, 200, "application/json", 2},
		{"native rejection remains visible", true, 403, 403, "application/json", 1},
		{"disabled protection preserves rejection", false, 403, 200, "application/json", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			store := &testHostKV{values: map[string][]byte{}}
			var bpsHits, nativeHits atomic.Int32
			bps := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempt := bpsHits.Add(1)
				w.Header().Set("Content-Type", tc.contentType)
				if attempt == 1 && tc.firstStatus != 403 {
					w.WriteHeader(tc.firstStatus)
					if tc.firstStatus == 400 {
						io.WriteString(w, `{"error":{"code":"invalid_encrypted_content","message":"Invalid encrypted history"}}`)
					} else {
						io.WriteString(w, `{"error":{"code":"server_error","message":"An error occurred while processing the request"}}`)
					}
					return
				}
				w.WriteHeader(http.StatusForbidden)
				io.WriteString(w, `{"error":{"code":"permission_denied","message":"BPS access denied"}}`)
			}))
			defer bps.Close()
			body := []byte(`{"model":"gpt-6-astra","input":[{"role":"user","content":"hello"},{"type":"reasoning","encrypted_content":"synthetic"}],"tools":[{"type":"function","name":"get_weather","parameters":{"type":"object"}}],"stream":false}`)
			nativeBodies := make(chan []byte, 2)
			native := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nativeHits.Add(1)
				raw, _ := io.ReadAll(r.Body)
				nativeBodies <- raw
				require.Equal(t, "Bearer synthetic", r.Header.Get("Authorization"))
				require.Empty(t, r.Header.Get("x-basispoints-auth-mode"))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.nativeStatus)
				if tc.nativeStatus == 403 {
					io.WriteString(w, `{"error":{"code":"permission_denied","message":"Native access denied"}}`)
				} else {
					io.WriteString(w, `{"id":"resp_native","status":"completed","output":[]}`)
				}
			}))
			defer native.Close()
			client := clientForTest(t, store, "")
			cfg, err := json.Marshal(map[string]any{
				"upstream_base_url": bps.URL, "native_upstream_base_url": native.URL,
				"proxy_mode": "disabled", "native_fallback": false, "tools_via_native": false,
				"auto_disable_bps_on_403": tc.enabled,
			})
			require.NoError(t, err)
			applied, err := client.ApplyConfig(ctx, &pluginv1.ApplyConfigRequest{ConfigJson: cfg})
			require.NoError(t, err)
			require.True(t, applied.Applied)
			// First request must recover immediately; subsequent requests bypass BPS.
			for i := 0; i < 2; i++ {
				start, output, failure := forwardForTest(t, client, body, ctx)
				require.Nil(t, failure)
				require.NotNil(t, start)
				if tc.enabled {
					require.EqualValues(t, tc.nativeStatus, start.StatusCode)
					require.Equal(t, body, <-nativeBodies, "native retry must use original unmodified history and tools")
					require.NotContains(t, string(output), "BPS access denied")
					if tc.nativeStatus == 403 {
						require.Contains(t, string(output), "Native access denied")
					}
				} else {
					require.EqualValues(t, 403, start.StatusCode)
					require.Contains(t, string(output), "BPS access denied")
				}
			}
			require.Equal(t, tc.wantBPS, bpsHits.Load())
			if tc.enabled {
				require.EqualValues(t, 2, nativeHits.Load())
			} else {
				require.Zero(t, nativeHits.Load())
			}
			store.mu.Lock()
			state, marked := store.values[bps403StateNamespace+"/7"]
			store.mu.Unlock()
			require.Equal(t, tc.enabled, marked)
			if marked {
				var persisted bps403State
				require.NoError(t, json.Unmarshal(state, &persisted))
				require.False(t, persisted.TriggeredAt.IsZero())
			}
		})
	}
}
