package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	pluginconfig "github.com/Wei-Shaw/sub2api/plugins/openai-bps-local/internal/config"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type fakeStream struct {
	grpc.BidiStreamingServer[pluginv1.ForwardRequest, pluginv1.ForwardResponse]
	ctx    context.Context
	in     []*pluginv1.ForwardRequest
	mu     sync.Mutex
	frames []*pluginv1.ForwardResponse
}

func (s *fakeStream) Context() context.Context { return s.ctx }

func (s *fakeStream) Recv() (*pluginv1.ForwardRequest, error) {
	if len(s.in) == 0 {
		return nil, io.EOF
	}
	frame := s.in[0]
	s.in = s.in[1:]
	return frame, nil
}

func (s *fakeStream) Send(frame *pluginv1.ForwardResponse) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.frames = append(s.frames, frame)
	return nil
}

func (s *fakeStream) result(t *testing.T) (*pluginv1.ForwardResponseStart, string, *pluginv1.ForwardResponseError) {
	t.Helper()
	var start *pluginv1.ForwardResponseStart
	var body bytes.Buffer
	for _, frame := range s.frames {
		if e := frame.GetError(); e != nil {
			return nil, "", e
		}
		if st := frame.GetStart(); st != nil {
			start = st
		}
		_, _ = body.Write(frame.GetBodyChunk())
	}
	require.NotNil(t, s.frames[len(s.frames)-1].GetEnd(), "stream must end with an end frame")
	return start, body.String(), nil
}

func newStream(t *testing.T, start *pluginv1.ForwardRequestStart, body string, session string) *fakeStream {
	t.Helper()
	ctx, err := pluginv1.WithRequestBodyLimit(context.Background(), 1<<20)
	require.NoError(t, err)
	// The host sends WithRequestBodyLimit as outgoing metadata; mirror it as incoming.
	md, _ := metadata.FromOutgoingContext(ctx)
	md = md.Copy()
	if session != "" {
		md.Set(pluginv1.ClientSessionIDMetadataKey, session)
	}
	ctx = metadata.NewIncomingContext(context.Background(), md)
	start.ContentLength = int64(len(body))
	start.HasBody = body != ""
	return &fakeStream{ctx: ctx, in: []*pluginv1.ForwardRequest{
		{Frame: &pluginv1.ForwardRequest_Start{Start: start}},
		{Frame: &pluginv1.ForwardRequest_BodyChunk{BodyChunk: []byte(body)}},
		{Frame: &pluginv1.ForwardRequest_BodyEnd{BodyEnd: true}},
	}}
}

func hostStart(url string) *pluginv1.ForwardRequestStart {
	return &pluginv1.ForwardRequestStart{
		RequestId: "req-1", Method: http.MethodPost, Url: url,
		AccountId: 42, Platform: "openai", AccountType: "oauth",
		Headers: headersToProto(http.Header{
			"Authorization":      {"Bearer host-token"},
			"Chatgpt-Account-Id": {"acct-host"},
			"Content-Type":       {"application/json"},
			"Accept":             {"text/event-stream"},
		}),
	}
}

// testPlugin points the bridge at a loopback SSE upstream. bps-local allows
// unencrypted upstreams only on loopback, so this bypasses config.Parse.
func testPlugin(t *testing.T, upstream string, mutate func(*pluginconfig.Config)) *Plugin {
	t.Helper()
	cfg := pluginconfig.Default()
	cfg.UpstreamURL = upstream
	cfg.UpstreamTransport = pluginconfig.TransportSSE
	cfg.ProxyMode = pluginconfig.ProxyDisabled
	if mutate != nil {
		mutate(&cfg)
	}
	state, err := buildState(cfg)
	require.NoError(t, err)
	p := New()
	p.state.Store(state)
	return p
}

const basicBody = `{"model":"gpt-6-astra","stream":true,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`

func TestForwardBridgesResponsesWithHostIdentity(t *testing.T) {
	var got http.Header
	var gotBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"output\":[],\"usage\":{\"input_tokens\":3,\"output_tokens\":1}}}\n\n")
	}))
	defer upstream.Close()

	p := testPlugin(t, upstream.URL, nil)
	stream := newStream(t, hostStart("https://chatgpt.com/backend-api/codex/responses"), basicBody, "sess-1")
	require.NoError(t, p.Forward(stream))

	start, body, frameErr := stream.result(t)
	require.Nil(t, frameErr)
	require.EqualValues(t, http.StatusOK, start.StatusCode)
	require.Contains(t, body, "response.completed")
	require.Contains(t, body, `"input_tokens":3`)

	require.Equal(t, "Bearer host-token", got.Get("Authorization"))
	require.Equal(t, "acct-host", got.Get("Chatgpt-Account-Id"))
	require.Equal(t, "basispoints-excel-plugin", got.Get("X-Openai-Internal-Basispoints-Client-Product"))
	require.Contains(t, string(gotBody), `"model_selection":"explicit"`)
	require.EqualValues(t, 1, p.stats.bridged.Load())
	require.EqualValues(t, 1, p.stats.succeeded.Load())
}

func TestForwardPropagatesUpstreamStatus(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer upstream.Close()

	p := testPlugin(t, upstream.URL, nil)
	stream := newStream(t, hostStart("https://chatgpt.com/backend-api/codex/responses"), basicBody, "")
	require.NoError(t, p.Forward(stream))
	start, body, frameErr := stream.result(t)
	require.Nil(t, frameErr)
	require.EqualValues(t, http.StatusTooManyRequests, start.StatusCode)
	require.Equal(t, []string{"7"}, start.Headers["Retry-After"].GetValues())
	require.Contains(t, body, "basispoints_upstream_error")
	require.EqualValues(t, 1, p.stats.failed.Load())
}

func TestForwardRejectsMissingBearerWithoutCallingUpstream(t *testing.T) {
	called := false
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer upstream.Close()

	p := testPlugin(t, upstream.URL, nil)
	start := hostStart("https://chatgpt.com/backend-api/codex/responses")
	delete(start.Headers, "Authorization")
	stream := newStream(t, start, basicBody, "")
	require.NoError(t, p.Forward(stream))
	require.Equal(t, "PLUGIN_AUTH_MISSING", stream.frames[0].GetError().GetCode())
	require.False(t, called)
}

func TestForwardPassesNonResponsesEndpointsThroughNatively(t *testing.T) {
	var nativePath, nativeAuth string
	native := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nativePath, nativeAuth = r.URL.Path, r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"output":[]}`)
	}))
	defer native.Close()

	p := testPlugin(t, "http://127.0.0.1:1/never", nil)
	stream := newStream(t, hostStart(native.URL+"/backend-api/codex/responses/compact"), `{"model":"gpt-6-astra","input":[]}`, "")
	require.NoError(t, p.Forward(stream))
	start, body, frameErr := stream.result(t)
	require.Nil(t, frameErr)
	require.EqualValues(t, http.StatusOK, start.StatusCode)
	require.Equal(t, `{"output":[]}`, body)
	require.Equal(t, "/backend-api/codex/responses/compact", nativePath)
	require.Equal(t, "Bearer host-token", nativeAuth)
	require.EqualValues(t, 1, p.stats.native.Load())
}

func TestForwardRejectsNonResponsesWhenPassthroughDisabled(t *testing.T) {
	p := testPlugin(t, "http://127.0.0.1:1/never", func(cfg *pluginconfig.Config) {
		off := false
		cfg.NativePassthrough = &off
	})
	stream := newStream(t, hostStart("https://chatgpt.com/backend-api/codex/responses/compact"), `{}`, "")
	require.NoError(t, p.Forward(stream))
	require.Equal(t, "PLUGIN_UNSUPPORTED_ENDPOINT", stream.frames[0].GetError().GetCode())
}

func TestReplayScopeIsolatesAccountsAndSessions(t *testing.T) {
	md := metadata.Pairs(pluginv1.ClientSessionIDMetadataKey, "sess-a")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	a := replayScope(ctx, &pluginv1.ForwardRequestStart{AccountId: 1}, http.Header{})
	b := replayScope(ctx, &pluginv1.ForwardRequestStart{AccountId: 2}, http.Header{})
	c := replayScope(context.Background(), &pluginv1.ForwardRequestStart{AccountId: 1}, http.Header{"Session_id": {"sess-b"}})
	require.NotEqual(t, a, b)
	require.NotEqual(t, a, c)
	require.True(t, strings.HasPrefix(a, "acct:1|"))
}

func TestConfigRoundTrip(t *testing.T) {
	p := New()
	resp, err := p.ValidateConfig(context.Background(), &pluginv1.ValidateConfigRequest{ConfigJson: []byte(`{"upstream_transport":"SSE","extra_headers":{"x-custom":"1"}}`)})
	require.NoError(t, err)
	require.True(t, resp.Valid, resp.Message)
	var normalized map[string]any
	require.NoError(t, json.Unmarshal(resp.NormalizedConfigJson, &normalized))
	require.Equal(t, "sse", normalized["upstream_transport"])
	require.Equal(t, map[string]any{"X-Custom": "1"}, normalized["extra_headers"])

	for _, bad := range []string{
		`{"upstream_url":"http://example.com/x"}`,
		`{"upstream_transport":"grpc"}`,
		`{"extra_headers":{"Authorization":"x"}}`,
		`{"extra_headers":{"X-OpenAI-Account-Id":"x"}}`,
		`{"unknown_field":1}`,
	} {
		resp, err := p.ValidateConfig(context.Background(), &pluginv1.ValidateConfigRequest{ConfigJson: []byte(bad)})
		require.NoError(t, err)
		require.False(t, resp.Valid, bad)
	}

	applied, err := p.ApplyConfig(context.Background(), &pluginv1.ApplyConfigRequest{ConfigJson: []byte(`{"upstream_transport":"ws","ws_idle_timeout_seconds":30}`)})
	require.NoError(t, err)
	require.True(t, applied.Applied, applied.Message)
	require.Equal(t, "ws", p.state.Load().cfg.UpstreamTransport)
}
