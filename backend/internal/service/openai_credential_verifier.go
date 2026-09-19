package service

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
)

// The issuer and key source are fixed provider endpoints. Neither a JWT header
// nor an administrator can choose the key URL. keyfunc handles rotation and
// throttles unknown key IDs; jwt validates signature, issuer, audience and expiry.
type openAICredentialVerifier struct {
	once sync.Once
	keys keyfunc.Keyfunc
}

func (v *openAICredentialVerifier) Verify(ctx context.Context, secret CredentialSecret) (VerifiedCredential, error) {
	v.once.Do(func() {
		tolerateUnavailable := true
		v.keys, _ = keyfunc.NewDefaultOverrideCtx(context.Background(), []string{"https://auth.openai.com/.well-known/jwks.json"}, keyfunc.Override{
			Client:      &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
			HTTPTimeout: 10 * time.Second, NoErrorReturnFirstHTTPReq: &tolerateUnavailable,
			RefreshInterval:         15 * time.Minute,
			RefreshErrorHandlerFunc: func(string) func(context.Context, error) { return func(context.Context, error) {} },
		})
	})
	if ctx.Err() != nil || v.keys == nil {
		return VerifiedCredential{State: "UNVERIFIED"}, nil
	}
	return verifyOpenAICredential(secret, v.keys.Keyfunc)
}

func verifyOpenAICredential(secret CredentialSecret, keys jwt.Keyfunc) (VerifiedCredential, error) {
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(secret.AccessToken, claims, keys,
		jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer("https://auth.openai.com"),
		jwt.WithAudience("app_EMoamEEZ73f0CkXaXp7hrann", "app_LlGpXReQgckcGGUo2JrYvtJK", "https://api.openai.com/v1"), jwt.WithExpirationRequired(), jwt.WithIssuedAt())
	if err != nil || !token.Valid {
		return VerifiedCredential{State: "UNVERIFIED"}, nil
	}
	auth, _ := claims["https://api.openai.com/auth"].(map[string]any)
	account, _ := auth["chatgpt_account_id"].(string)
	user, _ := auth["chatgpt_user_id"].(string)
	expiry, err := claims.GetExpirationTime()
	if err != nil || expiry == nil || account == "" || user == "" {
		return VerifiedCredential{State: "UNVERIFIED"}, nil
	}
	return VerifiedCredential{State: "VERIFIED", Provider: "openai_oauth", AccountSubject: account,
		UserSubject: user, ExpiresAt: expiry.Time, Capabilities: []string{"responses", "passthrough"}}, nil
}
