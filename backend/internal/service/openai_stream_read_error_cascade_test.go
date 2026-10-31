//go:build unit

package service

import (
	"errors"
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

// 级联 sub2api 上游（apikey 账号指向另一个网关）会用本网关同一套错误码上报
// 传输故障；这些码必须被识别为可 failover，否则未输出前的换号机会会丢失。
func TestOpenAIStreamReadErrorEventRecognizesCascadedGatewayCodes(t *testing.T) {
	for _, code := range []string{
		"stream_read_error",
		OpenAIUpstreamStreamReadErrorCode,
		OpenAIUpstreamHTTP2StreamErrorCode,
		OpenAIUpstreamStreamTruncatedCode,
	} {
		t.Run(code, func(t *testing.T) {
			bare := []byte(`{"code":"` + code + `","message":"Upstream stream failed","param":null,"sequence_number":0,"type":"error"}`)
			require.True(t, isOpenAIStreamReadErrorEvent(bare, "Upstream stream failed"))
			require.True(t, openAIStreamErrorEventShouldFailover(bare, "Upstream stream failed"))

			failed := []byte(`{"type":"response.failed","response":{"status":"failed","output":[],"error":{"code":"` + code + `","message":"Upstream stream failed"}}}`)
			require.True(t, openAIStreamFailedEventShouldFailover(failed, "Upstream stream failed"))
		})
	}
}

func TestOpenAIStreamCascadedHTTP2ErrorFailsOverBeforeOutput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stream := strings.Join([]string{
		"event: error",
		`data: {"code":"upstream_http2_stream_error","message":"Upstream HTTP/2 stream failed","param":null,"sequence_number":0,"type":"error"}`,
		"",
	}, "\n")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(stream))}
	account := &Account{ID: 437, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}}

	_, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "model", "model")
	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr), "pre-output cascaded HTTP/2 failure must fail over, got %v", err)
	require.NotContains(t, rec.Body.String(), "upstream_http2_stream_error", "the intermediate error must not reach the client")
}
