package admin

import (
	"reflect"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func emitContentModerationAdminAudit(c *gin.Context, action, targetType, targetID, result string, after map[string]any, err error) {
	subject, _ := middleware.GetAuthSubjectFromContext(c)
	role, _ := middleware.GetUserRoleFromContext(c)
	errorCode := ""
	if err != nil {
		errorCode = strings.TrimSpace(infraerrors.Reason(err))
		if errorCode == "" {
			errorCode = "INTERNAL_ERROR"
		}
	}
	service.EmitAdminAudit(c.Request.Context(), service.AdminAuditEvent{
		Action:          action,
		TargetType:      targetType,
		TargetID:        targetID,
		OperatorID:      subject.UserID,
		OperatorRole:    role,
		OperatorIP:      c.ClientIP(),
		UserAgent:       c.Request.UserAgent(),
		RequestID:       c.GetHeader("X-Request-ID"),
		ClientRequestID: c.GetHeader("X-Client-Request-ID"),
		Result:          result,
		ErrorCode:       errorCode,
		After:           after,
	})
}

func contentModerationConfigChangedFields(req contentModerationConfigRequest) []string {
	value := reflect.ValueOf(req)
	typ := value.Type()
	fields := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		field := value.Field(i)
		changed := false
		switch field.Kind() {
		case reflect.Pointer:
			changed = !field.IsNil()
		case reflect.Bool:
			changed = field.Bool()
		case reflect.String:
			changed = strings.TrimSpace(field.String()) != ""
		}
		if !changed {
			continue
		}
		name := strings.TrimSpace(strings.Split(typ.Field(i).Tag.Get("json"), ",")[0])
		if name != "" && name != "-" {
			fields = append(fields, name)
		}
	}
	return fields
}
