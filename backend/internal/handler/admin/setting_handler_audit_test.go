package admin

import (
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type captureLogSink struct {
	events []*logger.LogEvent
}

func (s *captureLogSink) WriteLogEvent(event *logger.LogEvent) {
	s.events = append(s.events, event)
}

func TestSettingHandlerAuditSettingsUpdate_EmitsAdminAuditEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink := &captureLogSink{}
	logger.SetSink(sink)
	t.Cleanup(func() { logger.SetSink(nil) })

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("PUT", "/api/v1/admin/settings", nil)
	c.Request.RemoteAddr = "192.0.2.8:12345"
	c.Request.Header.Set("User-Agent", "audit-test")
	c.Request.Header.Set("X-Request-ID", "req-1")
	c.Request.Header.Set("X-Client-Request-ID", "creq-1")
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 77})
	c.Set(string(middleware.ContextKeyUserRole), "admin")

	handler := &SettingHandler{}
	handler.auditSettingsUpdate(
		c,
		&service.SystemSettings{RegistrationEnabled: false},
		&service.SystemSettings{RegistrationEnabled: true},
		nil,
		nil,
		UpdateSettingsRequest{},
	)

	if len(sink.events) != 1 {
		t.Fatalf("captured events = %d, want 1", len(sink.events))
	}
	event := sink.events[0]
	if event.Level != "info" {
		t.Fatalf("level = %q, want info", event.Level)
	}
	if event.Component != "admin.audit" {
		t.Fatalf("component = %q, want admin.audit", event.Component)
	}
	if event.Message != "admin audit event" {
		t.Fatalf("message = %q, want admin audit event", event.Message)
	}
	if event.Fields["audit"] != true || event.Fields["admin_audit"] != true {
		t.Fatalf("audit markers missing: %+v", event.Fields)
	}
	if event.Fields["action"] != "settings.update" {
		t.Fatalf("action = %v, want settings.update", event.Fields["action"])
	}
	if event.Fields["target_type"] != "system_settings" || event.Fields["target_id"] != "global" {
		t.Fatalf("target fields invalid: %+v", event.Fields)
	}
	if event.Fields["user_id"] != int64(77) || event.Fields["operator_role"] != "admin" {
		t.Fatalf("operator fields invalid: %+v", event.Fields)
	}
	if event.Fields["request_id"] != "req-1" || event.Fields["client_request_id"] != "creq-1" {
		t.Fatalf("request fields invalid: %+v", event.Fields)
	}
	before, ok := event.Fields["before"].(map[string]any)
	if !ok {
		t.Fatalf("before field missing or invalid: %+v", event.Fields["before"])
	}
	after, ok := event.Fields["after"].(map[string]any)
	if !ok {
		t.Fatalf("after field missing or invalid: %+v", event.Fields["after"])
	}
	if before["registration_enabled"] != false || after["registration_enabled"] != true {
		t.Fatalf("before/after diff invalid: before=%+v after=%+v", before, after)
	}
	if before["changed"] != nil || after["changed"] != nil {
		t.Fatalf("before/after should contain field values, not only changed list: before=%+v after=%+v", before, after)
	}
}

func TestSettingsAuditDiffMaps_UsesCanonicalSettingFieldNamesForAcronyms(t *testing.T) {
	before, after := settingsAuditDiffMaps(
		[]string{"api_base_url", "oidc_connect_client_id"},
		&service.SystemSettings{
			APIBaseURL:          "https://old.example.com",
			OIDCConnectClientID: "old-client",
		},
		&service.SystemSettings{
			APIBaseURL:          "https://new.example.com",
			OIDCConnectClientID: "new-client",
		},
		nil,
		nil,
	)

	if before["api_base_url"] != "https://old.example.com" || after["api_base_url"] != "https://new.example.com" {
		t.Fatalf("api_base_url diff invalid: before=%+v after=%+v", before, after)
	}
	if before["oidc_connect_client_id"] != "old-client" || after["oidc_connect_client_id"] != "new-client" {
		t.Fatalf("oidc_connect_client_id diff invalid: before=%+v after=%+v", before, after)
	}
}
