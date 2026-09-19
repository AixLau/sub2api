package service

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

func TestOpenAICredentialVerifiedIdentity(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	for _, scenario := range []string{"valid", "issuer", "audience", "expiry", "identity", "forged"} {
		t.Run(scenario, func(t *testing.T) {
			claims := jwt.MapClaims{"iss": "https://auth.openai.com", "aud": "https://api.openai.com/v1", "exp": time.Now().Add(time.Hour).Unix(), "https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "verified-account", "chatgpt_user_id": "verified-user"}}
			switch scenario {
			case "issuer":
				claims["iss"] = "https://untrusted.test"
			case "audience":
				claims["aud"] = "unrelated-api"
			case "expiry":
				claims["exp"] = time.Now().Add(-time.Minute).Unix()
			case "identity":
				delete(claims, "https://api.openai.com/auth")
			}
			token, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(key)
			require.NoError(t, err)
			verificationKey := key
			if scenario == "forged" {
				verificationKey, err = rsa.GenerateKey(rand.Reader, 2048)
				require.NoError(t, err)
			}
			got, err := verifyOpenAICredential(CredentialSecret{AccessToken: token, AccountSubject: "caller-forged-account", UserSubject: "caller-forged-user"}, func(*jwt.Token) (any, error) { return &verificationKey.PublicKey, nil })
			require.NoError(t, err)
			if scenario == "valid" {
				require.Equal(t, "VERIFIED", got.State)
				require.Equal(t, "verified-account", got.AccountSubject)
				require.Equal(t, "verified-user", got.UserSubject)
			} else {
				require.Equal(t, "UNVERIFIED", got.State)
				require.Empty(t, got.AccountSubject)
			}
		})
	}
}
