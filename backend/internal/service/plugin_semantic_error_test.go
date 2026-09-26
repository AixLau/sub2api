package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type pluginSemanticReadCloser struct {
	r   io.Reader
	err error
}

func (r *pluginSemanticReadCloser) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if err == io.EOF {
		return n, r.err
	}
	return n, err
}
func (r *pluginSemanticReadCloser) Close() error { return nil }
func pluginSemanticLastSSEData(output string) string {
	var last string
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "data: ") {
			last = strings.TrimPrefix(line, "data: ")
		}
	}
	return last
}

func TestPluginSemanticFailureStreamDoesNotFailoverOrQuarantine(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, passthrough := range []bool{false, true} {
		for _, partial := range []bool{false, true} {
			for _, code := range []string{"TOOL_BRIDGE_CALL_INVALID", "TOOL_BRIDGE_CAPABILITY_UNAVAILABLE", "TOOL_BRIDGE_ENCRYPTED_REPLAY_UNSAFE"} {
				t.Run(fmt.Sprintf("passthrough=%t/partial=%t/%s", passthrough, partial, code), func(t *testing.T) {
					proxyID := int64(42)
					account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, ProxyID: &proxyID}
					svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}}
					svc.openaiProxyStreamCircuit = newOpenAIProxyStreamCircuit(openAIProxyStreamCircuitSettings{failureThreshold: 1, failureWindow: time.Minute, quarantineTTL: time.Minute, maxEntries: 16})
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
					text := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_semantic\"}}\n\n"
					if partial {
						text += "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n"
					}
					failure := &PluginTransportError{Code: code, Message: "bad payload, not a network disconnect", RequestSent: true}
					resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: &pluginSemanticReadCloser{r: strings.NewReader(text), err: fmt.Errorf("wrapped: %w", failure)}}
					var err error
					if passthrough {
						_, err = svc.handleStreamingResponsePassthrough(c.Request.Context(), resp, c, account, time.Now(), "model", "model")
					} else {
						_, err = svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "model", "model")
					}
					require.ErrorIs(t, err, failure)
					var failover *UpstreamFailoverError
					require.False(t, errors.As(err, &failover))
					require.False(t, svc.isOpenAIProxyStreamQuarantined(context.Background(), account))
					output := rec.Body.String()
					require.Equal(t, 1, strings.Count(output, "event: response.failed"))
					require.Equal(t, code, gjson.Get(pluginSemanticLastSSEData(output), "response.error.code").String())
					require.NotContains(t, output, "stream_read_error")
					require.True(t, IsResponseCommitted(c), "handler must not append another failure")
				})
			}
		}
	}
}

func TestPluginSemanticClassificationPreservesNetworkFailures(t *testing.T) {
	proxyID := int64(43)
	account := &Account{ID: 1, Platform: PlatformOpenAI, ProxyID: &proxyID}
	svc := &OpenAIGatewayService{}
	svc.openaiProxyStreamCircuit = newOpenAIProxyStreamCircuit(openAIProxyStreamCircuitSettings{failureThreshold: 1, failureWindow: time.Minute, quarantineTTL: time.Minute, maxEntries: 16})
	semantic := &PluginTransportError{Code: "TOOL_BRIDGE_CALL_INVALID", Message: "connection refused"}
	svc.recordOpenAIProxyStreamDisconnect(account, fmt.Errorf("wrapped: %w", semantic), "test")
	require.False(t, svc.isOpenAIProxyStreamQuarantined(context.Background(), account))
	svc.recordOpenAIProxyStreamDisconnect(account, io.ErrUnexpectedEOF, "test")
	require.True(t, svc.isOpenAIProxyStreamQuarantined(context.Background(), account), "real disconnects must still quarantine")
	require.False(t, classifyUpstreamTransportError(semantic).Persistent)
	require.False(t, shouldClassifyOpenAIUpstreamStreamReadError(fmt.Errorf("wrapped: %w", semantic)))
	for _, code := range []string{"TOOL_BRIDGE_STREAM_FAILED", "UPSTREAM_RESPONSE_FAILED", "UPSTREAM_REQUEST_FAILED"} {
		_, matched := pluginSemanticTransportError(&PluginTransportError{Code: code, RequestSent: true})
		require.False(t, matched)
	}
	require.True(t, shouldClassifyOpenAIUpstreamStreamReadError(io.ErrUnexpectedEOF))
	for _, code := range []string{"TOOL_BRIDGE_CALL_INVALID", "TOOL_BRIDGE_CAPABILITY_UNAVAILABLE", "TOOL_BRIDGE_ENCRYPTED_REPLAY_UNSAFE", "invalid_encrypted_content"} {
		payload := []byte(fmt.Sprintf("{\"response\":{\"error\":{\"code\":%q,\"message\":\"please retry\"}}}", code))
		require.False(t, openAIStreamFailedEventShouldFailover(payload, "please retry"))
		require.False(t, openAIStreamErrorEventShouldFailover(payload, "please retry"))
		require.Equal(t, http.StatusBadRequest, openAIStreamFailureStatus(payload, "please retry"))
	}
	require.True(t, openAIStreamFailedEventShouldFailover([]byte(`{"response":{"error":{"code":"server_error"}}}`), "temporary failure"))
}

func TestPluginSemanticWireFailureEndsOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, passthrough := range []bool{false, true} {
		for _, partial := range []bool{false, true} {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			payload := pluginSemanticFailurePayload("resp_wire", "model", &PluginTransportError{Code: "TOOL_BRIDGE_CALL_INVALID", Message: "bad arguments"})
			text := ""
			if partial {
				text = "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n"
			}
			text += "event: response.failed\ndata: " + string(payload) + "\n\n"
			resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(text))}
			svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}}
			account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
			var err error
			if passthrough {
				_, err = svc.handleStreamingResponsePassthrough(c.Request.Context(), resp, c, account, time.Now(), "model", "model")
			} else {
				_, err = svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "model", "model")
			}
			require.Error(t, err)
			var failover *UpstreamFailoverError
			require.False(t, errors.As(err, &failover))
			require.Equal(t, 1, strings.Count(w.Body.String(), "event: response.failed"))
			require.Contains(t, w.Body.String(), "TOOL_BRIDGE_CALL_INVALID")
			require.True(t, IsResponseCommitted(c))
		}
	}
}

func TestPluginSemanticRequestFailureNeverChangesAccounts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &OpenAIGatewayService{}
	for _, sent := range []bool{false, true} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		failure := &PluginTransportError{Code: "TOOL_BRIDGE_REQUEST_INVALID", RequestSent: sent}
		err := svc.handleOpenAIUpstreamTransportError(c.Request.Context(), c, &Account{ID: 1, Platform: PlatformOpenAI}, failure, false)
		require.ErrorIs(t, err, failure)
		var failover *UpstreamFailoverError
		require.False(t, errors.As(err, &failover))
	}
}
