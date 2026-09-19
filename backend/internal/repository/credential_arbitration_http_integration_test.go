//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type arbitrationHTTPProvider struct {
	service.OpenAIOAuthClient
	url string
}

func (p arbitrationHTTPProvider) RefreshTokenWithClientID(ctx context.Context, token, proxy, client string) (*openai.TokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, strings.NewReader(token))
	if err != nil {
		return nil, err
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	var result openai.TokenResponse
	err = json.NewDecoder(response.Body).Decode(&result)
	return &result, err
}
func TestCredentialOperationArbitrationRawHTTPOutsideTransaction(t *testing.T) {
	f := newOperationArbitrationFixture(t)
	ctx := context.Background()
	secret := arbitrationSecret()
	account := arbitrationAccount(secret)
	require.NoError(t, f.repo.Create(ctx, account))
	entered, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	var calls atomic.Int64
	latest := arbitrationSecret()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		close(entered)
		<-release
		_ = json.NewEncoder(w).Encode(openai.TokenResponse{AccessToken: latest.AccessToken, RefreshToken: latest.RefreshToken, ExpiresIn: 3600})
	}))
	defer upstream.Close()
	cfg := &config.Config{}
	cfg.Gateway.CredentialVaultKey = strings.Repeat("ab", 32)
	gateway := service.ProvideOpenAIOAuthService(f.repo, cfg, nil, arbitrationHTTPProvider{url: upstream.URL}, nil)
	type outcome struct {
		result *service.OpenAITokenInfo
		err    error
	}
	done := make(chan outcome, 1)
	go func() { v, e := gateway.RefreshToken(ctx, secret.RefreshToken, ""); done <- outcome{v, e} }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("mock provider was not called")
	}
	// Real transaction obtains the ownership barrier while HTTP is blocked.
	short, cancel := context.WithTimeout(ctx, time.Second)
	tx, err := f.db.BeginTx(short, nil)
	require.NoError(t, err)
	_, err = lockCredentialArbitration(short, tx)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())
	cancel()
	_, err = f.imports.Import(ctx, f.actor, uuid.NewString(), secret)
	require.ErrorIs(t, err, service.ErrCredentialLegacyBypass)
	_, err = gateway.RefreshToken(ctx, secret.RefreshToken, "")
	require.Error(t, err)
	require.Equal(t, int64(1), calls.Load())
	close(release)
	completed := <-done
	require.NoError(t, completed.err)
	require.Equal(t, latest.AccessToken, completed.result.AccessToken)
	var count int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM credential_legacy_refresh_operations WHERE state='SUCCEEDED' AND octet_length(result_ciphertext)>0`).Scan(&count))
	require.Equal(t, 1, count)
	replay, err := gateway.RefreshToken(ctx, secret.RefreshToken, "")
	require.NoError(t, err)
	require.Equal(t, completed.result.ExpiresAt, replay.ExpiresAt)
	require.Equal(t, int64(1), calls.Load())
	// Stale pre-refresh account documents cannot restore consumed credentials.
	require.ErrorIs(t, f.repo.UpdateCredentials(ctx, account.ID, arbitrationAccount(secret).Credentials), service.ErrCredentialLegacyBypass)
	require.NoError(t, f.repo.UpdateCredentials(ctx, account.ID, arbitrationAccount(latest).Credentials))
	staged, err := f.imports.Import(ctx, f.actor, uuid.NewString(), latest)
	require.NoError(t, err)
	_, err = f.createPrincipal(ctx, staged)
	require.ErrorIs(t, err, service.ErrCredentialLegacyBypass)
}
func TestCredentialOperationArbitrationDefaultOffKeyAndBootstrap(t *testing.T) {
	f := newOperationArbitrationFixture(t)
	ctx := context.Background()
	cfg := &config.Config{} // still false; omitting the stable key after init cannot bypass claims
	_, err := configuredCredentialAccountRepository(f.client, f.db, nil, cfg)
	require.ErrorIs(t, err, service.ErrCredentialVaultUnavailable)
	wrong, _ := service.NewCredentialVault(strings.Repeat("cd", 32))
	require.Error(t, configureCredentialArbitration(ctx, f.db, wrong))
	require.NoError(t, configureCredentialArbitration(ctx, f.db, f.vault))
}

func TestCredentialOperationArbitrationLegacyReceiptKeyRotation(t *testing.T) {
	f := newOperationArbitrationFixture(t)
	ctx := context.Background()
	input := arbitrationSecret()
	op, err := f.repo.BeginLegacyCredentialRefresh(ctx, []string{f.vault.Fingerprint("token", input.RefreshToken)}, "fixture")
	require.NoError(t, err)
	latest := arbitrationSecret()
	plain := []byte(`{"new_result":"opaque-fixture-fact"}`)
	cipher, err := f.vault.SealData(op.ID, plain)
	require.NoError(t, err)
	require.NoError(t, f.repo.FinishLegacyCredentialRefresh(ctx, op, f.fingerprints(latest), cipher, false))
	next, err := service.NewCredentialVaultWithFingerprintKey(strings.Repeat("cd", 32), f.vault.FingerprintKeyHex())
	require.NoError(t, err)
	rotation := NewCredentialVaultRotation(f.db, &vaultRotationFence{})
	counts, err := rotation.Rotate(ctx, f.actor, uuid.NewString(), f.vault, next)
	require.NoError(t, err)
	require.Equal(t, int64(1), counts["credential_legacy_refresh_operations"])
	var after []byte
	var state string
	require.NoError(t, f.db.QueryRow(`SELECT result_ciphertext,state FROM credential_legacy_refresh_operations WHERE id=$1`, op.ID).Scan(&after, &state))
	require.Equal(t, "UNKNOWN", state)
	opened, err := next.OpenData(op.ID, after)
	require.NoError(t, err)
	require.Equal(t, plain, opened)
	require.Equal(t, f.vault.Fingerprint("token", input.RefreshToken), next.Fingerprint("token", input.RefreshToken))
	cfg := &config.Config{}
	cfg.Gateway.CredentialVaultKey = strings.Repeat("cd", 32)
	cfg.Gateway.CredentialFingerprintKey = f.vault.FingerprintKeyHex()
	restarted, err := configuredCredentialAccountRepository(f.client, f.db, nil, cfg)
	require.NoError(t, err)
	_, err = restarted.BeginLegacyCredentialRefresh(ctx, []string{next.Fingerprint("token", input.RefreshToken)}, "fixture")
	require.Error(t, err)
	require.NoError(t, rotationTestReverse(ctx, rotation, f.actor, next, f.vault))
	require.NoError(t, f.db.QueryRow(`SELECT result_ciphertext FROM credential_legacy_refresh_operations WHERE id=$1`, op.ID).Scan(&after))
	opened, err = f.vault.OpenData(op.ID, after)
	require.NoError(t, err)
	require.Equal(t, plain, opened)
}
func rotationTestReverse(ctx context.Context, r *CredentialVaultRotation, actor int64, old, next *service.CredentialVault) error {
	_, err := r.Rotate(ctx, actor, uuid.NewString(), old, next)
	return err
}

func TestCredentialOperationArbitrationDeletedAccountRetainsResult(t *testing.T) {
	f := newOperationArbitrationFixture(t)
	ctx := context.Background()
	old := arbitrationSecret()
	account := arbitrationAccount(old)
	require.NoError(t, f.repo.Create(ctx, account))
	op, err := f.repo.BeginLegacyCredentialRefresh(ctx, []string{f.vault.Fingerprint("token", old.RefreshToken)}, "fixture")
	require.NoError(t, err)
	_, err = f.db.Exec(`UPDATE accounts SET deleted_at=clock_timestamp() WHERE id=$1`, account.ID)
	require.NoError(t, err)
	returned := arbitrationSecret()
	cipher, err := f.vault.SealData(op.ID, []byte(`{"returned":"newest-fixture"}`))
	require.NoError(t, err)
	require.ErrorIs(t, f.repo.FinishLegacyCredentialRefresh(ctx, op, f.fingerprints(returned), cipher, true), service.ErrCredentialLegacyBypass)
	// Repeated compensation must retain the same encrypted result, not abandon it.
	require.ErrorIs(t, f.repo.FinishLegacyCredentialRefresh(ctx, op, f.fingerprints(returned), cipher, false), service.ErrCredentialLegacyBypass)
	var state string
	var saved []byte
	var count int
	require.NoError(t, f.db.QueryRow(`SELECT state,result_ciphertext FROM credential_legacy_refresh_operations WHERE id=$1`, op.ID).Scan(&state, &saved))
	require.Equal(t, "UNKNOWN", state)
	require.Equal(t, cipher, saved)
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM credential_legacy_refresh_aliases WHERE operation_id=$1 AND is_output`, op.ID).Scan(&count))
	require.Equal(t, 2, count)
	_, err = f.imports.Import(ctx, f.actor, uuid.NewString(), returned)
	require.ErrorIs(t, err, service.ErrCredentialLegacyBypass)
}
