package service

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	hclog "github.com/hashicorp/go-hclog"
	hcplugin "github.com/hashicorp/go-plugin"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestPluginRuntimeLoggerPreservesStructuredFieldsAndLevels(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	logger := newPluginRuntimeLogger(&PluginInstallation{PluginKey: "example.plugin", Version: "0.4.2"})
	logger.Named("process").With("request_id", "req-observe").Warn("bps.tool_rejected",
		"account_id", 275, "tool", map[string]any{"stage": "upstream_tool_code", "json_offset": 22})
	var event map[string]any
	require.NoError(t, json.Unmarshal(output.Bytes(), &event))
	require.Equal(t, "WARN", event["level"])
	require.Equal(t, "bps.tool_rejected", event["msg"])
	require.Equal(t, "req-observe", event["request_id"])
	require.Equal(t, "plugin", event["component"])
	require.Equal(t, "example.plugin", event["plugin_id"])
	require.Equal(t, float64(275), event["account_id"])
	require.Equal(t, float64(22), event["tool"].(map[string]any)["json_offset"])
	output.Reset()
	logger.Debug("framework noise")
	require.Empty(t, output.String())
	logger.Log(hclog.Error, "plugin_failure", "request_id", "req-error")
	require.NoError(t, json.Unmarshal(output.Bytes(), &event))
	require.Equal(t, "ERROR", event["level"])
}

type diagnosticStartCapture struct {
	pluginv1.UnimplementedTransportPluginServer
	starts chan *pluginv1.ForwardRequestStart
}

func (s *diagnosticStartCapture) Forward(stream grpc.BidiStreamingServer[pluginv1.ForwardRequest, pluginv1.ForwardResponse]) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	s.starts <- first.GetStart()
	return stream.Send(&pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_Error{Error: &pluginv1.ForwardResponseError{Code: "TEST_CAPTURED"}}})
}

func TestPluginRuntimeForwardsIngressRequestID(t *testing.T) {
	capture := &diagnosticStartCapture{starts: make(chan *pluginv1.ForwardRequestStart, 2)}
	client, _ := hcplugin.TestPluginGRPCConn(t, false, map[string]hcplugin.Plugin{pluginv1.TransportPluginName: &pluginv1.GRPCPlugin{Impl: capture}})
	t.Cleanup(func() { _ = client.Close() })
	service, err := client.Dispense(pluginv1.TransportPluginName)
	require.NoError(t, err)
	runtime := &pluginRuntime{api: service.(*pluginv1.TransportClient)}
	for _, requestID := range []string{"3ce2ebca-f7dc-487a-9a54-52ba030c92e9", ""} {
		ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), ctxkey.RequestID, requestID), 5*time.Second)
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.com/responses", http.NoBody)
		require.NoError(t, err)
		_, err = runtime.roundTrip(ctx, request, "", &Account{ID: 275})
		require.Error(t, err)
		select {
		case start := <-capture.starts:
			if requestID != "" {
				require.Equal(t, requestID, start.RequestId)
			} else {
				require.NotEmpty(t, start.RequestId)
			}
		case <-ctx.Done():
			t.Fatal("missing forwarded request metadata")
		}
		cancel()
	}
}
