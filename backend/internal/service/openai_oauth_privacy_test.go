//go:build unit

package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

type readOnlyOAuthClient struct{ openaiOAuthClientStateStub }

func (c *readOnlyOAuthClient) ExchangeCode(ctx context.Context, _, _, _, _, _ string) (*openai.TokenResponse, error) {
	return c.RefreshTokenWithClientID(ctx, "", "", "")
}

func (c *readOnlyOAuthClient) RefreshTokenWithClientID(context.Context, string, string, string) (*openai.TokenResponse, error) {
	idToken := "e30." + base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"personal"}}`)) + ".signature"
	return &openai.TokenResponse{AccessToken: "at", RefreshToken: "rt", IDToken: idToken, ExpiresIn: 3600}, nil
}

// Redirect every backend request, including the constant settings URL, to a local
// server so an unexpected privacy write fails the test and never reaches ChatGPT.
func TestOpenAIOAuthFlowsDoNotModifyPrivacy(t *testing.T) {
	for _, flow := range []string{"authorization", "refresh", "account_refresh", "enrichment", "background_refresh"} {
		for _, mode := range []string{"", PrivacyModeFailed, PrivacyModeCFBlocked, PrivacyModeTrainingOff} {
			t.Run(flow+"/"+mode, func(t *testing.T) {
				var checks, subscriptions, unexpected atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					if r.Method != http.MethodGet {
						unexpected.Add(1)
					}
					switch r.URL.Path {
					case "/backend-api/accounts/check/v4-2023-04-27":
						checks.Add(1)
						_, _ = w.Write([]byte(`{"accounts":{"personal":{"account":{"account_id":"personal","plan_type":"plus","is_default":true}}}}`))
					case "/backend-api/subscriptions":
						subscriptions.Add(1)
						_, _ = w.Write([]byte(`{"active_until":"2027-01-01T00:00:00Z","plan_type":"plus"}`))
					default:
						unexpected.Add(1)
						_, _ = w.Write([]byte(`{}`))
					}
				}))
				defer server.Close()
				svc := NewOpenAIOAuthService(nil, &readOnlyOAuthClient{})
				defer svc.Stop()
				svc.SetPrivacyClientFactory(newQuotaRedirectingFactory(server))
				account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
					Credentials: map[string]any{"access_token": "old", "refresh_token": "rt", "chatgpt_account_id": "personal"}, Extra: map[string]any{"unrelated": true}}
				if mode != "" {
					account.Extra["privacy_mode"] = mode
				}
				before := shallowCopyMap(account.Extra)
				var info *OpenAITokenInfo
				var err error
				switch flow {
				case "authorization":
					svc.sessionStore.Set("sid", &openai.OAuthSession{State: "state", CodeVerifier: "verifier", CreatedAt: time.Now()})
					info, err = svc.ExchangeCode(context.Background(), &OpenAIExchangeCodeInput{SessionID: "sid", State: "state", Code: "code"})
				case "refresh":
					info, err = svc.RefreshToken(context.Background(), "rt", "")
				case "account_refresh":
					info, err = svc.RefreshAccountToken(context.Background(), account)
				case "enrichment":
					delete(account.Credentials, "refresh_token")
					info, err = svc.RefreshAccountToken(context.Background(), account)
				case "background_refresh":
					repo := &tokenRefreshAccountRepo{}
					refresh := NewTokenRefreshService(repo, nil, svc, nil, nil, nil, nil, &config.Config{TokenRefresh: config.TokenRefreshConfig{MaxRetries: 1}}, nil)
					refresher := NewOpenAITokenRefresher(svc, repo)
					err = refresh.refreshWithRetry(context.Background(), account, refresher, nil, time.Hour)
					require.Equal(t, 1, repo.updateCredentialsCalls)
					require.Zero(t, repo.updateExtraCalls)
					require.Equal(t, "at", account.GetCredential("access_token"))
					require.Equal(t, "plus", account.GetCredential("plan_type"))
				}
				require.NoError(t, err)
				if info != nil {
					require.Equal(t, "plus", info.PlanType)
					require.Equal(t, "2027-01-01T00:00:00Z", info.SubscriptionExpiresAt)
					body, marshalErr := json.Marshal(info)
					require.NoError(t, marshalErr)
					require.NotContains(t, string(body), "privacy_mode")
				}
				require.Equal(t, before, account.Extra)
				require.EqualValues(t, 1, checks.Load())
				require.EqualValues(t, 1, subscriptions.Load())
				require.Zero(t, unexpected.Load(), "OAuth flows must only query account and subscription information")
			})
		}
	}
}
