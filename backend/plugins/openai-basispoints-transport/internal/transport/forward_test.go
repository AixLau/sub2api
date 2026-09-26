package transport

import (
	"archive/zip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	hclog "github.com/hashicorp/go-hclog"
	hcplugin "github.com/hashicorp/go-plugin"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type testHostKV struct {
	pluginv1.UnimplementedHostServiceServer
	mu     sync.Mutex
	values map[string][]byte
}

func (s *testHostKV) KVGet(_ context.Context, r *pluginv1.KVGetRequest) (*pluginv1.KVGetResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.values[r.Namespace+"/"+r.Key]
	return &pluginv1.KVGetResponse{Found: ok, Value: v}, nil
}
func (s *testHostKV) KVSet(_ context.Context, r *pluginv1.KVSetRequest) (*pluginv1.KVSetResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[r.Namespace+"/"+r.Key] = append([]byte(nil), r.Value...)
	return &pluginv1.KVSetResponse{}, nil
}

// Test through the public gRPC contract, optionally using a packaged executable.
// The only HTTP endpoint contacted is httptest; all credentials are synthetic.
type testClientOptions struct {
	plugin *Plugin
	logger hclog.Logger
}

func clientForTest(t *testing.T, store *testHostKV, binary string, options ...testClientOptions) *pluginv1.TransportClient {
	t.Helper()
	opts := testClientOptions{logger: hclog.NewNullLogger()}
	if len(options) > 0 {
		opts = options[0]
	}
	var rpc hcplugin.ClientProtocol
	if binary == "" {
		if opts.plugin == nil {
			opts.plugin = New()
			opts.plugin.diagnosticLogger = hclog.NewNullLogger()
		}
		client, _ := hcplugin.TestPluginGRPCConn(t, false, map[string]hcplugin.Plugin{pluginv1.TransportPluginName: &pluginv1.GRPCPlugin{Impl: opts.plugin}})
		rpc = client
		t.Cleanup(func() { _ = client.Close() })
	} else {
		client := hcplugin.NewClient(&hcplugin.ClientConfig{HandshakeConfig: pluginv1.HandshakeConfig, Plugins: pluginv1.ClientPluginMap(), Cmd: exec.Command(binary), AllowedProtocols: []hcplugin.Protocol{hcplugin.ProtocolGRPC}, StartTimeout: 10 * time.Second, Logger: opts.logger, SyncStdout: io.Discard, SyncStderr: io.Discard, UnixSocketConfig: &hcplugin.UnixSocketConfig{TempDir: os.TempDir()}})
		var err error
		rpc, err = client.Client()
		require.NoError(t, err)
		t.Cleanup(client.Kill)
	}
	dispensed, err := rpc.Dispense(pluginv1.TransportPluginName)
	require.NoError(t, err)
	c := dispensed.(*pluginv1.TransportClient)
	info, err := c.GetInfo(context.Background(), &pluginv1.GetInfoRequest{})
	require.NoError(t, err)
	require.Equal(t, PluginVersion, info.PluginVersion)
	id := c.Broker.NextId()
	go c.Broker.AcceptAndServe(id, func(opts []grpc.ServerOption) *grpc.Server {
		server := grpc.NewServer(opts...)
		pluginv1.RegisterHostServiceServer(server, store)
		return server
	})
	init, err := c.InitHostServices(context.Background(), &pluginv1.InitHostServicesRequest{HostServiceId: id, HostServiceApiVersion: pluginv1.HostServiceAPIVersion})
	require.NoError(t, err)
	require.True(t, init.Ready)
	return c
}

func forwardForTest(t *testing.T, c *pluginv1.TransportClient, body []byte, ctx context.Context, starts ...*pluginv1.ForwardRequestStart) (*pluginv1.ForwardResponseStart, []byte, *pluginv1.ForwardResponseError) {
	t.Helper()
	stream, err := c.Forward(ctx)
	require.NoError(t, err)
	start := &pluginv1.ForwardRequestStart{AccountId: 7, Platform: "openai", AccountType: "oauth", Method: "POST", Url: "https://chatgpt.com/backend-api/codex/responses", Host: "chatgpt.com", HasBody: true, ContentLength: int64(len(body)), Headers: map[string]*pluginv1.HeaderValues{"authorization": {Values: []string{"Bearer synthetic"}}, "chatgpt-account-id": {Values: []string{"synthetic-account"}}, "session_id": {Values: []string{"isolated-session"}}}}
	start.RequestId = "req_diagnostic_test"
	if len(starts) > 0 {
		start = starts[0]
	}
	require.NoError(t, stream.Send(&pluginv1.ForwardRequest{Frame: &pluginv1.ForwardRequest_Start{Start: start}}))
	for len(body) > 0 {
		n := min(17, len(body))
		require.NoError(t, stream.Send(&pluginv1.ForwardRequest{Frame: &pluginv1.ForwardRequest_BodyChunk{BodyChunk: body[:n]}}))
		body = body[n:]
	}
	require.NoError(t, stream.Send(&pluginv1.ForwardRequest{Frame: &pluginv1.ForwardRequest_BodyEnd{BodyEnd: true}}))
	require.NoError(t, stream.CloseSend())
	var responseStart *pluginv1.ForwardResponseStart
	var output []byte
	for {
		frame, err := stream.Recv()
		require.NoError(t, err)
		if start := frame.GetStart(); start != nil {
			responseStart = start
		}
		if failure := frame.GetError(); failure != nil {
			return responseStart, output, failure
		}
		output = append(output, frame.GetBodyChunk()...)
		if end := frame.GetEnd(); end != nil {
			require.Equal(t, int64(len(output)), end.BytesReceived)
			return responseStart, output, nil
		}
	}
}

func testForwardReplay(t *testing.T, binary string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	store := &testHostKV{values: map[string][]byte{}}
	// Exercise the fully qualified name plus namespace through both the RPC
	// client and packaged process, including exact original-item replay.
	native := json.RawMessage(`{"type":"function_call","id":"fc_server","call_id":"call_server","name":"functions.run_officejs","namespace":"functions","arguments":{"summary":"Weather","code":"{\"name\":\"get_weather\",\"arguments\":{\"city\":\"Tokyo\"}}","destructive":false,"references":["get_weather"]},"status":"completed"}`)
	requests := make(chan map[string]json.RawMessage, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		raw, err := io.ReadAll(req.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		require.Equal(t, int64(len(raw)), req.ContentLength)
		require.Equal(t, "/responses", req.URL.Path)
		require.Equal(t, "Bearer synthetic", req.Header.Get("Authorization"))
		require.Equal(t, "synthetic-account", req.Header.Get("X-OpenAI-Account-ID"))
		require.Equal(t, "chatgpt", req.Header.Get("X-Basispoints-Auth-Mode"))
		var body map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(raw, &body))
		requests <- body
		for _, field := range []string{"tools", "tool_choice", "parallel_tool_calls"} {
			require.NotContains(t, body, field)
		}
		require.Equal(t, `"explicit"`, string(body["model_selection"]))
		require.Equal(t, `false`, string(body["store"]))
		var meta map[string]string
		require.NoError(t, json.Unmarshal(body["metadata"], &meta))
		w.Header().Set("Content-Type", "text/event-stream")
		if meta["agent_iteration"] == "0" {
			w.Write([]byte("event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":" + string(native) + "}\n\n"))
			w.(http.Flusher).Flush()
			w.Write([]byte("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_server\",\"status\":\"completed\",\"output\":[" + string(native) + "],\"usage\":{\"total_tokens\":10}}}\n\n"))
		} else {
			w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"18°C\"}\n\n"))
			w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_next\",\"status\":\"completed\",\"output\":[],\"usage\":{\"total_tokens\":12}}}\n\n"))
		}
	}))
	defer upstream.Close()
	apply := func(c *pluginv1.TransportClient) {
		raw, _ := json.Marshal(map[string]any{"upstream_base_url": upstream.URL, "proxy_mode": "disabled", "tools_via_native": false})
		result, err := c.ApplyConfig(ctx, &pluginv1.ApplyConfigRequest{ConfigJson: raw})
		require.NoError(t, err)
		require.True(t, result.Applied)
	}
	c1 := clientForTest(t, store, binary)
	apply(c1)
	tools := json.RawMessage(`[{"type":"function","name":"get_weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}]`)
	body, _ := json.Marshal(map[string]any{"model": "gpt-6-astra", "input": "Weather?", "tools": tools, "stream": true})
	start, output, failure := forwardForTest(t, c1, body, ctx)
	require.Nil(t, failure)
	require.Equal(t, int32(200), start.StatusCode)
	require.NotContains(t, string(output), "run_officejs")
	var callID string
	for _, line := range strings.Split(string(output), "\n") {
		if !strings.HasPrefix(line, "data: {") {
			continue
		}
		var event struct {
			Type string `json:"type"`
			Item struct {
				CallID string `json:"call_id"`
			} `json:"item"`
		}
		require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event))
		if event.Type == "response.output_item.done" {
			callID = event.Item.CallID
		}
	}
	require.NotEmpty(t, callID)
	first := <-requests
	// A second process uses the same host KV but no process-local history.
	c2 := clientForTest(t, store, binary)
	apply(c2)
	body, _ = json.Marshal(map[string]any{"model": "gpt-6-astra", "input": []any{map[string]any{"type": "function_call_output", "call_id": callID, "output": "18°C"}}, "tools": tools, "stream": true})
	_, output, failure = forwardForTest(t, c2, body, ctx)
	require.Nil(t, failure)
	require.Contains(t, string(output), "18°C")
	second := <-requests
	var firstMeta, secondMeta map[string]string
	require.NoError(t, json.Unmarshal(first["metadata"], &firstMeta))
	require.NoError(t, json.Unmarshal(second["metadata"], &secondMeta))
	require.Equal(t, firstMeta["turn_id"], secondMeta["turn_id"])
	require.Equal(t, "1", secondMeta["agent_iteration"])
	var input []json.RawMessage
	require.NoError(t, json.Unmarshal(second["input"], &input))
	require.Len(t, input, 3)
	require.JSONEq(t, string(native), string(input[1]))
	require.Contains(t, string(input[2]), "call_server")
}

func TestForwardToolReplayAcrossPluginProcesses(t *testing.T) { testForwardReplay(t, "") }

func TestForwardPreserves422AndRejectsInvalidJSONBeforeNetwork(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p := New()
	client, _ := hcplugin.TestPluginGRPCConn(t, false, map[string]hcplugin.Plugin{pluginv1.TransportPluginName: &pluginv1.GRPCPlugin{Impl: p}})
	t.Cleanup(func() { client.Close() })
	dispensed, err := client.Dispense(pluginv1.TransportPluginName)
	require.NoError(t, err)
	c := dispensed.(*pluginv1.TransportClient)
	requests := make(chan bool, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(422)
		w.Write([]byte(`{"error":{"message":"422: Invalid request body."}}`))
	}))
	defer upstream.Close()
	config, _ := json.Marshal(map[string]any{"upstream_base_url": upstream.URL, "proxy_mode": "disabled"})
	applied, err := c.ApplyConfig(ctx, &pluginv1.ApplyConfigRequest{ConfigJson: config})
	require.NoError(t, err)
	require.True(t, applied.Applied)
	invalidStart, invalidBody, failure := forwardForTest(t, c, []byte(`{broken`), ctx)
	require.Nil(t, failure)
	require.Equal(t, int32(400), invalidStart.StatusCode)
	require.Contains(t, string(invalidBody), "TOOL_BRIDGE_REQUEST_INVALID")
	select {
	case <-requests:
		t.Fatal("invalid JSON reached upstream")
	default:
	}
	start, body, failure := forwardForTest(t, c, []byte(`{"input":"hello"}`), ctx)
	require.Nil(t, failure)
	require.Equal(t, int32(422), start.StatusCode)
	require.Contains(t, string(body), "Invalid request body")
	health, err := c.Health(ctx, &pluginv1.HealthRequest{})
	require.NoError(t, err)
	require.Contains(t, health.StatusJson, `"requests_failed":1`)
}

func TestForwardCancellationClosesUpstream(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := clientForTest(t, &testHostKV{values: map[string][]byte{}}, "")
	closed := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"type\":\"response.created\",\"response\":{\"output\":[]}}\n\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(closed)
	}))
	defer upstream.Close()
	config, _ := json.Marshal(map[string]any{"upstream_base_url": upstream.URL, "proxy_mode": "disabled"})
	_, err := c.ApplyConfig(ctx, &pluginv1.ApplyConfigRequest{ConfigJson: config})
	require.NoError(t, err)
	requestCtx, stop := context.WithCancel(ctx)
	defer stop()
	stream, err := c.Forward(requestCtx)
	require.NoError(t, err)
	body := []byte(`{"input":"hello","stream":true}`)
	start := &pluginv1.ForwardRequestStart{AccountId: 7, Platform: "openai", AccountType: "oauth", Method: "POST", Url: "https://chatgpt.com/backend-api/codex/responses", HasBody: true, ContentLength: int64(len(body)), Headers: map[string]*pluginv1.HeaderValues{"Authorization": {Values: []string{"Bearer synthetic"}}, "Chatgpt-Account-Id": {Values: []string{"synthetic"}}}}
	require.NoError(t, stream.Send(&pluginv1.ForwardRequest{Frame: &pluginv1.ForwardRequest_Start{Start: start}}))
	require.NoError(t, stream.Send(&pluginv1.ForwardRequest{Frame: &pluginv1.ForwardRequest_BodyChunk{BodyChunk: body}}))
	require.NoError(t, stream.Send(&pluginv1.ForwardRequest{Frame: &pluginv1.ForwardRequest_BodyEnd{BodyEnd: true}}))
	stream.CloseSend()
	frame, err := stream.Recv()
	require.NoError(t, err)
	require.NotNil(t, frame.GetStart())
	frame, err = stream.Recv()
	require.NoError(t, err)
	require.Contains(t, string(frame.GetBodyChunk()), "response.created")
	stop()
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatal("cancellation did not close upstream HTTP request")
	}
}

func TestPackagedPluginToolReplay(t *testing.T) {
	packagePath := os.Getenv("SUB2API_TEST_BPS_PACKAGE")
	if packagePath == "" {
		t.Skip("set SUB2API_TEST_BPS_PACKAGE to verify the packaged native runtime")
	}
	archive, err := zip.OpenReader(packagePath)
	require.NoError(t, err)
	defer archive.Close()
	var manifest struct {
		Version  string `json:"version"`
		Runtimes map[string]struct {
			Path string `json:"path"`
		} `json:"runtimes"`
	}
	for _, file := range archive.File {
		if file.Name == "manifest.json" {
			reader, err := file.Open()
			require.NoError(t, err)
			require.NoError(t, json.NewDecoder(reader).Decode(&manifest))
			reader.Close()
		}
	}
	require.Equal(t, PluginVersion, manifest.Version)
	nativeOS := os.Getenv("SUB2API_TEST_BPS_RUNTIME")
	require.NotEmpty(t, nativeOS, "provide e.g. darwin-arm64")
	entry, ok := manifest.Runtimes[nativeOS]
	require.True(t, ok)
	binary := filepath.Join(t.TempDir(), "plugin")
	found := false
	for _, file := range archive.File {
		if file.Name == entry.Path {
			reader, err := file.Open()
			require.NoError(t, err)
			data, err := io.ReadAll(reader)
			require.NoError(t, err)
			reader.Close()
			require.NoError(t, os.WriteFile(binary, data, 0700))
			found = true
		}
	}
	require.True(t, found)
	testForwardReplay(t, binary)
	testForwardDiagnosticLogs(t, binary)
	testForwardFailedRequestBodies(t, binary)
	testAlphaSearchNativeForward(t, binary)
}
