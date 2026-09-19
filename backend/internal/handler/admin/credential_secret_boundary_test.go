package admin

import (
	"bytes"
	"context"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type panickingCredentialVerifier struct{}

func (panickingCredentialVerifier) Verify(context.Context, service.CredentialSecret) (service.VerifiedCredential, error) {
	panic("opaque-token-canary-39c1")
}
func TestCredentialImportGinPanicLogSecretScan(t *testing.T) {
	var logs bytes.Buffer
	previousGin, previousSlog := gin.DefaultErrorWriter, slog.Default()
	gin.DefaultErrorWriter = &logs
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { gin.DefaultErrorWriter = previousGin; slog.SetDefault(previousSlog) })
	vault, err := service.NewCredentialVault(strings.Repeat("ab", 32))
	require.NoError(t, err)
	h := NewCredentialImportHandler(service.NewCredentialImportService(handlerCredentialImportStore{}, vault, panickingCredentialVerifier{}), nil, nil, nil)
	router := gin.New()
	router.Use(middleware.Recovery())
	router.POST("/api/v1/admin/credential-imports", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
		h.Import(c)
	})
	req := httptest.NewRequest("POST", "/api/v1/admin/credential-imports", strings.NewReader(`{"access_token":"opaque-token-canary-39c1","refresh_token":"opaque-refresh-canary-39c1"}`))
	req.Header.Set("Idempotency-Key", "operation")
	req.Header.Set("Authorization", "Bearer opaque-admin-canary-39c1")
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	require.Equal(t, 409, response.Code)
	require.Contains(t, logs.String(), "credential_import_panic")
	for _, canary := range []string{"opaque-token-canary-39c1", "opaque-refresh-canary-39c1", "opaque-admin-canary-39c1"} {
		require.NotContains(t, logs.String()+response.Body.String(), canary)
	}
}
