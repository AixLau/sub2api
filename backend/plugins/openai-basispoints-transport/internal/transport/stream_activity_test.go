package transport

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/stretchr/testify/require"
)

func TestStreamActivityTracksReadsWithoutInventingProgress(t *testing.T) {
	now := time.Unix(100, 0)
	var signals []string
	a := &streamActivity{now: func() time.Time { return now }, emit: func(p []byte) error {
		signals = append(signals, string(p))
		return nil
	}}
	body := a.wrap(io.NopCloser(strings.NewReader("abcdef")))
	buf := make([]byte, 2)
	n, err := body.Read(buf)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.Equal(t, "ab", string(buf))
	require.Equal(t, []string{upstreamActivityComment}, signals)
	now = now.Add(time.Second)
	_, err = body.Read(buf)
	require.NoError(t, err)
	require.Len(t, signals, 1, "throttle consecutive reads")
	now = now.Add(upstreamActivityInterval)
	_, err = body.Read(buf)
	require.NoError(t, err)
	require.Len(t, signals, 2)
	now = now.Add(time.Hour)
	n, err = body.Read(buf)
	require.ErrorIs(t, err, io.EOF)
	require.Zero(t, n)
	require.Len(t, signals, 2, "elapsed time without bytes is not activity")
	n, err = a.wrap(io.NopCloser(strings.NewReader("feedback"))).Read(buf)
	require.NoError(t, err)
	require.Positive(t, n)
	require.Len(t, signals, 3, "feedback reads use the same activity clock")
}

func TestStreamActivityPropagatesDownstreamFailure(t *testing.T) {
	sendErr := errors.New("downstream closed")
	a := &streamActivity{now: time.Now, emit: func([]byte) error { return sendErr }}
	n, err := a.wrap(io.NopCloser(strings.NewReader("upstream"))).Read(make([]byte, 32))
	require.Zero(t, n)
	require.ErrorIs(t, err, sendErr)
}

// The upstream withholds its terminal event until the host receives activity.
// This would deadlock until the deadline if buffering hid all incoming bytes.
func TestForwardReportsActivityBeforeBufferedToolCompletes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	payload, err := json.Marshal(map[string]string{"city": "private-city"})
	require.NoError(t, err)
	outer, err := json.Marshal(map[string]any{"references": []string{"client-tool:get_weather"}, "code": string(payload)})
	require.NoError(t, err)
	item := map[string]any{"type": "function_call", "name": "run_officejs", "id": "fc_activity", "call_id": "call_activity", "arguments": string(outer)}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, wireEvent("response.output_item.added", map[string]any{"output_index": 0, "item": item}))
		io.WriteString(w, wireEvent("response.function_call_arguments.delta", map[string]any{"output_index": 0, "delta": string(outer)}))
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-req.Context().Done():
			return
		}
		io.WriteString(w, wireEvent("response.output_item.done", map[string]any{"output_index": 0, "item": item}))
		io.WriteString(w, wireEvent("response.completed", map[string]any{"response": map[string]any{"id": "resp_activity", "status": "completed", "output": []any{item}}}))
	}))
	defer upstream.Close()
	c := recoveryClient(t, ctx, upstream.URL)
	ctx, err = pluginv1.WithRequestBodyLimit(ctx, 256<<20)
	require.NoError(t, err)
	stream, err := c.Forward(ctx)
	require.NoError(t, err)
	body, err := json.Marshal(map[string]any{"model": "gpt-6-sol", "stream": true, "input": "weather", "tools": []any{map[string]any{"type": "function", "name": "get_weather"}}})
	require.NoError(t, err)
	start := &pluginv1.ForwardRequestStart{AccountId: 7, Platform: "openai", AccountType: "oauth", Method: "POST", Url: "https://chatgpt.com/backend-api/codex/responses", HasBody: true, ContentLength: int64(len(body)), Headers: map[string]*pluginv1.HeaderValues{"authorization": {Values: []string{"Bearer synthetic"}}, "chatgpt-account-id": {Values: []string{"synthetic-account"}}}}
	require.NoError(t, stream.Send(&pluginv1.ForwardRequest{Frame: &pluginv1.ForwardRequest_Start{Start: start}}))
	require.NoError(t, stream.Send(&pluginv1.ForwardRequest{Frame: &pluginv1.ForwardRequest_BodyChunk{BodyChunk: body}}))
	require.NoError(t, stream.Send(&pluginv1.ForwardRequest{Frame: &pluginv1.ForwardRequest_BodyEnd{BodyEnd: true}}))
	require.NoError(t, stream.CloseSend())
	var output strings.Builder
	activitySeen := false
	for {
		frame, err := stream.Recv()
		require.NoError(t, err)
		require.Nil(t, frame.GetError())
		if chunk := frame.GetBodyChunk(); len(chunk) > 0 {
			if !activitySeen {
				require.Equal(t, upstreamActivityComment, string(chunk))
				require.NotContains(t, string(chunk), "private-city")
				activitySeen = true
				unblock()
			}
			output.Write(chunk)
		}
		if end := frame.GetEnd(); end != nil {
			require.Equal(t, int64(output.Len()), end.BytesReceived)
			break
		}
	}
	require.True(t, activitySeen)
	require.Contains(t, output.String(), "response.completed")
	require.Contains(t, output.String(), "get_weather")
	require.Contains(t, output.String(), "private-city")
	require.NotContains(t, output.String(), "run_officejs")
}
