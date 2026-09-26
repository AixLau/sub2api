package transport

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNativeFailureObserver(t *testing.T) {
	for _, tc := range []struct {
		name, body  string
		sse, failed bool
	}{
		{"json_failed", `{"status":"failed","error":{"message":"reason"}}`, false, true},
		{"json_incomplete", `{"status":"incomplete"}`, false, true},
		{"json_success", `{"status":"completed","error":null,"output":[{"text":"response.failed"}]}`, false, false},
		{"stream_event", "event: response.failed\r\ndata: {}\r\n\r\n", true, true},
		{"stream_data", "data: {\"type\":\"response.failed\"}", true, true},
		{"stream_multiline", "data: {\ndata: \"type\":\"error\"}\n\n", true, true},
		{"stream_incomplete", "event: response.incomplete\ndata: {}\n\n", true, true},
		{"stream_success", "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\ndata: [DONE]\n\n", true, false},
		{"stream_text_not_error", "data: {\"type\":\"response.output_text.delta\",\"delta\":\"response.failed\"}\n\n", true, false},
		{"huge_delta_then_failure", "data: " + strings.Repeat("x", maxErrorProbeBytes+100) + "\n\nevent: response.failed\ndata: {}\n\n", true, true},
		{"huge_delta_then_success", "data: " + strings.Repeat("x", maxErrorProbeBytes+100) + "\n\nevent: response.completed\ndata: {}\n\n", true, false},
	} {
		for _, chunkSize := range []int{1, 17, 8192} {
			t.Run(fmt.Sprintf("%s/chunk=%d", tc.name, chunkSize), func(t *testing.T) {
				o := nativeFailureObserver{sse: tc.sse}
				for body := []byte(tc.body); len(body) > 0; {
					n := min(len(body), chunkSize)
					o.feed(body[:n])
					body = body[n:]
					if len(o.line) > maxErrorProbeBytes || len(o.data) > maxErrorProbeBytes {
						t.Fatal("observer exceeded buffer limit")
					}
				}
				require.Equal(t, tc.failed, o.finish())
			})
		}
	}
}
