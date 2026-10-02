package service

import (
	"net/http"
	"testing"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/stretchr/testify/require"
)

func TestApplyPluginOutboundHeaderMutationMergesOnlyRouteCookies(t *testing.T) {
	headers := make(http.Header)
	headers.Set("Cookie", "session=keep; __cflb=old")
	applyPluginOutboundHeaderMutation(headers, &pluginv1.PrepareOutboundResponse{
		HeadersToSet: map[string]*pluginv1.HeaderValues{
			pluginOutboundTicketHeader:      {Values: []string{"ticket"}},
			pluginOutboundRouteCookieHeader: {Values: []string{"__cflb=new; __oailb=pair"}},
			"Cookie":                        {Values: []string{"session=must-not-change"}},
		},
	})
	require.Equal(t, "ticket", headers.Get(pluginOutboundTicketHeader))
	require.Equal(t, "session=keep; __cflb=new; __oailb=pair", headers.Get("Cookie"))
	require.Empty(t, headers.Get(pluginOutboundRouteCookieHeader))
}

func TestApplyPluginOutboundHeaderMutationReplacesNonCanonicalTicket(t *testing.T) {
	headers := http.Header{"x-codex-turn-state": []string{"old"}}
	applyPluginOutboundHeaderMutation(headers, &pluginv1.PrepareOutboundResponse{
		HeadersToSet: map[string]*pluginv1.HeaderValues{
			pluginOutboundTicketHeader: {Values: []string{"new"}},
		},
	})
	require.Equal(t, []string{"new"}, headers.Values(pluginOutboundTicketHeader))
	_, hasLowercase := headers["x-codex-turn-state"]
	require.False(t, hasLowercase, "a direct lower-case map entry must not survive beside the replacement")
}

func TestMergePluginRouteCookiesRejectsArbitraryCookie(t *testing.T) {
	_, ok := mergePluginRouteCookies("", "session=secret")
	require.False(t, ok)
	_, ok = mergePluginRouteCookies("", "__cflb=ok\r\nX-Evil: yes")
	require.False(t, ok)
	_, ok = mergePluginRouteCookies("__cflb=old", "__cflb=one; __cflb=two")
	require.False(t, ok)
}

func TestExtractPluginRouteCookieSeedNeverReturnsGeneralCookies(t *testing.T) {
	seed, ok := extractPluginRouteCookieSeed("session=secret; __oailb=o; __cflb=c")
	require.True(t, ok)
	require.Equal(t, "__cflb=c; __oailb=o", seed)
	_, ok = extractPluginRouteCookieSeed("session=secret")
	require.False(t, ok)
}
