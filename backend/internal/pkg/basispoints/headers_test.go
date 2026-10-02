package basispoints

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCredentialHeadersPrecedenceAndIsolation(t *testing.T) {
	credentials := map[string]any{
		"access_token": "secret",
		"headers":      map[string]any{"User-Agent": "old-browser", "X-Stainless-Lang": "js"},
		"captured_headers": map[string]any{
			"user-agent":               []any{"  captured-browser  "},
			"X-OpenAI-Account-User-Id": "captured-user",
			"X-OpenAI-Internal-Basispoints-Office-Platform": "Mac",
			"Authorization": "Bearer stale-secret", "Cookie": "secret-cookie",
			"Chatgpt-Account-Id": "other-account", "Origin": "https://untrusted.invalid",
			"X-Basispoints-Auth-Mode": "other-mode", "X-Unknown": "unknown",
		},
	}
	headers, err := CredentialHeaders(credentials)
	require.NoError(t, err)
	require.Len(t, headers, 4)
	require.Equal(t, "captured-browser", headers.Get("User-Agent"))
	require.Equal(t, "js", headers.Get("X-Stainless-Lang"))
	require.Equal(t, "Mac", headers.Get("X-OpenAI-Internal-Basispoints-Office-Platform"))
	credentials["captured_headers"].(map[string]any)["user-agent"] = "rotated-browser"
	next, err := CredentialHeaders(credentials)
	require.NoError(t, err)
	require.Equal(t, "rotated-browser", next.Get("User-Agent"))
	require.Equal(t, "captured-browser", headers.Get("User-Agent"))
	other, err := CredentialHeaders(nil)
	require.NoError(t, err)
	require.Empty(t, other)
}

func TestCredentialHeadersRejectMalformedIdentityWithoutValuesInErrors(t *testing.T) {
	for _, value := range []any{
		"not-an-object",
		map[string]any{"User-Agent": 42},
		map[string]any{"User-Agent": []any{"secret-value", 42}},
		map[string]any{"User-Agent": "secret-value\r\nInjected: true"},
		map[string]any{"User-Agent": "secret-value\t"},
		map[string]any{"User-Agent": strings.Repeat("x", MaxClientHeaderBytes+1)},
		map[string]any{"User-Agent": "secret-value", "user-agent": "other"},
	} {
		_, err := CredentialHeaders(map[string]any{"captured_headers": value})
		require.Error(t, err)
		require.NotContains(t, err.Error(), "secret-value")
	}
}

func TestCredentialHeadersBlankAndArrayValues(t *testing.T) {
	h, err := CredentialHeaders(map[string]any{
		"headers":          map[string]string{"user-agent": "base"},
		"captured_headers": http.Header{"user-agent": {" "}, "x-stainless-runtime": {"browser:safari"}},
	})
	require.NoError(t, err)
	require.Equal(t, "base", h.Get("User-Agent"))
	require.Equal(t, "browser:safari", h.Get("X-Stainless-Runtime"))
}
