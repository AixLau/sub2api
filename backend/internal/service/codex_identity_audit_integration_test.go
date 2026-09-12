package service

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCodexIdentityAudit_WebSocketPassthroughPreservesTurnMetadataPerFrame(t *testing.T) {
	controlCtx, cancelControl := context.WithCancelCause(context.Background())
	defer cancelControl(context.Canceled)

	upstream := newStagedPassthroughConn()
	upstream.Send(`{"type":"response.completed","response":{"id":"resp_a","model":"gpt-5.1"}}`)
	upstream.Send(`{"type":"response.completed","response":{"id":"resp_b","model":"gpt-5.1"}}`)
	server, serverErr := startPassthroughLifecycleServer(
		t,
		controlCtx,
		newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream),
		passthroughLifecycleAccount(),
	)
	defer server.Close()

	dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
	clientConn, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(server.URL, "http"), &coderws.DialOptions{
		HTTPHeader: http.Header{
			"session-id":            []string{"session-1"},
			"thread-id":             []string{"thread-1"},
			"x-codex-turn-metadata": []string{`{"session_id":"session-1","thread_id":"thread-1","turn_id":"turn-1","turn_started_at_unix_ms":1000}`},
		},
	})
	cancelDial()
	require.NoError(t, err)
	defer func() { _ = clientConn.CloseNow() }()

	first := `{"type":"response.create","model":"gpt-5.1","stream":false,"client_metadata":{"session_id":"session-1","thread_id":"thread-1","x-codex-turn-metadata":"{\"session_id\":\"session-1\",\"thread_id\":\"thread-1\",\"turn_id\":\"turn-1\",\"turn_started_at_unix_ms\":1000}"}}`
	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	require.NoError(t, clientConn.Write(writeCtx, coderws.MessageText, []byte(first)))
	cancelWrite()
	firstUpstream := requirePassthroughUpstreamWrite(t, upstream, 3*time.Second)
	firstTurnMetadata := gjson.Parse(gjson.GetBytes(firstUpstream, "client_metadata.x-codex-turn-metadata").String())
	require.Equal(t, "turn-1", firstTurnMetadata.Get("turn_id").String())
	require.Equal(t, float64(1000), firstTurnMetadata.Get("turn_started_at_unix_ms").Float())
	_, err = readPassthroughLifecycleFrame(t, clientConn, 3*time.Second)
	require.NoError(t, err)

	second := `{"type":"response.create","model":"gpt-5.1","stream":false,"client_metadata":{"session_id":"session-1","thread_id":"thread-1","x-codex-turn-metadata":"{\"session_id\":\"session-1\",\"thread_id\":\"thread-1\",\"turn_id\":\"turn-2\",\"turn_started_at_unix_ms\":2000}"}}`
	writeCtx, cancelWrite = context.WithTimeout(context.Background(), 3*time.Second)
	require.NoError(t, clientConn.Write(writeCtx, coderws.MessageText, []byte(second)))
	cancelWrite()
	secondUpstream := requirePassthroughUpstreamWrite(t, upstream, 3*time.Second)
	secondTurnMetadata := gjson.Parse(gjson.GetBytes(secondUpstream, "client_metadata.x-codex-turn-metadata").String())
	require.Equal(t, "turn-2", secondTurnMetadata.Get("turn_id").String())
	require.Equal(t, float64(2000), secondTurnMetadata.Get("turn_started_at_unix_ms").Float())
	_, err = readPassthroughLifecycleFrame(t, clientConn, 3*time.Second)
	require.NoError(t, err)
	require.NoError(t, clientConn.Close(coderws.StatusNormalClosure, "done"))

	select {
	case serverErrValue := <-serverErr:
		require.NoError(t, serverErrValue)
	case <-time.After(3 * time.Second):
		t.Fatal("two-frame identity audit did not terminate")
	}
}

func TestCodexSessionWebSocketPassthroughRefreshesCacheBindingPerFrame(t *testing.T) {
	for _, mode := range []string{"v2", "legacy"} {
		t.Run(mode, func(t *testing.T) {
			controlCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
			upstream := newStagedPassthroughConn()
			cfg := passthroughLifecycleConfig()
			cfg.Gateway.OpenAIWS.OAuthEnabled = true
			cfg.Gateway.CodexSessionIdentityMapping = mode
			svc := newPassthroughLifecycleService(cfg, upstream)
			svc.cache = &codexSessionIdentityTestStore{GatewayCache: &stubGatewayCache{}, values: map[string]string{}}
			account := codexSessionIdentityV2Account("ws-cache-binding")
			account.Concurrency = 1
			account.Credentials["access_token"] = "token"
			account.Extra = map[string]any{"openai_oauth_responses_websockets_v2_mode": OpenAIWSIngressModePassthrough}
			server, serverErr := startPassthroughLifecycleServer(t, controlCtx, svc, account)
			defer server.Close()
			raw := newCodexUUIDv7ForTest(t)
			dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
			client, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(server.URL, "http"), &coderws.DialOptions{HTTPHeader: http.Header{"Session-Id": {raw}}})
			cancelDial()
			require.NoError(t, err)
			defer client.CloseNow()
			var session string
			for i, key := range []string{raw, "independent-cache", raw} {
				// Only the handshake carries the session. Every response.create
				// must capture whether this frame's key references that session.
				payload := []byte(`{"type":"response.create","model":"gpt-5.2","instructions":"test","prompt_cache_key":"` + key + `","input":[]}`)
				writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
				require.NoError(t, client.Write(writeCtx, coderws.MessageText, payload))
				cancelWrite()
				var output []byte
				select {
				case output = <-upstream.writes:
				case err := <-serverErr:
					t.Fatalf("websocket forwarding ended before frame %d: %v", i, err)
				case <-time.After(3 * time.Second):
					t.Fatalf("frame %d was not forwarded", i)
				}
				mapped := gjson.GetBytes(output, "client_metadata.session_id").String()
				if i == 0 {
					session = mapped
				}
				require.NotEmpty(t, mapped)
				require.NotEqual(t, raw, mapped)
				require.Equal(t, session, mapped)
				wantKey := mapped
				if key != raw {
					wantKey = scopeCodexAccountIdentityValue(account, 0, "prompt-cache", key)
				}
				require.Equal(t, wantKey, gjson.GetBytes(output, "prompt_cache_key").String())
				upstream.Send(`{"type":"response.completed","response":{"id":"resp_cache_turn","model":"gpt-5.2"}}`)
				_, err = readPassthroughLifecycleFrame(t, client, 3*time.Second)
				require.NoError(t, err)
			}
			require.NoError(t, client.Close(coderws.StatusNormalClosure, "done"))
			select {
			case err := <-serverErr:
				require.NoError(t, err)
			case <-time.After(3 * time.Second):
				t.Fatal("session cache binding test did not terminate")
			}
		})
	}
}
