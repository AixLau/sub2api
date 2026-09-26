package transport

import (
	"testing"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/stretchr/testify/require"
)

func TestReadRequestBodyUsesHostLimit(t *testing.T) {
	for _, tc := range []struct {
		name            string
		declared, limit int64
		chunks          []string
		wantError       bool
	}{
		{"exact", 6, 6, []string{"abc", "def"}, false},
		{"declared_oversize", 7, 6, nil, true},
		{"chunked_exact", -1, 6, []string{"abc", "def"}, false},
		{"chunked_oversize", -1, 6, []string{"abc", "defg"}, true},
		{"invalid_policy", 0, 0, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &bodyInputTestStream{}
			for _, chunk := range tc.chunks {
				s.frames = append(s.frames, &pluginv1.ForwardRequest{Frame: &pluginv1.ForwardRequest_BodyChunk{BodyChunk: []byte(chunk)}})
			}
			s.frames = append(s.frames, &pluginv1.ForwardRequest{Frame: &pluginv1.ForwardRequest_BodyEnd{BodyEnd: true}})
			body, err := readRequestBody(s, &pluginv1.ForwardRequestStart{HasBody: true, ContentLength: tc.declared}, tc.limit)
			if tc.wantError {
				require.ErrorContains(t, err, "gateway.max_body_size")
			} else {
				require.NoError(t, err)
				require.Equal(t, "abcdef", string(body))
			}
		})
	}
	// A host-approved request over 64 MiB passes the length gate and reaches
	// the stream reader. This is not rejected by a second, private plugin cap.
	_, err := readRequestBody(&bodyInputTestStream{}, &pluginv1.ForwardRequestStart{HasBody: true, ContentLength: 65 << 20}, 128<<20)
	require.ErrorContains(t, err, "body_end 前中断")
}
