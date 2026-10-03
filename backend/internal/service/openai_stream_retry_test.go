package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsOpenAIHTTP2StreamReadFailover(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want bool
	}{
		{
			name: "http2 reset",
			body: `{"error":{"message":"OpenAI stream disconnected before completion: stream error: stream ID 3241; INTERNAL_ERROR; received from peer"}}`,
			want: true,
		},
		{
			name: "http2 client connection lost",
			body: `{"error":{"message":"OpenAI stream disconnected before completion: http2: client connection lost"}}`,
			want: true,
		},
		{
			name: "generic eof",
			body: `{"error":{"message":"OpenAI stream disconnected before completion: unexpected EOF"}}`,
			want: false,
		},
		{
			name: "pre-output failover marked safe after keepalive",
			body: `{"error":{"message":"stream error: stream ID 7; INTERNAL_ERROR; received from peer"}}`,
			want: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			failover := &UpstreamFailoverError{
				StatusCode:               http.StatusBadGateway,
				ResponseBody:             []byte(tc.body),
				SafeToFailoverAfterWrite: tc.name == "pre-output failover marked safe after keepalive",
			}
			require.Equal(t, tc.want, isOpenAIHTTP2StreamReadFailover(failover))
		})
	}
}

func TestIsOpenAIHTTP2StreamReadFailoverUnwrapsErrors(t *testing.T) {
	body, err := json.Marshal(map[string]any{
		"error": map[string]string{"message": "stream error: stream ID 9; INTERNAL_ERROR; received from peer"},
	})
	require.NoError(t, err)
	wrapped := fmt.Errorf("wrapped: %w", &UpstreamFailoverError{ResponseBody: body})
	require.True(t, isOpenAIHTTP2StreamReadFailover(wrapped))
}
