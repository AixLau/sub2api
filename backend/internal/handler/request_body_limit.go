package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func extractMaxBytesError(err error) (*http.MaxBytesError, bool) {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return maxErr, true
	}
	return nil, false
}

func formatBodyLimit(limit int64) string {
	const mb = 1024 * 1024
	if limit >= mb {
		return fmt.Sprintf("%dMB", limit/mb)
	}
	return fmt.Sprintf("%dB", limit)
}

func buildBodyTooLargeMessage(limit int64) string {
	return fmt.Sprintf("Request body too large, limit is %s", formatBodyLimit(limit))
}

func markOpsRequestBodyReadError(c *gin.Context, err error) {
	if c == nil || err == nil {
		return
	}
	rawErr := strings.TrimSpace(err.Error())
	reason := "request_body_read_failed"
	message := "读取请求体失败"
	if errors.Is(err, context.Canceled) || strings.Contains(strings.ToLower(rawErr), "context canceled") {
		reason = "client_upload_canceled"
		message = "客户端在请求体上传完成前取消连接"
	} else if errors.Is(err, io.ErrUnexpectedEOF) || strings.Contains(strings.ToLower(rawErr), "unexpected eof") {
		reason = "client_upload_interrupted"
		message = "请求体上传未完成：客户端在请求体传输完成前断开连接"
	}
	detail := map[string]string{
		"source":  "request_body_reader",
		"reason":  reason,
		"message": message,
	}
	if rawErr != "" {
		detail["raw_error"] = rawErr
	}
	if c.Request != nil && c.Request.ContentLength > 0 {
		detail["content_length"] = strconv.FormatInt(c.Request.ContentLength, 10)
	}
	raw, _ := json.Marshal(detail)
	service.SetOpsDiagnostic(c, message, string(raw))
}

func readLenientJSONRequestBodyWithPrealloc(req *http.Request, cfg *config.Config) ([]byte, error) {
	limit := gatewayMaxBodySize(cfg)
	if req != nil {
		if protected := service.RequestBodyLimit(req.Context()); protected > 0 && (limit <= 0 || protected < limit) {
			limit = protected
		}
	}
	return pkghttputil.ReadLenientJSONRequestBodyWithPrealloc(req, limit)
}

func gatewayMaxBodySize(cfg *config.Config) int64 {
	if cfg == nil {
		return 0
	}
	return cfg.Gateway.MaxBodySize
}
