package service

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/gin-gonic/gin"
	hclog "github.com/hashicorp/go-hclog"
	hcplugin "github.com/hashicorp/go-plugin"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
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
	starts   chan *pluginv1.ForwardRequestStart
	metadata chan metadata.MD
}

func (s *diagnosticStartCapture) Forward(stream grpc.BidiStreamingServer[pluginv1.ForwardRequest, pluginv1.ForwardResponse]) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	s.starts <- first.GetStart()
	md, _ := metadata.FromIncomingContext(stream.Context())
	s.metadata <- md
	return stream.Send(&pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_Error{Error: &pluginv1.ForwardResponseError{Code: "TEST_CAPTURED"}}})
}

func TestPluginRuntimeForwardsIngressCorrelation(t *testing.T) {
	capture := &diagnosticStartCapture{starts: make(chan *pluginv1.ForwardRequestStart, 3), metadata: make(chan metadata.MD, 3)}
	client, _ := hcplugin.TestPluginGRPCConn(t, false, map[string]hcplugin.Plugin{pluginv1.TransportPluginName: &pluginv1.GRPCPlugin{Impl: capture}})
	t.Cleanup(func() { _ = client.Close() })
	service, err := client.Dispense(pluginv1.TransportPluginName)
	require.NoError(t, err)
	runtime := &pluginRuntime{api: service.(*pluginv1.TransportClient)}
	for _, tc := range []struct{ requestID, sessionID string }{
		{"3ce2ebca-f7dc-487a-9a54-52ba030c92e9", "original-client-session"},
		{"", ""},
		{"req-unicode", "会话中文"},
	} {
		requestID := tc.requestID
		ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), ctxkey.RequestID, requestID), 5*time.Second)
		ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(pluginv1.ClientSessionIDMetadataKey, "stale-session", "test-marker", "preserved"))
		requestCtx := context.WithValue(ctx, ctxkey.ClientSessionID, tc.sessionID)
		request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, "https://example.com/responses", http.NoBody)
		require.NoError(t, err)
		request.Header.Set("session_id", "rewritten-upstream-session")
		_, err = runtime.roundTrip(ctx, request, "", &Account{ID: 275})
		require.Error(t, err)
		select {
		case start := <-capture.starts:
			md := <-capture.metadata
			require.Equal(t, []string{sanitizeSessionID(tc.sessionID)}, md.Get(pluginv1.ClientSessionIDMetadataKey))
			require.Equal(t, []string{"preserved"}, md.Get("test-marker"))
			require.Equal(t, "rewritten-upstream-session", request.Header.Get("session_id"))
			original, _ := metadata.FromOutgoingContext(ctx)
			require.Equal(t, []string{"stale-session"}, original.Get(pluginv1.ClientSessionIDMetadataKey))
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

func TestOpenAIRequestBuildersRetainClientSessionForAudit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"chatgpt_account_id": "upstream-account"}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}}
	for _, mode := range []string{"responses", "passthrough", "search"} {
		t.Run(mode, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			c.Request.Header.Set("session_id", "client-session-original")
			c.Set("api_key", &APIKey{ID: 99})
			var req *http.Request
			var err error
			switch mode {
			case "responses":
				req, err = svc.buildUpstreamRequest(c.Request.Context(), c, account, []byte(`{"model":"gpt-5"}`), "token", false, "", true)
			case "passthrough":
				req, err = svc.buildUpstreamRequestOpenAIPassthrough(c.Request.Context(), c, account, []byte(`{"model":"gpt-5"}`), "token")
			case "search":
				req, err = svc.buildOpenAIAlphaSearchRequest(c.Request.Context(), c, account, []byte(`{"model":"gpt-5"}`), "token")
			}
			require.NoError(t, err)
			require.Equal(t, "client-session-original", req.Context().Value(ctxkey.ClientSessionID))
			require.NotEqual(t, "client-session-original", req.Header.Get("session_id"))
			require.Empty(t, req.Header.Get(pluginv1.ClientSessionIDMetadataKey))
		})
	}
}
