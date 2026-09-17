//go:build unit

package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

type openAIPrivacyRefreshClient struct{ err error }

func (c *openAIPrivacyRefreshClient) ExchangeCode(context.Context, string, string, string, string, string) (*openai.TokenResponse, error) {
	return nil, errors.New("unexpected authorization")
}
func (c *openAIPrivacyRefreshClient) RefreshToken(ctx context.Context, token, proxy string) (*openai.TokenResponse, error) {
	return c.RefreshTokenWithClientID(ctx, token, proxy, "")
}
func (c *openAIPrivacyRefreshClient) RefreshTokenWithClientID(context.Context, string, string, string) (*openai.TokenResponse, error) {
	if c.err != nil {
		return nil, c.err
	}
	return &openai.TokenResponse{AccessToken: "new-token", RefreshToken: "new-refresh", ExpiresIn: 3600}, nil
}

type openAIPrivacyRefreshAdmin struct {
	*stubAdminService
	privacyCalls atomic.Int32
}

func (s *openAIPrivacyRefreshAdmin) ForceOpenAIPrivacy(context.Context, *service.Account) string {
	s.privacyCalls.Add(1)
	return ""
}

func TestRefreshSingleAccountOpenAIDoesNotModifyPrivacy(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "success"
		client := &openAIPrivacyRefreshClient{}
		if fail {
			name = "failure"
			client.err = errors.New("refresh failed")
		}
		t.Run(name, func(t *testing.T) {
			oauth := service.NewOpenAIOAuthService(nil, client)
			defer oauth.Stop()
			paths := []string{}
			oauth.SetPrivacyClientFactory(func(string) (*req.Client, error) {
				return req.C().WrapRoundTripFunc(func(req.RoundTripper) req.RoundTripFunc {
					return func(r *req.Request) (*req.Response, error) {
						paths = append(paths, r.URL.Path)
						return nil, errors.New("account information unavailable")
					}
				}), nil
			})
			admin := &openAIPrivacyRefreshAdmin{stubAdminService: newStubAdminService()}
			handler := &AccountHandler{adminService: admin, openaiOAuthService: oauth}
			account := &service.Account{ID: 42, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
				Credentials: map[string]any{"access_token": "old-token", "refresh_token": "rt"},
				Extra:       map[string]any{"privacy_mode": service.PrivacyModeCFBlocked}}
			_, _, err := handler.refreshSingleAccount(context.Background(), account)
			if fail {
				require.ErrorIs(t, err, client.err)
				require.Empty(t, paths)
				require.Zero(t, admin.updateAccountCalls)
			} else {
				require.NoError(t, err)
				require.Equal(t, []string{"/backend-api/accounts/check/v4-2023-04-27"}, paths)
				require.Equal(t, "new-token", admin.lastUpdateAccountInput.Credentials["access_token"])
				require.NotContains(t, admin.lastUpdateAccountInput.Extra, "privacy_mode")
			}
			require.Equal(t, service.PrivacyModeCFBlocked, account.Extra["privacy_mode"])
			require.Zero(t, admin.privacyCalls.Load())
		})
	}
}

func (s *openAIPrivacyRefreshAdmin) CreateAccount(_ context.Context, input *service.CreateAccountInput) (*service.Account, error) {
	return &service.Account{ID: 42, Name: input.Name, Platform: input.Platform, Type: input.Type, Credentials: input.Credentials, Extra: input.Extra}, nil
}

func TestCreateOpenAIAccountsDoesNotModifyPrivacy(t *testing.T) {
	for _, batch := range []bool{false, true} {
		name := "single"
		if batch {
			name = "batch"
		}
		t.Run(name, func(t *testing.T) {
			admin := &openAIPrivacyRefreshAdmin{stubAdminService: newStubAdminService()}
			handler := &AccountHandler{adminService: admin}
			router := gin.New()
			body := `{"name":"oauth-account","platform":"openai","type":"oauth","credentials":{"access_token":"at","refresh_token":"rt"}}`
			if batch {
				router.POST("/accounts", handler.BatchCreate)
				body = `{"accounts":[` + body + `]}`
			} else {
				router.POST("/accounts", handler.Create)
			}
			request := httptest.NewRequest(http.MethodPost, "/accounts", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			require.Never(t, func() bool { return admin.privacyCalls.Load() != 0 }, 100*time.Millisecond, 10*time.Millisecond)
		})
	}
}
