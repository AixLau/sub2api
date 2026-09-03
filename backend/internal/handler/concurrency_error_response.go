package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const statusClientClosedRequest = 499

const (
	gatewayQueueFullCode        = "gateway_queue_full"
	gatewayConcurrencyLimitCode = "gateway_concurrency_limit"
)

func concurrencyErrorResponse(err error, slotType string) (int, string, string, string) {
	var waitQueueFullErr *WaitQueueFullError
	if errors.As(err, &waitQueueFullErr) {
		return http.StatusTooManyRequests, "rate_limit_error", gatewayQueueFullCode,
			"Too many pending requests, please retry later"
	}

	var concurrencyErr *ConcurrencyError
	if errors.As(err, &concurrencyErr) {
		if concurrencyErr.SlotType != "" {
			slotType = concurrencyErr.SlotType
		}
		return http.StatusTooManyRequests, "rate_limit_error", gatewayConcurrencyLimitCode,
			fmt.Sprintf("Concurrency limit exceeded for %s, please retry later", slotType)
	}

	if errors.Is(err, context.Canceled) {
		return statusClientClosedRequest, "api_error", "", "context canceled"
	}

	return http.StatusServiceUnavailable, "api_error", "", "Service temporarily unavailable, please retry later"
}

func markOpsConcurrencyErrorDiagnostic(c *gin.Context, err error) {
	if c == nil || err == nil {
		return
	}
	rawErr := strings.TrimSpace(err.Error())
	message := "请求处理失败：并发控制或请求上下文异常"
	reason := "concurrency_or_context_error"
	if errors.Is(err, context.Canceled) {
		message = "客户端已断开连接或取消请求，网关停止继续处理"
		reason = "client_request_canceled"
	} else if errors.Is(err, context.DeadlineExceeded) {
		message = "请求处理超时：网关等待并发资源或下游处理超时"
		reason = "request_context_deadline_exceeded"
	}
	detail := map[string]string{
		"source":  "request_context",
		"reason":  reason,
		"message": message,
	}
	if rawErr != "" {
		detail["raw_error"] = rawErr
	}
	raw, _ := json.Marshal(detail)
	service.SetOpsDiagnostic(c, message, string(raw))
}
