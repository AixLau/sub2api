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

type accountSettingsPrincipalReader struct {
	principalReaderStub
	view service.PrincipalView
}

func (r *accountSettingsPrincipalReader) AccountPrincipals(context.Context, []int64) ([]service.PrincipalView, error) {
	return []service.PrincipalView{r.view}, nil
}

type accountSettingsAdminStub struct {
	*stubAdminService
	reader *accountSettingsPrincipalReader
	input  *service.UpdateAccountInput
}

func (s *accountSettingsAdminStub) UpdateAccount(_ context.Context, id int64, input *service.UpdateAccountInput) (*service.Account, error) {
	s.input = input
	s.reader.view.ConfigVersion++
	s.reader.view.Name = input.Name
	return &service.Account{ID: id, Name: input.Name, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: "inactive"}, nil
}

func TestAccountUpdateControlledSettingsRequireVersionAndReturnPrincipal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reader := &accountSettingsPrincipalReader{view: service.PrincipalView{ID: 2, AccountID: 7, ConfigVersion: 4, AdminState: "ACTIVE", RequestedLimit: 9}}
	svc := &accountSettingsAdminStub{reader: reader}
	handler := &AccountHandler{adminService: svc, principals: &UpstreamPrincipalHandler{reader: reader, enabled: true}}
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1}) })
	router.PUT("/accounts/:id", handler.UpdateWithCredentialStepUp(func(*gin.Context) {}))
	request := httptest.NewRequest("PUT", "/accounts/7", strings.NewReader(`{"name":"edited","concurrency":9}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	require.Equal(t, 428, recorder.Code)
	require.Nil(t, svc.input)

	request = httptest.NewRequest("PUT", "/accounts/7", strings.NewReader(`{"name":"edited","concurrency":9}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", `"v4"`)
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	require.Equal(t, 200, recorder.Code, recorder.Body.String())
	require.Equal(t, &service.CredentialAccountEdit{ActorID: 1, PrincipalID: 2, ConfigVersion: 4}, svc.input.CredentialEdit)
	require.Empty(t, svc.input.Status, "omitted status must preserve principal pause/drain state")
	require.Equal(t, `"v5"`, recorder.Header().Get("ETag"))
	require.Contains(t, recorder.Body.String(), `"config_version":5`)
	require.Contains(t, recorder.Body.String(), `"concurrency":9`)
	require.Contains(t, recorder.Body.String(), `"status":"active"`)
}

func TestAccountUpdateControlledSettingsEnforcesStepUpWithoutVersion(t *testing.T) {
	reader := &accountSettingsPrincipalReader{view: service.PrincipalView{ID: 2, AccountID: 7}}
	svc := &accountSettingsAdminStub{reader: reader}
	handler := &AccountHandler{adminService: svc, principals: &UpstreamPrincipalHandler{reader: reader}}
	for _, version := range []string{"", `"v1"`} {
		t.Run(version, func(t *testing.T) {
			router := gin.New()
			checked := false
			router.PUT("/accounts/:id", handler.UpdateWithCredentialStepUp(func(c *gin.Context) {
				checked = true
				c.AbortWithStatus(403)
			}))
			request := httptest.NewRequest("PUT", "/accounts/7", strings.NewReader(`{"name":"edited"}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("If-Match", version)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			require.Equal(t, 403, recorder.Code)
			require.True(t, checked)
			require.Nil(t, svc.input)
		})
	}
}
