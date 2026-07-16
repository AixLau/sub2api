package service

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/util/logredact"
)

type AdminAuditEvent struct {
	Action          string
	TargetType      string
	TargetID        string
	OperatorID      int64
	OperatorRole    string
	OperatorIP      string
	UserAgent       string
	RequestID       string
	ClientRequestID string
	Before          map[string]any
	After           map[string]any
	Result          string
	ErrorCode       string
}

func EmitAdminAudit(ctx context.Context, event AdminAuditEvent) {
	_ = ctx
	fields := map[string]any{
		"audit":         true,
		"admin_audit":   true,
		"component":     "admin.audit",
		"action":        strings.TrimSpace(event.Action),
		"target_type":   strings.TrimSpace(event.TargetType),
		"target_id":     strings.TrimSpace(event.TargetID),
		"user_id":       event.OperatorID,
		"operator_id":   event.OperatorID,
		"operator_role": strings.TrimSpace(event.OperatorRole),
		"operator_ip":   strings.TrimSpace(event.OperatorIP),
		"user_agent":    strings.TrimSpace(event.UserAgent),
		"request_id":    strings.TrimSpace(event.RequestID),
		"result":        normalizeAdminAuditResult(event.Result),
		"before":        redactAdminAuditMap(event.Before),
		"after":         redactAdminAuditMap(event.After),
		"risk_level":    "medium",
	}
	if clientReqID := strings.TrimSpace(event.ClientRequestID); clientReqID != "" {
		fields["client_request_id"] = clientReqID
	}
	if errorCode := strings.TrimSpace(event.ErrorCode); errorCode != "" {
		fields["error_code"] = errorCode
	}
	logger.WriteSinkEvent("info", "admin.audit", "admin audit event", fields)
}

func normalizeAdminAuditResult(result string) string {
	switch strings.ToLower(strings.TrimSpace(result)) {
	case "attempt":
		return "attempt"
	case "failed", "failure", "error":
		return "failed"
	default:
		return "success"
	}
}

func redactAdminAuditMap(value map[string]any) map[string]any {
	if len(value) == 0 {
		return map[string]any{}
	}
	redacted := logredact.RedactMap(value,
		"secret",
		"token",
		"api_key",
		"apikey",
		"private_key",
		"client_secret",
		"merchant_key",
		"smtp_password",
		"password",
	)
	return redactAdminAuditSensitiveFieldNames(redacted).(map[string]any)
}

func redactAdminAuditSensitiveFieldNames(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			if isAdminAuditSensitiveFieldName(key) {
				out[key] = "***"
				continue
			}
			out[key] = redactAdminAuditSensitiveFieldNames(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = redactAdminAuditSensitiveFieldNames(item)
		}
		return out
	default:
		return value
	}
}

func isAdminAuditSensitiveFieldName(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	if normalized == "" {
		return false
	}
	for _, marker := range []string{"secret", "token", "password", "api_key", "apikey", "private_key", "merchant_key"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}
