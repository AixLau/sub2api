package store

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/plugins/openai-codex-ticket/internal/routecookie"
	"github.com/stretchr/testify/require"
)

func TestRoutePairPersistsAcrossStoreInstances(t *testing.T) {
	kv := NewMemoryKV()
	first := New(kv)
	pair := &routecookie.Pair{Values: map[string]string{
		routecookie.CFLB:  "edge",
		routecookie.OAILB: "lb",
	}, SeenAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
	require.NoError(t, first.PutRoutePair(context.Background(), pair, time.Hour))

	second := New(kv)
	got, err := second.GetRoutePair(context.Background())
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, pair.Header(), got.Header())

	got.Values[routecookie.CFLB] = "mutated"
	again, err := second.GetRoutePair(context.Background())
	require.NoError(t, err)
	require.Equal(t, "edge", again.Values[routecookie.CFLB])
}

func TestRoutePoolKeepsMultipleGatewaysAndSelectsHealthyPair(t *testing.T) {
	kv := NewMemoryKV()
	s := New(kv)
	now := time.Now().UTC()
	first := &routecookie.Pair{Values: map[string]string{
		routecookie.CFLB: "edge-a", routecookie.OAILB: "unified-88-a",
	}, SeenAt: now, ExpiresAt: now.Add(time.Hour), Gateway: "unified-88"}
	second := &routecookie.Pair{Values: map[string]string{
		routecookie.CFLB: "edge-b", routecookie.OAILB: "unified-97-b",
	}, SeenAt: now.Add(time.Second), ExpiresAt: now.Add(time.Hour), Gateway: "unified-97"}
	require.NoError(t, s.MergeRoutePair(context.Background(), first, time.Hour))
	require.NoError(t, s.MergeRoutePair(context.Background(), second, time.Hour))
	pairs, err := s.GetRoutePairs(context.Background())
	require.NoError(t, err)
	require.Len(t, pairs, 2)

	selected, err := s.SelectRoutePair(context.Background(), now.Add(2*time.Second), time.Hour)
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, second.Header(), selected.Header())
	require.NoError(t, s.MarkRoutePair(context.Background(), selected.Key(), false, time.Hour))
	selected, err = s.SelectRoutePair(context.Background(), now.Add(2*time.Second), time.Hour)
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, first.Header(), selected.Header())
}

func TestDeleteRoutePairLeavesOtherGateways(t *testing.T) {
	s := New(NewMemoryKV())
	first := &routecookie.Pair{Values: map[string]string{routecookie.CFLB: "cf-a", routecookie.OAILB: "lb-a"}, SeenAt: time.Now()}
	second := &routecookie.Pair{Values: map[string]string{routecookie.CFLB: "cf-b", routecookie.OAILB: "lb-b"}, SeenAt: time.Now()}
	require.NoError(t, s.MergeRoutePair(context.Background(), first, time.Hour))
	require.NoError(t, s.MergeRoutePair(context.Background(), second, time.Hour))
	require.NoError(t, s.DeleteRoutePair(context.Background(), first.Key(), time.Hour))
	pairs, err := s.GetRoutePairs(context.Background())
	require.NoError(t, err)
	require.Len(t, pairs, 1)
	require.Equal(t, second.Key(), pairs[0].Key())
}

func TestRoutePoolPersistsPartialPair(t *testing.T) {
	s := New(NewMemoryKV())
	pair := &routecookie.Pair{Values: map[string]string{routecookie.CFLB: "cf-only"}, SeenAt: time.Now()}
	require.NoError(t, s.MergeRoutePair(context.Background(), pair, time.Hour))
	selected, err := s.SelectRoutePair(context.Background(), time.Now(), time.Hour)
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, "__cflb=cf-only", selected.Header())
}
