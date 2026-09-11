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
