package service

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func newOpenAICapacityRerouteTestRequest(t *testing.T, state *OpenAICapacityRerouteState) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(
		WithOpenAICapacityReroute(context.Background(), state),
		http.MethodPost,
		"https://chatgpt.com/backend-api/codex/responses",
		nil,
	)
	require.NoError(t, err)
	return req
}

func openAICapacityRerouteResponseHeader(ticket string) http.Header {
	if ticket == "" {
		return http.Header{}
	}
	return http.Header{http.CanonicalHeaderKey(openAICodexTurnStateHeader): []string{ticket}}
}

func openAICapacityRerouteTestAccount() *Account {
	return &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
}

func TestOpenAICapacityRerouteKeepsTicketOnFirstRetry(t *testing.T) {
	state := NewOpenAICapacityRerouteState()
	ctx := WithOpenAICapacityReroute(context.Background(), state)
	req := newOpenAICapacityRerouteTestRequest(t, state)
	req.Header.Set(pluginOutboundTicketHeader, "old-ticket")
	req.Header.Set("Cookie", "__cflb=edge-a; __oailb=edge-b; session=keep")

	AdvanceOpenAICapacityReroute(ctx, openAICapacityRerouteResponseHeader("upstream-new-ticket"))
	require.Equal(t, 1, OpenAICapacityRerouteStage(ctx))

	applyOpenAICapacityReroute(req, openAICapacityRerouteTestAccount())

	require.Equal(t, "old-ticket", req.Header.Get(pluginOutboundTicketHeader))
	require.Contains(t, req.Header.Get("Cookie"), "__cflb=edge-a")
	require.Contains(t, req.Header.Get("Cookie"), "__oailb=edge-b")
}

func TestOpenAICapacityRerouteReplacesTicketOnSecondRetry(t *testing.T) {
	state := NewOpenAICapacityRerouteState()
	ctx := WithOpenAICapacityReroute(context.Background(), state)
	req := newOpenAICapacityRerouteTestRequest(t, state)
	req.Header.Set(pluginOutboundTicketHeader, "old-ticket")

	AdvanceOpenAICapacityReroute(ctx, openAICapacityRerouteResponseHeader("upstream-new-ticket"))
	applyOpenAICapacityReroute(req, openAICapacityRerouteTestAccount())
	require.Equal(t, "old-ticket", req.Header.Get(pluginOutboundTicketHeader))

	AdvanceOpenAICapacityReroute(ctx, openAICapacityRerouteResponseHeader("upstream-new-ticket"))
	require.Equal(t, 2, OpenAICapacityRerouteStage(ctx))

	req.Header.Set(pluginOutboundTicketHeader, "old-ticket")
	applyOpenAICapacityReroute(req, openAICapacityRerouteTestAccount())

	require.Equal(t, "upstream-new-ticket", req.Header.Get(pluginOutboundTicketHeader))
}

func TestOpenAICapacityRerouteDropsTicketWhenNoNewTicketArrives(t *testing.T) {
	state := NewOpenAICapacityRerouteState()
	ctx := WithOpenAICapacityReroute(context.Background(), state)
	req := newOpenAICapacityRerouteTestRequest(t, state)
	req.Header.Set(pluginOutboundTicketHeader, "old-ticket")

	AdvanceOpenAICapacityReroute(ctx, http.Header{})
	applyOpenAICapacityReroute(req, openAICapacityRerouteTestAccount())

	AdvanceOpenAICapacityReroute(ctx, http.Header{})
	req.Header.Set(pluginOutboundTicketHeader, "old-ticket")
	applyOpenAICapacityReroute(req, openAICapacityRerouteTestAccount())

	require.Empty(t, req.Header.Get(pluginOutboundTicketHeader))
}

func TestOpenAICapacityRerouteDropsTicketOnThirdRetryAndStripsRouteCookies(t *testing.T) {
	state := NewOpenAICapacityRerouteState()
	ctx := WithOpenAICapacityReroute(context.Background(), state)
	req := newOpenAICapacityRerouteTestRequest(t, state)
	req.Header.Set(pluginOutboundTicketHeader, "old-ticket")
	req.Header.Set("Cookie", "__cflb=edge-a; __oailb=edge-b; session=keep")

	for i := 0; i < 3; i++ {
		AdvanceOpenAICapacityReroute(ctx, http.Header{})
	}
	require.Equal(t, 3, OpenAICapacityRerouteStage(ctx))

	applyOpenAICapacityReroute(req, openAICapacityRerouteTestAccount())

	require.Empty(t, req.Header.Get(pluginOutboundTicketHeader))
	require.Equal(t, "session=keep", req.Header.Get("Cookie"))
}

func TestStripOpenAIRouteCookiesPreservesOtherCookiesAcrossHeaders(t *testing.T) {
	headers := http.Header{}
	headers["cookie"] = []string{"__cflb=edge-a; session=keep"}
	headers["Cookie"] = []string{"__oailb=edge-b; theme=dark"}

	stripOpenAIRouteCookies(headers)

	require.ElementsMatch(t, []string{"session=keep", "theme=dark"}, strings.Split(headers.Get("Cookie"), "; "))
}

func TestOpenAICapacityRerouteResetClearsStageAndOverride(t *testing.T) {
	state := NewOpenAICapacityRerouteState()
	ctx := WithOpenAICapacityReroute(context.Background(), state)

	for i := 0; i < 3; i++ {
		AdvanceOpenAICapacityReroute(ctx, openAICapacityRerouteResponseHeader("new-ticket"))
	}
	require.Equal(t, 3, OpenAICapacityRerouteStage(ctx))

	ResetOpenAICapacityReroute(ctx)

	require.Zero(t, OpenAICapacityRerouteStage(ctx))
	require.Empty(t, state.override())
}
