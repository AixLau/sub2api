package plugin

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"strings"
	"testing"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/Wei-Shaw/sub2api/plugins/openai-codex-ticket/internal/config"
	"github.com/Wei-Shaw/sub2api/plugins/openai-codex-ticket/internal/routecookie"
	"github.com/Wei-Shaw/sub2api/plugins/openai-codex-ticket/internal/store"
	"github.com/Wei-Shaw/sub2api/plugins/openai-codex-ticket/internal/ticket"
	"github.com/stretchr/testify/require"
)

func TestObserveOutboundResponseBlocksDegradedModel(t *testing.T) {
	c := config.Defaults()
	c.Enabled = true
	c.BlockDegraded = true
	c.Models = []string{"gpt-6-astra"}
	p := &Plugin{}
	p.state.Store(&Runtime{cfg: c, rev: c.Revision(), store: store.New(nil)})

	resp, err := p.ObserveOutboundResponse(context.Background(), &pluginv1.ObserveOutboundResponseRequest{
		AccountId:   7,
		Model:       "gpt-6-astra",
		Platform:    "openai",
		AccountType: "oauth",
		Headers: map[string]*pluginv1.HeaderValues{
			observedServedModelHeader: {Values: []string{"gpt-5.6-sol"}},
			observedEventHeader:       {Values: []string{"response.created"}},
			observedCompleteHeader:    {Values: []string{"false"}},
		},
	})
	require.NoError(t, err)
	require.True(t, resp.Observed)
	require.Equal(t, "DEGRADED_MODEL", resp.ReasonCode)
}

func TestObserveOutboundResponseRecordsMatchingModel(t *testing.T) {
	c := config.Defaults()
	c.Enabled = true
	c.Models = []string{"gpt-6-astra"}
	p := &Plugin{}
	p.state.Store(&Runtime{cfg: c, rev: c.Revision(), store: store.New(nil)})

	resp, err := p.ObserveOutboundResponse(context.Background(), &pluginv1.ObserveOutboundResponseRequest{
		AccountId: 7, Model: "gpt-6-astra", Platform: "openai", AccountType: "oauth",
		Headers: map[string]*pluginv1.HeaderValues{observedServedModelHeader: {Values: []string{"gpt-6-astra"}}},
	})
	require.NoError(t, err)
	require.True(t, resp.Observed)
	require.Equal(t, "MODEL_MATCH", resp.ReasonCode)
	require.Len(t, p.events.snapshot(), 1)
}

func TestPrepareSteersRoutePoolWithoutTicket(t *testing.T) {
	c := config.Defaults()
	c.Enabled = true
	c.FailClosed = false
	c.Models = []string{"gpt-6-astra"}
	kv := store.NewMemoryKV()
	st := store.New(kv)
	pair := &routecookie.Pair{Values: map[string]string{routecookie.CFLB: "edge-a", routecookie.OAILB: "lb-a"}, SeenAt: time.Now()}
	require.NoError(t, st.MergeRoutePair(context.Background(), pair, time.Hour))
	p := &Plugin{}
	p.state.Store(&Runtime{cfg: c, rev: c.Revision(), store: st})

	resp, err := p.Prepare(context.Background(), PrepareRequest{AccountID: 7, Model: "gpt-6-astra", Platform: "openai", AccountType: "oauth", RequestID: "req-route-only"})
	require.NoError(t, err)
	require.Equal(t, "CONTINUE", resp.Action)
	require.Equal(t, "IDENTITY_UNAVAILABLE_FAIL_OPEN", resp.ReasonCode)
	require.Equal(t, "__cflb=edge-a; __oailb=lb-a", strings.Join(resp.HeadersToSet[routeCookieHeader], "; "))
}

func TestPrepareSteersPartialRoutePoolWithoutTicket(t *testing.T) {
	c := config.Defaults()
	c.Enabled = true
	c.Models = []string{"gpt-6-astra"}
	st := store.New(nil)
	require.NoError(t, st.MergeRoutePair(context.Background(), &routecookie.Pair{Values: map[string]string{routecookie.CFLB: "edge-partial"}, SeenAt: time.Now()}, time.Hour))
	p := &Plugin{}
	p.state.Store(&Runtime{cfg: c, rev: c.Revision(), store: st})

	resp, err := p.Prepare(context.Background(), PrepareRequest{AccountID: 7, Model: "gpt-6-astra", Platform: "openai", AccountType: "oauth", RequestID: "req-partial-route"})
	require.NoError(t, err)
	require.Equal(t, "__cflb=edge-partial", strings.Join(resp.HeadersToSet[routeCookieHeader], "; "))
}

func TestPreparePreservesClientTicketAndStillSteersRoute(t *testing.T) {
	c := config.Defaults()
	c.Enabled = true
	c.FailClosed = true
	c.Models = []string{"gpt-6-astra"}
	st := store.New(nil)
	require.NoError(t, st.MergeRoutePair(context.Background(), &routecookie.Pair{Values: map[string]string{routecookie.CFLB: "edge-b", routecookie.OAILB: "lb-b"}, SeenAt: time.Now()}, time.Hour))
	p := &Plugin{}
	p.state.Store(&Runtime{cfg: c, rev: c.Revision(), store: st})

	state := pluginTestState(t, 292, time.Now().Add(-time.Minute))
	resp, err := p.Prepare(context.Background(), PrepareRequest{AccountID: 7, Model: "gpt-6-astra", Platform: "openai", AccountType: "oauth", RequestID: "req-client-ticket", Headers: map[string][]string{turnStateHeader: {state}}})
	require.NoError(t, err)
	require.Equal(t, "CONTINUE", resp.Action)
	require.Equal(t, "CLIENT_TICKET_PRESERVED", resp.ReasonCode)
	require.Equal(t, "__cflb=edge-b; __oailb=lb-b", strings.Join(resp.HeadersToSet[routeCookieHeader], "; "))
}

func TestPrepareClearsStaleClientTicketBeforeFailOpen(t *testing.T) {
	c := config.Defaults()
	c.Enabled = true
	c.FailClosed = false
	c.Models = []string{"gpt-6-astra"}
	p := &Plugin{}
	p.state.Store(&Runtime{cfg: c, rev: c.Revision(), store: store.New(nil)})

	resp, err := p.Prepare(context.Background(), PrepareRequest{AccountID: 7, Model: "gpt-6-astra", Platform: "openai", AccountType: "oauth", RequestID: "req-stale-ticket", Headers: map[string][]string{turnStateHeader: {"stale-or-malformed"}}})
	require.NoError(t, err)
	require.Equal(t, "CONTINUE", resp.Action)
	require.Contains(t, resp.HeadersToDelete, turnStateHeader)
}

func TestObserveRouteOutcomeUsesTicketLength(t *testing.T) {
	c := config.Defaults()
	c.Enabled = true
	c.Models = []string{"gpt-6-astra"}
	st := store.New(nil)
	pair := &routecookie.Pair{Values: map[string]string{routecookie.CFLB: "edge-route", routecookie.OAILB: "lb-route"}, SeenAt: time.Now()}
	require.NoError(t, st.MergeRoutePair(context.Background(), pair, time.Hour))
	p := &Plugin{}
	p.state.Store(&Runtime{cfg: c, rev: c.Revision(), store: st})
	p.rememberRouteRequest("route-limited", pair.Key())
	_, err := p.ObserveOutboundResponse(context.Background(), &pluginv1.ObserveOutboundResponseRequest{
		RequestId: "route-limited", AccountId: 7, Model: "gpt-6-astra", Platform: "openai", AccountType: "oauth",
		StatusCode: 200,
		Headers:    map[string]*pluginv1.HeaderValues{turnStateHeader: {Values: []string{strings.Repeat("x", c.ReplaceLength)}}},
	})
	require.NoError(t, err)
	got, err := st.GetRoutePairs(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.False(t, got[0].BadAt.IsZero(), "replacement-length response should penalize the steered pair")
}

func TestObserveRouteDeletionDoesNotResurrectStaleIssuance(t *testing.T) {
	c := config.Defaults()
	c.Enabled = true
	c.Models = []string{"gpt-6-astra"}
	st := store.New(nil)
	pair := &routecookie.Pair{Values: map[string]string{routecookie.CFLB: "edge-delete", routecookie.OAILB: "lb-delete"}, SeenAt: time.Now()}
	require.NoError(t, st.MergeRoutePair(context.Background(), pair, time.Hour))
	p := &Plugin{}
	p.state.Store(&Runtime{cfg: c, rev: c.Revision(), store: st})
	p.rememberRouteRequest("route-delete", pair.Key())
	_, err := p.ObserveOutboundResponse(context.Background(), &pluginv1.ObserveOutboundResponseRequest{
		RequestId: "route-delete", AccountId: 7, Model: "gpt-6-astra", Platform: "openai", AccountType: "oauth",
		StatusCode: 200,
		Headers: map[string]*pluginv1.HeaderValues{"set-cookie": {Values: []string{
			routecookie.CFLB + "=; Max-Age=0",
			routecookie.OAILB + "=fresh-but-stale",
		}}},
	})
	require.NoError(t, err)
	got, err := st.GetRoutePairs(context.Background())
	require.NoError(t, err)
	require.Empty(t, got)
}

func pluginTestState(t *testing.T, length int, issuedAt time.Time) string {
	t.Helper()
	blocks := 10
	if length == 780 {
		blocks = 33
	}
	raw := make([]byte, 57+16*blocks)
	raw[0] = 0x80
	binary.BigEndian.PutUint64(raw[1:9], uint64(issuedAt.Unix()))
	for i := 9; i < len(raw); i++ {
		raw[i] = byte(i*31 + 7)
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	if length == 292 {
		encoded += "=="
	}
	require.Len(t, encoded, length)
	require.True(t, strings.HasPrefix(encoded, ticket.StatePrefix))
	return encoded
}

func TestTicketObservationLearnsRecurringNaturalLength(t *testing.T) {
	p := &Plugin{observed: map[string]semanticObservation{}}
	p.recordTicketObservation(7, "gpt-6-astra", 777, 780, 292, 312, false)
	p.recordTicketObservation(7, "gpt-6-astra", 777, 780, 292, 312, false)
	p.obsMu.Lock()
	item := p.observed["7\x00gpt-6-astra"]
	p.obsMu.Unlock()
	require.Equal(t, int64(1), item.NaturalOther)
	require.Equal(t, int64(1), item.NaturalNormal)
	require.Equal(t, "normal", item.LastTicketKind)
}

func TestTicketObservationKeepsLastSignedAcrossInjectedSilence(t *testing.T) {
	p := &Plugin{observed: map[string]semanticObservation{}}
	p.recordTicketObservation(7, "gpt-6-astra", 292, 780, 292, 312, true)
	p.recordTicketObservation(7, "gpt-6-astra", 0, 780, 292, 312, true)
	p.obsMu.Lock()
	item := p.observed["7\x00gpt-6-astra"]
	recent := append([]observationEvent(nil), p.obsRecent...)
	p.obsMu.Unlock()
	require.Equal(t, "normal", item.LastSignedKind)
	require.True(t, item.LastSignedInjected)
	require.Equal(t, int64(1), item.InjectedSilent)
	require.Len(t, item.Hourly, 1)
	require.Len(t, recent, 2)
}

func TestSemanticDowngradeAnnotatesObservationFeed(t *testing.T) {
	c := config.Defaults()
	c.Enabled = true
	c.Models = []string{"gpt-6-astra"}
	p := &Plugin{}
	p.state.Store(&Runtime{cfg: c, rev: c.Revision(), store: store.New(nil)})
	_, err := p.ObserveOutboundResponse(context.Background(), &pluginv1.ObserveOutboundResponseRequest{
		AccountId: 7, Model: "gpt-6-astra", Platform: "openai", AccountType: "oauth", SentTicketState: "injected",
		Headers: map[string]*pluginv1.HeaderValues{observedServedModelHeader: {Values: []string{"gpt-5.6-sol"}}},
	})
	require.NoError(t, err)
	p.obsMu.Lock()
	item := p.observed["7\x00gpt-6-astra"]
	recent := append([]observationEvent(nil), p.obsRecent...)
	p.obsMu.Unlock()
	require.Equal(t, int64(1), item.InjectedLimited)
	require.Len(t, recent, 1)
	require.Equal(t, "gpt-5.6-sol", recent[0].Served)
}
