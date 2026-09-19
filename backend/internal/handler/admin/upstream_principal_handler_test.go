package admin

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type principalReaderStub struct {
	scope int64
	err   error
}

func (r *principalReaderStub) AccountPrincipals(context.Context, []int64) ([]service.PrincipalView, error) {
	return nil, r.err
}

func (r *principalReaderStub) ListPrincipals(_ context.Context, scope, after int64, limit int) ([]service.PrincipalView, error) {
	r.scope = scope
	return []service.PrincipalView{}, r.err
}
func (r *principalReaderStub) GetPrincipal(_ context.Context, scope, id int64) (*service.PrincipalView, error) {
	r.scope = scope
	return &service.PrincipalView{ID: id, RequestedLimit: 5, Occupied: 8}, r.err
}

func TestUpstreamPrincipalReadBoundary(t *testing.T) {
	repo := &principalReaderStub{}
	h := NewUpstreamPrincipalHandler(repo, &config.Config{})
	router := gin.New()
	router.GET("/principals", h.List)
	router.GET("/principals/:id", h.Get)
	for _, tt := range []struct {
		path     string
		status   int
		contains string
	}{
		{"/principals?tenant_id=2", 200, `"http_enabled":false`},
		{"/principals/1?tenant_id=2", 200, `"overhang":3`},
		{"/principals?after=bad", 400, "INVALID_CURSOR"},
		{"/principals/0", 400, "INVALID_ID"},
	} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", tt.path, nil))
		require.Equal(t, tt.status, w.Code)
		require.Contains(t, w.Body.String(), tt.contains)
		require.NotContains(t, w.Body.String(), "verified_subject_key")
		require.NotContains(t, w.Body.String(), "installation_id")
	}
	require.Equal(t, service.DeploymentPrincipalScope, repo.scope)
	repo.err = errors.New("secret-database-error")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/principals/1", nil))
	require.Equal(t, 503, w.Code)
	require.NotContains(t, w.Body.String(), "secret-database-error")
}
