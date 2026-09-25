package transport

import (
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"strings"
	"testing"
)

type probeBody struct {
	io.Reader
	closed bool
}

func (b *probeBody) Close() error { b.closed = true; return nil }

func TestFailureProbePreservesBytesAndClose(t *testing.T) {
	failed := wireEvent("response.failed", map[string]any{"response": map[string]any{"instructions": strings.Repeat("x", 64<<10), "error": map[string]string{"code": "invalid_encrypted_content", "message": "cannot decrypt"}}})
	for _, tc := range []struct {
		name, body string
		status     int
		code       string
	}{
		{"long failure", failed, 200, "invalid_encrypted_content"},
		{"already output", wireEvent("response.output_text.delta", map[string]any{"delta": "hello"}) + failed, 200, ""},
		{"created with output", wireEvent("response.created", map[string]any{"response": map[string]any{"output": []any{map[string]any{"type": "function_call"}}}}) + failed, 200, ""},
		{"size cap", strings.Repeat("x", maxErrorProbeBytes+1), 200, ""},
		{"HTTP size cap", strings.Repeat("x", maxErrorProbeBytes+1), 502, ""},
		{"direct error", wireEvent("error", map[string]any{"code": "invalid_encrypted_content", "message": "cannot decrypt"}), 200, "invalid_encrypted_content"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &probeBody{Reader: strings.NewReader(tc.body)}
			response := &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: body}
			code, _, err := probeUpstreamFailure(response)
			require.NoError(t, err)
			require.Equal(t, tc.code, code)
			restored, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.Equal(t, tc.body, string(restored))
			require.False(t, body.closed)
			require.NoError(t, response.Body.Close())
			require.True(t, body.closed)
		})
	}
}
