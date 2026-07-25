package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestRequestBodyLimitTooLarge(t *testing.T) {
	gin.SetMode(gin.TestMode)

	limit := int64(16)
	router := gin.New()
	router.Use(middleware.RequestBodyLimit(limit))
	router.POST("/test", func(c *gin.Context) {
		_, err := io.ReadAll(c.Request.Body)
		if err != nil {
			if maxErr, ok := extractMaxBytesError(err); ok {
				c.JSON(http.StatusRequestEntityTooLarge, gin.H{
					"error": buildBodyTooLargeMessage(maxErr.Limit),
				})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "read_failed",
			})
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
	require.Equal(t, "客户端在请求体上传完成前取消连接", gotMsg)

	gotDetail, ok := c.Get(service.OpsDiagnosticDetailKey)
	require.True(t, ok)
	require.JSONEq(t, `{
		"source":"request_body_reader",
		"reason":"client_upload_canceled",
		"message":"客户端在请求体上传完成前取消连接",
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

func TestOpenAIHTTPGatewayPipeline_OversizedAdmissionRecordsSafeDiagnostic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"private":"body"}`))
	c.Request.ContentLength = 51 << 20

	h := &OpenAIGatewayHandler{
		contentModerationService: newDisabledContentModerationServiceForHandlerTest(t),
	}
	result := h.EnterOpenAIHTTPGatewayPipeline(c, openAIResponsesHTTPRouteMetaForTest())

	require.True(t, result.Stop)
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	gotDetail, ok := c.Get(service.OpsDiagnosticDetailKey)
	require.True(t, ok)
	require.Equal(t, int64(51<<20), gjson.Get(gotDetail.(string), "content_length").Int())
	require.Equal(t, int64(50<<20), gjson.Get(gotDetail.(string), "limit_bytes").Int())
	require.False(t, gjson.Get(gotDetail.(string), "body_inspected").Bool())
	require.NotContains(t, gotDetail.(string), "private")
}

func TestGatewayPreForwardPipeline_OversizedAdmissionRecordsSafeDiagnostic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(`{"private":"body"}`))
	c.Request.ContentLength = 51 << 20

	h := &GatewayHandler{
		contentModerationService: newDisabledContentModerationServiceForHandlerTest(t),
	}
	result := h.EnterGatewayPreForwardPipeline(c, gatewayCountTokensRouteMetaForTest())

	require.True(t, result.Blocked)
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	gotDetail, ok := c.Get(service.OpsDiagnosticDetailKey)
	require.True(t, ok)
	require.Equal(t, int64(51<<20), gjson.Get(gotDetail.(string), "content_length").Int())
	require.Equal(t, int64(50<<20), gjson.Get(gotDetail.(string), "limit_bytes").Int())
	require.False(t, gjson.Get(gotDetail.(string), "body_inspected").Bool())
	require.NotContains(t, gotDetail.(string), "private")
}
