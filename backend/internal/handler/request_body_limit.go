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
	"github.com/Wei-Shaw/sub2api/internal/pkg/clientmsg"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

const maxLoggedDataURLLengths = 128

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

type requestBodyImageSummary struct {
	ImageFieldCount         int
	DataURLCount            int
	DataURLLengths          []int
	DataURLLengthsTruncated bool
}

func summarizeRequestBodyImages(body []byte) (requestBodyImageSummary, bool) {
	summary := requestBodyImageSummary{DataURLLengths: make([]int, 0)}
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return summary, false
	}

	var collectDataURLs func(gjson.Result)
	collectDataURLs = func(value gjson.Result) {
		switch {
		case value.IsArray(), value.IsObject():
			value.ForEach(func(_, child gjson.Result) bool {
				collectDataURLs(child)
				return true
			})
		case value.Type == gjson.String:
			raw := value.String()
			if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(raw)), "data:") {
				return
			}
			summary.DataURLCount++
			if len(summary.DataURLLengths) < maxLoggedDataURLLengths {
				summary.DataURLLengths = append(summary.DataURLLengths, len(raw))
			} else {
				summary.DataURLLengthsTruncated = true
			}
		}
	}

	var walk func(gjson.Result)
	walk = func(value gjson.Result) {
		switch {
		case value.IsArray():
			value.ForEach(func(_, child gjson.Result) bool {
				walk(child)
				return true
			})
		case value.IsObject():
			value.ForEach(func(key, child gjson.Result) bool {
				if isRequestImageField(key.String()) {
					summary.ImageFieldCount++
					collectDataURLs(child)
					return true
				}
				walk(child)
				return true
			})
		}
	}
	walk(gjson.ParseBytes(body))
	return summary, true
}

func isRequestImageField(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "image_url", "image", "images", "mask", "mask_image_url", "reference_images", "inline_data", "inlinedata":
		return true
	default:
		return false
	}
}

func markOpsRequestBodyTooLarge(c *gin.Context, limit int64, inspectedBody []byte, bodyInspected bool) {
	if c == nil {
		return
	}

	contentLength := int64(-1)
	if c.Request != nil {
		contentLength = c.Request.ContentLength
	}
	detail := map[string]any{
		"source":                     "gateway_admission",
		"reason":                     "request_body_too_large",
		"content_length":             contentLength,
		"limit_bytes":                limit,
		"body_inspected":             bodyInspected,
		"inspection_complete":        false,
		"image_field_count":          nil,
		"data_url_count":             nil,
		"data_url_lengths":           nil,
		"data_url_lengths_truncated": false,
	}

	var imageFieldCount any
	var dataURLCount any
	var dataURLLengths any
	dataURLLengthsTruncated := false
	inspectionComplete := false
	if bodyInspected {
		if summary, ok := summarizeRequestBodyImages(inspectedBody); ok {
			inspectionComplete = true
			imageFieldCount = summary.ImageFieldCount
			dataURLCount = summary.DataURLCount
			dataURLLengths = summary.DataURLLengths
			dataURLLengthsTruncated = summary.DataURLLengthsTruncated
			detail["inspection_complete"] = true
			detail["image_field_count"] = summary.ImageFieldCount
			detail["data_url_count"] = summary.DataURLCount
			detail["data_url_lengths"] = summary.DataURLLengths
			detail["data_url_lengths_truncated"] = summary.DataURLLengthsTruncated
		}
	}

	ctx := context.Background()
	if c.Request != nil {
		ctx = c.Request.Context()
	}
	logger.FromContext(ctx).Warn("request_body_too_large_rejected",
		zap.Int64("request_content_length", contentLength),
		zap.Int64("request_body_limit_bytes", limit),
		zap.Bool("request_body_inspected", bodyInspected),
		zap.Bool("request_body_inspection_complete", inspectionComplete),
		zap.Any("request_image_field_count", imageFieldCount),
		zap.Any("request_data_url_count", dataURLCount),
		zap.Any("request_data_url_lengths", dataURLLengths),
		zap.Bool("request_data_url_lengths_truncated", dataURLLengthsTruncated),
	)

	raw, _ := json.Marshal(detail)
	service.SetOpsDiagnostic(c, clientmsg.Localize(buildBodyTooLargeMessage(limit)), string(raw))
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
		message = "网络连接不稳定，请检查网络后重试。"
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
