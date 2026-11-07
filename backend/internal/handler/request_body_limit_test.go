package handler

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRequestBodyLimitTooLarge(t *testing.T) {
	gin.SetMode(gin.TestMode)

	limit := int64(16)
	router := gin.New()
	router.Use(middleware.RequestBodyLimit(limit))
	router.POST("/test", func(c *gin.Context) {
		_, err := io.ReadAll(c.Request.Body)
		if err != nil {
			status, message := requestBodyReadFailure(nil, c.Request, err)
			c.JSON(status, gin.H{"error": message})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	payload := bytes.Repeat([]byte("a"), int(limit+1))
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(payload))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	require.Contains(t, recorder.Body.String(), buildBodyTooLargeMessage(limit))
}

func TestMarkOpsRequestBodyReadError_ContextCanceled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	req.ContentLength = 35962130
	c.Request = req

	markOpsRequestBodyReadError(c, context.Canceled)

	gotMsg, ok := c.Get(service.OpsDiagnosticMessageKey)
	require.True(t, ok)
	require.Equal(t, "网络连接不稳定，请检查网络后重试。", gotMsg)

	gotDetail, ok := c.Get(service.OpsDiagnosticDetailKey)
	require.True(t, ok)
	require.JSONEq(t, `{
		"source":"request_body_reader",
		"reason":"client_upload_canceled",
		"message":"网络连接不稳定，请检查网络后重试。",
		"raw_error":"context canceled",
		"content_length":"35962130"
	}`, gotDetail.(string))
}

func TestMarkOpsRequestBodyReadError_UnexpectedEOF(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	req.ContentLength = 1056991
	c.Request = req

	markOpsRequestBodyReadError(c, io.ErrUnexpectedEOF)

	gotMsg, ok := c.Get(service.OpsDiagnosticMessageKey)
	require.True(t, ok)
	require.Equal(t, "请求体上传未完成：客户端在请求体传输完成前断开连接", gotMsg)

	gotDetail, ok := c.Get(service.OpsDiagnosticDetailKey)
	require.True(t, ok)
	require.JSONEq(t, `{
		"source":"request_body_reader",
		"reason":"client_upload_interrupted",
		"message":"请求体上传未完成：客户端在请求体传输完成前断开连接",
		"raw_error":"unexpected EOF",
		"content_length":"1056991"
	}`, gotDetail.(string))
}

func TestSummarizeRequestBodyImages_RecordsOnlyLengths(t *testing.T) {
	firstDataURL := "data:image/png;base64,first-secret"
	secondDataURL := "data:image/jpeg;base64,second-secret"
	body := []byte(`{
		"input":[
			{"type":"input_image","image_url":"` + firstDataURL + `"},
			{"type":"input_image","image_url":{"url":"https://example.com/private-image.png"}}
		],
		"image":{"url":"` + secondDataURL + `"},
		"prompt":"data:image/png;base64,prompt-text-must-not-count"
	}`)

	summary, complete := summarizeRequestBodyImages(body)

	require.True(t, complete)
	require.Equal(t, 3, summary.ImageFieldCount)
	require.Equal(t, 2, summary.DataURLCount)
	require.Equal(t, []int{len(firstDataURL), len(secondDataURL)}, summary.DataURLLengths)
	require.False(t, summary.DataURLLengthsTruncated)
}

func TestSummarizeRequestBodyImages_BoundsLengthList(t *testing.T) {
	var body strings.Builder
	body.WriteString(`{"images":[`)
	for i := 0; i < maxLoggedDataURLLengths+2; i++ {
		if i > 0 {
			body.WriteByte(',')
		}
		body.WriteString(`"data:image/png;base64,secret"`)
	}
	body.WriteString(`]}`)

	summary, complete := summarizeRequestBodyImages([]byte(body.String()))

	require.True(t, complete)
	require.Equal(t, 1, summary.ImageFieldCount)
	require.Equal(t, maxLoggedDataURLLengths+2, summary.DataURLCount)
	require.Len(t, summary.DataURLLengths, maxLoggedDataURLLengths)
	require.True(t, summary.DataURLLengthsTruncated)
}

func TestMarkOpsRequestBodyTooLarge_EarlyRejectDoesNotReadOrLogBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader("private-body"))
	req.ContentLength = 90 << 20
	c.Request = req

	markOpsRequestBodyTooLarge(c, 80<<20, nil, false)

	gotMsg, ok := c.Get(service.OpsDiagnosticMessageKey)
	require.True(t, ok)
	require.Equal(t, "请求体过大，最大允许 80MB", gotMsg)

	gotDetail, ok := c.Get(service.OpsDiagnosticDetailKey)
	require.True(t, ok)
	require.JSONEq(t, `{
		"source":"gateway_admission",
		"reason":"request_body_too_large",
		"content_length":94371840,
		"limit_bytes":83886080,
		"body_inspected":false,
		"inspection_complete":false,
		"image_field_count":null,
		"data_url_count":null,
		"data_url_lengths":null,
		"data_url_lengths_truncated":false
	}`, gotDetail.(string))
	require.NotContains(t, gotDetail.(string), "private-body")
}

func TestMarkOpsRequestBodyTooLarge_InspectedBodyLogsMetadataOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	dataURL := "data:image/png;base64,do-not-log-this"
	body := []byte(`{"input":[{"type":"input_image","image_url":"` + dataURL + `"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request = req

	markOpsRequestBodyTooLarge(c, 32, body, true)

	gotDetail, ok := c.Get(service.OpsDiagnosticDetailKey)
	require.True(t, ok)
	require.JSONEq(t, `{
		"source":"gateway_admission",
		"reason":"request_body_too_large",
		"content_length":86,
		"limit_bytes":32,
		"body_inspected":true,
		"inspection_complete":true,
		"image_field_count":1,
		"data_url_count":1,
		"data_url_lengths":[37],
		"data_url_lengths_truncated":false
	}`, gotDetail.(string))
	require.NotContains(t, gotDetail.(string), "do-not-log-this")
	require.NotContains(t, gotDetail.(string), dataURL)
}

func TestRequestBodyReadersUseGatewayConfiguredLimit(t *testing.T) {
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxBodySize: 32}}
	for name, read := range map[string]func(*http.Request, *config.Config) ([]byte, error){"raw": readRawRequestBodyWithLimit, "json": readLenientJSONRequestBodyWithPrealloc} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(strings.Repeat("x", 64)))
			_, err := read(req, cfg)
			maxErr, ok := extractMaxBytesError(err)
			require.True(t, ok)
			require.Equal(t, int64(32), maxErr.Limit)
		})
	}
}

func TestHandleRequestBodyReadErrorCancellation(t *testing.T) {
	for _, tc := range []struct {
		name      string
		cancelled bool
		committed bool
		readErr   error
		handled   bool
	}{
		{name: "cancelled", cancelled: true, handled: true},
		{name: "cancelled_after_commit", cancelled: true, committed: true, handled: true},
		{name: "ordinary_read_error"},
		// An HTTP/1 read deadline also cancels the request context; the
		// timeout must reach requestBodyReadFailure (408).
		{name: "read_timeout_with_cancelled_context", cancelled: true, readErr: &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancelled {
				cancel()
			}
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
			if tc.committed {
				_, err := c.Writer.WriteString("already committed")
				require.NoError(t, err)
			}
			bodyBefore := recorder.Body.String()
			readErr := tc.readErr
			if readErr == nil {
				readErr = io.ErrUnexpectedEOF
			}
			require.Equal(t, tc.handled, handleRequestBodyReadError(c, nil, readErr))
			require.Equal(t, tc.handled, c.IsAborted())
			status := http.StatusOK
			if tc.handled && !tc.committed {
				status = statusClientClosedRequest
			}
			require.Equal(t, status, c.Writer.Status())
			require.Equal(t, bodyBefore, recorder.Body.String(), "cancelled clients must not receive an error body")
		})
	}
}
