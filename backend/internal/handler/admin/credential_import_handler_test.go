package admin

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type handlerCredentialImportStore struct{}

func (handlerCredentialImportStore) PutImport(_ context.Context, r service.CredentialImportRecord) (service.CredentialImportView, error) {
	return service.CredentialImportView{ID: r.ID, State: r.State}, nil
}
func (handlerCredentialImportStore) GetImport(context.Context, int64, int64, string) (*service.CredentialImportView, error) {
	return nil, service.ErrCredentialNotFound
}
func TestCredentialImportHandlerDoesNotEchoSecrets(t *testing.T) {
	vault, err := service.NewCredentialVault(strings.Repeat("ab", 32))
	require.NoError(t, err)
	svc := service.NewCredentialImportService(handlerCredentialImportStore{}, vault, nil)
	h := NewCredentialImportHandler(svc, nil, nil)
	router := gin.New()
	router.POST("/imports", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
		h.Import(c)
	})
	req := httptest.NewRequest("POST", "/imports", strings.NewReader(`{"access_token":"mock-secret-access","refresh_token":"mock-secret-refresh"}`))
	req.Header.Set("Idempotency-Key", "fixture")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, 202, w.Code)
	require.Contains(t, w.Body.String(), "UNVERIFIED")
	require.NotContains(t, w.Body.String(), "mock-secret")
	require.NotContains(t, w.Body.String(), "fingerprint")
}
