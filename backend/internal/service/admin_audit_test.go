package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

func TestEmitAdminAudit_WritesStructuredRedactedSystemLog(t *testing.T) {
	var captured []*OpsInsertSystemLogInput
	repo := &opsRepoMock{
		BatchInsertSystemLogsFn: func(_ context.Context, inputs []*OpsInsertSystemLogInput) (int64, error) {
			captured = append(captured, inputs...)
			return int64(len(inputs)), nil
		},
	}
	sink := NewOpsSystemLogSink(repo)
	sink.batchSize = 1
	sink.flushInterval = time.Hour
	logger.SetSink(sink)
	t.Cleanup(func() { logger.SetSink(nil) })

	EmitAdminAudit(context.Background(), AdminAuditEvent{
		Action:          "settings.update",
		TargetType:      "system_settings",
		TargetID:        "global",
		OperatorID:      42,
		OperatorRole:    "admin",
		OperatorIP:      "203.0.113.9",
		UserAgent:       "unit-test",
		RequestID:       "req-1",
		ClientRequestID: "creq-1",
		Result:          "success",
		Before: map[string]any{
			"enabled":       false,
			"client_secret": "secret-before",
		},
		After: map[string]any{
			"enabled":       true,
			"client_secret": "secret-after",
		},
	})

	inserted, err := sink.flushBatch(context.Background(), drainOpsSystemLogSinkQueue(sink))
	if err != nil {
		t.Fatalf("flushBatch() error: %v", err)
	}
	if inserted != 1 || len(captured) != 1 {
		t.Fatalf("captured=%d inserted=%d, want 1", len(captured), inserted)
	}

	item := captured[0]
	if item.Level != "info" {
		t.Fatalf("level = %q, want info", item.Level)
	}
	if item.Component != "admin.audit" {
		t.Fatalf("component = %q, want admin.audit", item.Component)
	}
	if item.Message != "admin audit event" {
		t.Fatalf("message = %q, want admin audit event", item.Message)
	}
	if item.RequestID != "req-1" || item.ClientRequestID != "creq-1" {
		t.Fatalf("request ids not propagated: %+v", item)
	}
	if item.UserID == nil || *item.UserID != 42 {
		t.Fatalf("user id not propagated: %+v", item.UserID)
	}
	var extra map[string]any
	if err := json.Unmarshal([]byte(item.ExtraJSON), &extra); err != nil {
		t.Fatalf("extra json invalid: %v", err)
	}
	for _, key := range []string{"audit", "admin_audit", "action", "target_type", "target_id", "operator_role", "operator_ip", "user_agent", "result"} {
		if _, ok := extra[key]; !ok {
			t.Fatalf("extra missing %s: %s", key, item.ExtraJSON)
		}
	}
	if extra["audit"] != true || extra["admin_audit"] != true {
		t.Fatalf("audit markers missing: %s", item.ExtraJSON)
	}
	if strings.Contains(item.ExtraJSON, "secret-before") || strings.Contains(item.ExtraJSON, "secret-after") {
		t.Fatalf("sensitive values leaked: %s", item.ExtraJSON)
	}
	if !strings.Contains(item.ExtraJSON, "***") {
		t.Fatalf("redacted marker missing: %s", item.ExtraJSON)
	}
}

func TestEmitAdminAudit_RedactsSensitiveFieldNamesWithPrefixes(t *testing.T) {
	var captured []*OpsInsertSystemLogInput
	repo := &opsRepoMock{
		BatchInsertSystemLogsFn: func(_ context.Context, inputs []*OpsInsertSystemLogInput) (int64, error) {
			captured = append(captured, inputs...)
			return int64(len(inputs)), nil
		},
	}
	sink := NewOpsSystemLogSink(repo)
	logger.SetSink(sink)
	t.Cleanup(func() { logger.SetSink(nil) })

	EmitAdminAudit(context.Background(), AdminAuditEvent{
		Action:     "settings.update",
		TargetType: "system_settings",
		TargetID:   "global",
		Before: map[string]any{
			"oidc_connect_client_secret": "old-secret",
			"nested": map[string]any{
				"payment_merchant_key": "old-merchant-key",
			},
		},
		After: map[string]any{
			"oidc_connect_client_secret": "new-secret",
			"nested": map[string]any{
				"payment_merchant_key": "new-merchant-key",
			},
		},
	})

	inserted, err := sink.flushBatch(context.Background(), drainOpsSystemLogSinkQueue(sink))
	if err != nil {
		t.Fatalf("flushBatch() error: %v", err)
	}
	if inserted != 1 || len(captured) != 1 {
		t.Fatalf("captured=%d inserted=%d, want 1", len(captured), inserted)
	}
	for _, leaked := range []string{"old-secret", "new-secret", "old-merchant-key", "new-merchant-key"} {
		if strings.Contains(captured[0].ExtraJSON, leaked) {
			t.Fatalf("sensitive value %q leaked: %s", leaked, captured[0].ExtraJSON)
		}
	}
	if !strings.Contains(captured[0].ExtraJSON, "***") {
		t.Fatalf("redacted marker missing: %s", captured[0].ExtraJSON)
	}
}

func TestNormalizeAdminAuditResultPreservesAttempt(t *testing.T) {
	if got := normalizeAdminAuditResult("attempt"); got != "attempt" {
		t.Fatalf("normalizeAdminAuditResult(attempt) = %q, want attempt", got)
	}
}

func drainOpsSystemLogSinkQueue(sink *OpsSystemLogSink) []*logger.LogEvent {
	var out []*logger.LogEvent
	for {
		select {
		case event := <-sink.queue:
			out = append(out, event)
		default:
			return out
		}
	}
}
