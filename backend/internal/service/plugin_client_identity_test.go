package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type clientIdentitySnapshot struct {
	start   *pluginv1.ForwardRequestStart
	headers http.Header
}

type clientIdentityProbe struct {
	pluginv1.UnimplementedTransportPluginServer
	seen chan clientIdentitySnapshot
}

func (p *clientIdentityProbe) Forward(stream pluginv1.TransportPlugin_ForwardServer) error {
	md, _ := metadata.FromIncomingContext(stream.Context())
	headers, err := pluginv1.AccountClientHeaders(md)
	if err != nil {
		return err
	}
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	p.seen <- clientIdentitySnapshot{start: first.GetStart(), headers: headers}
	for {
		frame, err := stream.Recv()
		if err != nil {
			return err
		}
		if frame.GetBodyEnd() {
			break
		}
	}
	if err := stream.Send(&pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_Start{Start: &pluginv1.ForwardResponseStart{StatusCode: 200, Status: "200 OK", ContentLength: 0}}}); err != nil {
		return err
	}
	return stream.Send(&pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_End{End: &pluginv1.ForwardResponseEnd{BytesReceived: 0}}})
}

func TestPluginRuntimeAccountClientHeadersUseSelectedAccount(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	probe := &clientIdentityProbe{seen: make(chan clientIdentitySnapshot, 3)}
	client := dispenseTransportClient(t, probe)
	runtime := &pluginRuntime{api: client.TransportPluginClient, done: make(chan struct{})}
	// Incoming context metadata cannot replace the host's selected account.
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(pluginv1.AccountClientHeadersMetadataKey, "forged-snapshot"))
	for i, accountID := range []int64{7, 9, 7} {
		ua := fmt.Sprintf("captured-browser/%d", i)
		account := &Account{ID: accountID, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
			"access_token": "not-forwarded-raw", "refresh_token": "never-forwarded",
			"captured_headers": map[string]any{"User-Agent": ua, "Authorization": "stale-token", "Cookie": "cookie"},
		}}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", http.NoBody)
		require.NoError(t, err)
		req.Header.Set("Authorization", fmt.Sprintf("Bearer authorized-%d", i))
		req.Header.Set("User-Agent", "native-codex")
		require.True(t, runtime.beginRequest())
		resp, err := runtime.roundTrip(ctx, req, "", account)
		require.NoError(t, err)
		_, err = io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		snapshot := <-probe.seen
		require.Equal(t, accountID, snapshot.start.AccountId)
		require.Equal(t, http.Header{"User-Agent": {ua}}, snapshot.headers)
		require.Equal(t, req.Header.Get("Authorization"), snapshot.start.Headers["Authorization"].Values[0])
		require.Equal(t, "native-codex", snapshot.start.Headers["User-Agent"].Values[0])
		require.NotContains(t, snapshot.start.Headers, pluginv1.AccountClientHeadersMetadataKey)
	}
}

type identityHeaderStream struct{ headers metadata.MD }

func (s *identityHeaderStream) Method() string { return "ResolveOutboundIdentity" }
func (s *identityHeaderStream) SetHeader(md metadata.MD) error {
	s.headers = metadata.Join(s.headers, md)
	return nil
}
func (s *identityHeaderStream) SendHeader(md metadata.MD) error { return s.SetHeader(md) }
func (s *identityHeaderStream) SetTrailer(metadata.MD) error    { return nil }

func TestPluginHostServiceAccountClientHeadersArePrivate(t *testing.T) {
	directory := &fakeAccountDirectory{identity: &PluginOutboundIdentity{
		AccountID: 7, Token: "current-token", Headers: http.Header{"User-Agent": {"native-codex"}},
		ClientHeaders: http.Header{"User-Agent": {"captured-browser"}},
	}}
	scope := newPluginAccountScope(pluginAccountScopeEntry{Platform: PlatformOpenAI, AccountType: AccountTypeOAuth}).WithAccountIDs([]int64{7})
	server := newPluginHostServiceServer("test.identity", newFakePluginKVStore(), directory, scope)
	stream := &identityHeaderStream{}
	ctx := grpc.NewContextWithServerTransportStream(context.Background(), stream)
	identity, err := server.ResolveOutboundIdentity(ctx, &pluginv1.ResolveOutboundIdentityRequest{AccountId: 7})
	require.NoError(t, err)
	require.Equal(t, "current-token", identity.Token)
	require.Equal(t, "native-codex", identity.Headers["User-Agent"].Values[0])
	headers, err := pluginv1.AccountClientHeaders(stream.headers)
	require.NoError(t, err)
	require.Equal(t, directory.identity.ClientHeaders, headers)
	stream.headers = nil
	identity, err = server.ResolveOutboundIdentity(ctx, &pluginv1.ResolveOutboundIdentityRequest{AccountId: 9})
	require.NoError(t, err)
	require.False(t, identity.Found)
	require.Empty(t, stream.headers)
}
