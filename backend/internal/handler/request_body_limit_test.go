package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

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
