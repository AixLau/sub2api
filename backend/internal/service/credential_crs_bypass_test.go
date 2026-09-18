package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type credentialCRSRepo struct {
	AccountRepository
	legacyCredentialRegistry
	creates, updates, lookups int
	existing                  *Account
}

func (r *credentialCRSRepo) GetByCRSAccountID(context.Context, string) (*Account, error) {
	r.lookups++
	return r.existing, nil
}
func (r *credentialCRSRepo) Create(context.Context, *Account) error { r.creates++; return nil }
func (r *credentialCRSRepo) Update(context.Context, *Account) error { r.updates++; return nil }
func TestCredentialCRSSyncRejectsKnownTokensBeforeCreateOrUpdate(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.CredentialVaultKey = strings.Repeat("ab", 32)
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	vault, err := NewCredentialVault(cfg.Gateway.CredentialVaultKey)
	require.NoError(t, err)
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "create", true: "update"}[existing], func(t *testing.T) {
			repo := &credentialCRSRepo{legacyCredentialRegistry: legacyCredentialRegistry{known: map[string]bool{vault.Fingerprint("token", "known-refresh"): true}}}
			if existing {
				repo.existing = &Account{ID: 1, Credentials: map[string]any{"access_token": "existing"}}
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/web/auth/login" {
					_, _ = w.Write([]byte(`{"success":true,"token":"mock-admin"}`))
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"openaiOAuthAccounts": []any{map[string]any{"id": "existing-crs-id", "name": "fixture", "isActive": true, "schedulable": true, "credentials": map[string]any{"access_token": "known-access", "refresh_token": "known-refresh"}}}}})
			}))
			defer upstream.Close()
			syncer := NewCRSSyncService(repo, nil, nil, nil, nil, cfg)
			result, err := syncer.SyncFromCRS(context.Background(), SyncFromCRSInput{BaseURL: upstream.URL, Username: "mock", Password: "mock"})
			require.NoError(t, err)
			require.Equal(t, 1, result.Failed)
			require.Zero(t, result.Created)
			require.Zero(t, result.Updated)
			require.Equal(t, "CONTROLLED_CREDENTIAL_REQUIRES_PRINCIPAL_API", result.Items[0].Error)
			require.Zero(t, repo.creates)
			require.Zero(t, repo.updates)
			require.Zero(t, repo.lookups)
			if existing {
				require.Equal(t, "existing", repo.existing.Credentials["access_token"])
			}
		})
	}
}
