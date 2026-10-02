package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractCodexWSMetadataHeaders(t *testing.T) {
	headers := extractCodexWSMetadataHeaders([]byte(`{"type":"codex.response.metadata","headers":{"x-codex-turn-state":"gAAAAAstate","set-cookie":["__cflb=edge; Max-Age=60","__oailb=lb; Max-Age=60"]}}`))
	require.Equal(t, "gAAAAAstate", headers.Get("x-codex-turn-state"))
	require.Len(t, headers.Values("Set-Cookie"), 2)
}

func TestExtractCodexWSMetadataHeadersIgnoresUnrelatedSecrets(t *testing.T) {
	headers := extractCodexWSMetadataHeaders([]byte(`{"headers":{"authorization":"Bearer secret","cookie":"session=secret"}}`))
	require.Empty(t, headers)
}
