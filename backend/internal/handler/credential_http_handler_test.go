package handler

import (
	"context"
	"errors"
	"go.uber.org/zap"
	"net/http/httptest"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type credentialHandlerRouteStub struct {
	service.CredentialRouteStore
	candidates []service.CredentialRouteCandidate
	mixed      bool
	err        error
}

func (s credentialHandlerRouteStub) CredentialRoutes(context.Context, int64) ([]service.CredentialRouteCandidate, bool, error) {
	return s.candidates, s.mixed, s.err
}
func TestCredentialHTTPHandlerFailClosedBeforeLegacySlots(t *testing.T) {
	group := int64(1)
	key := &service.APIKey{ID: 1, GroupID: &group}
	for _, tt := range []struct {
		name    string
		enabled bool
		routes  credentialHandlerRouteStub
		code    string
	}{
		{"store down", true, credentialHandlerRouteStub{err: errors.New("unavailable")}, "ADMISSION_STORE_UNAVAILABLE"},

		{"mixed", true, credentialHandlerRouteStub{candidates: []service.CredentialRouteCandidate{{PrincipalID: 1}}, mixed: true}, "GROUPED_ROUTE_MIXED_UNSUPPORTED"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			// Nil legacy concurrency helper ensures no Redis slot is touched on rejection.
			h := &OpenAIGatewayHandler{credentialHTTP: &service.CredentialHTTPRuntime{Enabled: tt.enabled, Routes: tt.routes}}
			handled := h.tryCredentialHTTP(c, key, middleware2.AuthSubject{UserID: 1}, nil, []byte(`{}`), []byte(`{}`), []byte(`{}`), "mock", false, zap.NewNop(), false)
			require.True(t, handled)
			require.Equal(t, 503, w.Code)
			require.Contains(t, w.Body.String(), tt.code)
		})
	}
}
