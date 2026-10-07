//go:build unit

package service

import (
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
)

const processingFailureMessage = "An error occurred while processing your request. You can retry your request, or contact us through our help center at help.openai.com if the error persists. Please include the request ID rid-processing in your message."

func TestOpenAIProcessingFailureRetryClassification(t *testing.T) {
	for _, status := range []int{400, 500, 502, 503, 504} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			payload := []byte(fmt.Sprintf(`{"error":{"code":"server_error","message":%q}}`, processingFailureMessage))
			require.True(t, isOpenAIProcessingFailure(status, processingFailureMessage, payload))
			// Ordinary HTTP failures already have a transport retry policy.
			failure := newOpenAIUpstreamFailoverError(status, nil, payload, processingFailureMessage, false)
			require.NotEqual(t, OpenAIProcessingFailureReason, failure.Reason)
			require.Zero(t, failure.SameAccountRetryMax)
		})
	}
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"ordinary server error", 502, `{"error":{"code":"server_error","message":"a different provider failure"}}`},
		{"echoed request content", 502, fmt.Sprintf(`{"error":{"message":"upstream unavailable"},"echo":%q}`, processingFailureMessage)},
		{"auth", 401, fmt.Sprintf(`{"error":{"message":%q}}`, processingFailureMessage)},
		{"rate limit", 429, fmt.Sprintf(`{"error":{"message":%q}}`, processingFailureMessage)},
		{"context window", 400, fmt.Sprintf(`{"error":{"code":"context_length_exceeded","message":%q}}`, processingFailureMessage)},
		{"bridge protocol", 400, fmt.Sprintf(`{"error":{"code":"TOOL_BRIDGE_CALL_INVALID","message":%q}}`, processingFailureMessage)},
		{"specific parameter error", 400, fmt.Sprintf(`{"error":{"code":"invalid_parameter","message":%q}}`, processingFailureMessage)},
		{"policy", 400, fmt.Sprintf(`{"error":{"code":"content_policy_violation","message":%q}}`, processingFailureMessage)},
		{"access state", 502, fmt.Sprintf(`{"error":{"code":"deactivated_workspace","message":%q}}`, processingFailureMessage)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.False(t, isOpenAIProcessingFailure(tc.status, "", []byte(tc.body)))
		})
	}
}

func TestOpenAIProcessingFailureRetryBeforeSemanticOutput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, passthrough := range []bool{false, true} {
		for _, event := range []string{"response.failed", "error"} {
			t.Run(fmt.Sprintf("passthrough=%t/%s", passthrough, event), func(t *testing.T) {
				payload := fmt.Sprintf(`{"type":"response.failed","response":{"id":"resp_failed","status":"failed","error":{"code":"server_error","message":%q}}}`, processingFailureMessage)
				if event == "error" {
					payload = fmt.Sprintf(`{"type":"error","error":{"code":"server_error","message":%q}}`, processingFailureMessage)
				}
				stream := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_failed\"}}\n\n: keepalive\n\nevent: " + event + "\ndata: " + payload + "\n\n"
				svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}}
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(stream))}
				account := &Account{ID: 296, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
				var err error
				if passthrough {
					_, err = svc.handleStreamingResponsePassthrough(c.Request.Context(), resp, c, account, time.Now(), "gpt-6-astra", "gpt-6-astra")
				} else {
					_, err = svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "gpt-6-astra", "gpt-6-astra")
				}
				var failure *UpstreamFailoverError
				require.True(t, errors.As(err, &failure), "expected a retry signal: %v", err)
				require.Equal(t, OpenAIProcessingFailureReason, failure.Reason)
				require.True(t, failure.RetryableOnSameAccount)
				require.True(t, failure.SafeToFailoverAfterWrite)
				require.Equal(t, 5, failure.SameAccountRetryMax)
				require.True(t, failure.RequestScopedTransient)
				require.Equal(t, GatewayFailureScopeRequest, failure.Scope)
				require.Equal(t, http.StatusBadGateway, failure.ClientStatusCode)
				require.False(t, failure.ShouldReportAccountScheduleFailure())
				require.NotContains(t, rec.Body.String(), "response.failed")
				require.NotContains(t, rec.Body.String(), processingFailureMessage)
			})
		}
	}
}

func TestOpenAIStreamReadErrorEventUsesBoundedOAuthRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	payload := []byte(`{"type":"response.failed","response":{"id":"resp_read_error","model":"gpt-6.1-sol","status":"failed","output":[],"error":{"code":"stream_read_error","message":"stream_read_error","type":"upstream_error"}}}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	account := &Account{ID: 297, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	svc := &OpenAIGatewayService{}

	require.True(t, openAIStreamFailedEventShouldFailover(payload, "stream_read_error"))
	failure := svc.newOpenAIStreamFailoverErrorWithModel(c, account, true, "rid-read-error", payload, "stream_read_error", "gpt-6.1-sol")
	require.True(t, failure.RetryableOnSameAccount)
	require.True(t, failure.RequestScopedTransient)
	require.Equal(t, OpenAIStreamReadFailureReason, failure.Reason)
	require.Equal(t, OpenAIStreamReadFailureRetryLimit, failure.SameAccountRetryMax)
	require.Equal(t, GatewayFailureScopeRequest, failure.Scope)
	require.False(t, failure.ShouldReportAccountScheduleFailure())
	require.False(t, c.Writer.Written(), "pre-output semantic failures must remain available for server retry")
}

func TestOpenAIStreamReadErrorEventDoesNotEnableOAuthRetryAfterSemanticOutput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stream := strings.Join([]string{
		"event: response.output_text.delta",
		`data: {"type":"response.output_text.delta","delta":"partial"}`,
		"",
		"event: response.failed",
		`data: {"type":"response.failed","response":{"id":"resp_read_error","status":"failed","output":[],"error":{"code":"stream_read_error","message":"stream_read_error","type":"upstream_error"}}}`,
		"",
	}, "\n")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(stream))}
	account := &Account{ID: 298, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}}

	_, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "model", "model")
	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr), "a stream error after visible output must not replay the request")
	require.Contains(t, rec.Body.String(), "stream_read_error")
}
