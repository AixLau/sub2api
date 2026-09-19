package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/google/uuid"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

type legacyRefreshMemory struct {
	mu                                                                                               sync.Mutex
	op                                                                                               CredentialLegacyRefreshOperation
	input, output                                                                                    []string
	hash                                                                                             string
	begins, finishes                                                                                 int
	beginErr, errorAfterBegin, finishErr, panicBegin, panicFinish, panicCompensation, loseSuccessACK bool
	afterBegin                                                                                       func()
}

func (m *legacyRefreshMemory) KnownCredentialTokenFingerprints(context.Context, []string) (bool, error) {
	return false, nil
}
func (m *legacyRefreshMemory) BeginLegacyCredentialRefresh(_ context.Context, input []string, hash string) (CredentialLegacyRefreshOperation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.begins++
	if m.panicBegin {
		panic("opaque-begin-secret")
	}
	if m.beginErr {
		return CredentialLegacyRefreshOperation{}, errors.New("opaque-store-secret")
	}
	if m.op.ID != "" {
		if m.hash != hash {
			return CredentialLegacyRefreshOperation{}, ErrCredentialLegacyBypass
		}
		if m.op.State != "SUCCEEDED" {
			return CredentialLegacyRefreshOperation{}, ErrLegacyCredentialRefreshUnknown
		}
		op := m.op
		op.Ciphertext = append([]byte(nil), op.Ciphertext...)
		return op, nil
	}
	m.input = append([]string(nil), input...)
	m.hash = hash
	m.op = CredentialLegacyRefreshOperation{ID: uuid.NewString(), OwnerNonce: uuid.NewString(), State: "SENDING"}
	if m.afterBegin != nil {
		m.afterBegin()
	}
	if m.errorAfterBegin {
		return CredentialLegacyRefreshOperation{}, errors.New("opaque-lost-begin-ack")
	}
	return m.op, nil
}
func (m *legacyRefreshMemory) FinishLegacyCredentialRefresh(_ context.Context, op CredentialLegacyRefreshOperation, output []string, cipher []byte, success bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.finishes++
	if success && m.panicFinish {
		panic("opaque-new-token-in-store-panic")
	}
	if !success && m.panicCompensation {
		panic("opaque-compensation-secret")
	}
	if success && m.finishErr {
		return errors.New("opaque-completion-secret")
	}
	if op.ID != m.op.ID || op.OwnerNonce != m.op.OwnerNonce {
		return ErrCredentialLegacyBypass
	}
	if m.op.State == "SUCCEEDED" {
		return nil
	}
	m.output = append([]string(nil), output...)
	m.op.Ciphertext = append([]byte(nil), cipher...)
	m.op.State = "UNKNOWN"
	if success {
		m.op.State = "SUCCEEDED"
		if m.loseSuccessACK {
			return errors.New("opaque-lost-finish-ack")
		}
	}
	return nil
}
func (m *legacyRefreshMemory) snapshot() (CredentialLegacyRefreshOperation, []string, []string, string, int, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	op := m.op
	op.Ciphertext = append([]byte(nil), op.Ciphertext...)
	return op, append([]string(nil), m.input...), append([]string(nil), m.output...), m.hash, m.begins, m.finishes
}

type legacyRefreshClient struct {
	OpenAIOAuthClient
	calls   atomic.Int64
	refresh func(context.Context, string, string, string) (*openai.TokenResponse, error)
}

func (c *legacyRefreshClient) RefreshTokenWithClientID(ctx context.Context, token, proxy, client string) (*openai.TokenResponse, error) {
	c.calls.Add(1)
	return c.refresh(ctx, token, proxy, client)
}
func legacyRefreshFixture(t *testing.T) (*OpenAIOAuthService, *legacyRefreshMemory, *legacyRefreshClient, *CredentialVault) {
	t.Helper()
	vault, err := NewCredentialVault(strings.Repeat("ab", 32))
	require.NoError(t, err)
	store := &legacyRefreshMemory{}
	client := &legacyRefreshClient{refresh: func(context.Context, string, string, string) (*openai.TokenResponse, error) {
		return &openai.TokenResponse{AccessToken: "opaque-new-access", RefreshToken: "opaque-new-refresh", ExpiresIn: 3600, TokenType: "Bearer"}, nil
	}}
	svc := NewOpenAIOAuthService(nil, client)
	t.Cleanup(svc.Stop)
	svc.credentialTokenGuard = &CredentialLegacyTokenGuard{store: store, vault: vault}
	return svc, store, client, vault
}
func TestCredentialLegacyRefreshDurableBeforeEnrichmentAndReplay(t *testing.T) {
	svc, store, client, vault := legacyRefreshFixture(t)
	var checks int
	svc.SetPrivacyClientFactory(func(string) (*req.Client, error) {
		op, _, _, _, _, _ := store.snapshot()
		require.Equal(t, "SUCCEEDED", op.State)
		checks++
		return nil, errors.New("fixture read-only enrichment unavailable")
	})
	first, err := svc.RefreshTokenWithClientID(context.Background(), " old-refresh ", "http://user:password@proxy.test:1234", "client")
	require.NoError(t, err)
	require.Positive(t, checks)
	second, err := svc.RefreshTokenWithClientID(context.Background(), " old-refresh ", "http://user:password@proxy.test:1234", "client")
	require.NoError(t, err)
	require.Equal(t, first.ExpiresAt, second.ExpiresAt)
	require.Equal(t, first.AccessToken, second.AccessToken)
	require.EqualValues(t, 1, client.calls.Load())
	op, input, output, hash, _, _ := store.snapshot()
	require.Equal(t, legacyRefreshFingerprints(vault, " old-refresh "), input)
	require.Equal(t, legacyRefreshFingerprints(vault, "opaque-new-access", "opaque-new-refresh"), output)
	require.NotContains(t, hash, "old-refresh")
	require.NotContains(t, hash, "password")
	require.NotContains(t, string(op.Ciphertext), "opaque-new")
	plain, err := vault.OpenData(op.ID, op.Ciphertext)
	require.NoError(t, err)
	defer clear(plain)
	var receipt credentialLegacyRefreshReceipt
	require.NoError(t, json.Unmarshal(plain, &receipt))
	require.Equal(t, first.ExpiresAt, receipt.ReceivedAt.Unix()+receipt.Response.ExpiresIn)
	_, err = svc.RefreshTokenWithClientID(context.Background(), " old-refresh ", "http://different-proxy.test", "client")
	require.ErrorIs(t, err, ErrCredentialLegacyBypass)
	require.EqualValues(t, 1, client.calls.Load())
}
func TestCredentialLegacyRefreshManualAndWorkerShareOperation(t *testing.T) {
	svc, store, client, _ := legacyRefreshFixture(t)
	account := &Account{ID: 41, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "old-access", "refresh_token": "old-refresh"}}
	first, err := svc.RefreshToken(context.Background(), "old-refresh", "")
	require.NoError(t, err)
	second, err := svc.RefreshAccountToken(context.Background(), account)
	require.NoError(t, err)
	credentials, err := NewOpenAITokenRefresher(svc, nil).Refresh(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, first.AccessToken, second.AccessToken)
	require.Equal(t, first.AccessToken, credentials["access_token"])
	require.EqualValues(t, 1, client.calls.Load())
	op, _, _, _, _, _ := store.snapshot()
	require.Equal(t, "SUCCEEDED", op.State)
}
func TestCredentialLegacyRefreshConcurrentOwnerDoesNotReplay(t *testing.T) {
	svc, store, client, _ := legacyRefreshFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	client.refresh = func(context.Context, string, string, string) (*openai.TokenResponse, error) {
		close(entered)
		<-release
		return &openai.TokenResponse{AccessToken: "new-access", RefreshToken: "new-refresh", ExpiresIn: 3600}, nil
	}
	done := make(chan error, 1)
	go func() { _, err := svc.RefreshToken(context.Background(), "old-refresh", ""); done <- err }()
	<-entered
	_, err := svc.RefreshToken(context.Background(), "old-refresh", "")
	require.ErrorIs(t, err, ErrLegacyCredentialRefreshUnknown)
	op, _, _, _, _, _ := store.snapshot()
	require.Equal(t, "SENDING", op.State)
	close(release)
	require.NoError(t, <-done)
	_, err = svc.RefreshToken(context.Background(), "old-refresh", "")
	require.NoError(t, err)
	require.EqualValues(t, 1, client.calls.Load())
}
func TestCredentialLegacyRefreshUnknownWindowsRetainCipherAndClaims(t *testing.T) {
	for _, window := range []string{"provider_error", "provider_panic", "response_and_error", "finish_error", "finish_panic", "compensation_panic", "lost_success_ack"} {
		t.Run(window, func(t *testing.T) {
			svc, store, client, vault := legacyRefreshFixture(t)
			switch window {
			case "provider_error":
				client.refresh = func(context.Context, string, string, string) (*openai.TokenResponse, error) {
					return nil, errors.New("opaque-provider-token")
				}
			case "provider_panic":
				client.refresh = func(context.Context, string, string, string) (*openai.TokenResponse, error) {
					panic("opaque-provider-token")
				}
			case "response_and_error":
				client.refresh = func(context.Context, string, string, string) (*openai.TokenResponse, error) {
					return &openai.TokenResponse{AccessToken: "opaque-new-access", RefreshToken: "opaque-new-refresh", ExpiresIn: 3600}, errors.New("opaque-provider-token")
				}
			case "finish_error":
				store.finishErr = true
			case "finish_panic":
				store.panicFinish = true
			case "compensation_panic":
				store.finishErr = true
				store.panicCompensation = true
			case "lost_success_ack":
				store.loseSuccessACK = true
			}
			_, err := svc.RefreshToken(context.Background(), "old-refresh", "")
			require.ErrorIs(t, err, ErrLegacyCredentialRefreshUnknown)
			op, inputs, outputs, _, _, _ := store.snapshot()
			require.NotEmpty(t, inputs)
			if window == "lost_success_ack" {
				require.Equal(t, "SUCCEEDED", op.State)
			} else if window == "compensation_panic" {
				require.Equal(t, "SENDING", op.State)
			} else {
				require.Equal(t, "UNKNOWN", op.State)
			}
			if window == "response_and_error" || window == "finish_error" || window == "finish_panic" || window == "lost_success_ack" {
				require.NotEmpty(t, outputs)
				plain, openErr := vault.OpenData(op.ID, op.Ciphertext)
				require.NoError(t, openErr)
				require.Contains(t, string(plain), "opaque-new-refresh")
				clear(plain)
			}
			_, retryErr := svc.RefreshToken(context.Background(), "old-refresh", "")
			if window == "lost_success_ack" {
				require.NoError(t, retryErr)
			} else {
				require.Error(t, retryErr)
			}
			require.EqualValues(t, 1, client.calls.Load())
		})
	}
}
func TestCredentialLegacyRefreshBeginAndConfigurationFailClosed(t *testing.T) {
	for _, window := range []string{"begin_error", "begin_panic", "begin_ack_lost", "cancelled_after_begin", "missing_vault"} {
		t.Run(window, func(t *testing.T) {
			svc, store, client, _ := legacyRefreshFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch window {
			case "begin_error":
				store.beginErr = true
			case "begin_panic":
				store.panicBegin = true
			case "begin_ack_lost":
				store.errorAfterBegin = true
			case "cancelled_after_begin":
				store.afterBegin = cancel
			case "missing_vault":
				svc.credentialTokenGuard.vault = nil
			}
			_, err := svc.RefreshToken(ctx, "old-refresh", "")
			require.Error(t, err)
			require.NotContains(t, err.Error(), "opaque")
			require.Zero(t, client.calls.Load())
			op, _, _, _, _, finishes := store.snapshot()
			if window == "cancelled_after_begin" {
				require.Equal(t, "UNKNOWN", op.State)
				require.Equal(t, 1, finishes)
			} else {
				require.Zero(t, finishes)
			}
			if window == "begin_ack_lost" {
				require.Equal(t, "SENDING", op.State)
			}
		})
	}
}
func TestCredentialLegacyRefreshCachedExpiryIsNotExtended(t *testing.T) {
	svc, store, client, vault := legacyRefreshFixture(t)
	_, err := svc.RefreshToken(context.Background(), "old-refresh", "")
	require.NoError(t, err)
	op, _, _, _, _, _ := store.snapshot()
	plain, err := json.Marshal(credentialLegacyRefreshReceipt{Response: openai.TokenResponse{AccessToken: "expired-access", ExpiresIn: 60}, ReceivedAt: time.Now().Add(-2 * time.Minute)})
	require.NoError(t, err)
	defer clear(plain)
	sealed, err := vault.SealData(op.ID, plain)
	require.NoError(t, err)
	store.mu.Lock()
	store.op.Ciphertext = sealed
	store.mu.Unlock()
	_, err = svc.RefreshToken(context.Background(), "old-refresh", "")
	require.ErrorIs(t, err, ErrLegacyCredentialRefreshExpired)
	require.EqualValues(t, 1, client.calls.Load())
}
func TestCredentialLegacyRefreshPanicDiagnosticScan(t *testing.T) {
	svc, store, client, _ := legacyRefreshFixture(t)
	client.refresh = func(context.Context, string, string, string) (*openai.TokenResponse, error) {
		panic("opaque-secret-canary")
	}
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	_, err := svc.RefreshToken(context.Background(), "opaque-input-canary", "")
	require.ErrorIs(t, err, ErrLegacyCredentialRefreshUnknown)
	require.Contains(t, logs.String(), "legacy_credential_refresh_panic")
	require.NotContains(t, logs.String(), "opaque-secret-canary")
	require.NotContains(t, logs.String(), "opaque-input-canary")
	op, _, _, _, _, _ := store.snapshot()
	require.Equal(t, "UNKNOWN", op.State)
}

func TestCredentialLegacyRefreshEnrichmentPanicKeepsDurableSuccess(t *testing.T) {
	svc, store, client, _ := legacyRefreshFixture(t)
	svc.SetPrivacyClientFactory(func(string) (*req.Client, error) { panic("opaque-enrichment-token") })
	result, err := svc.RefreshToken(context.Background(), "old-refresh", "")
	require.ErrorIs(t, err, ErrLegacyCredentialRefreshUnknown)
	require.Nil(t, result)
	op, _, _, _, _, finishes := store.snapshot()
	require.Equal(t, "SUCCEEDED", op.State)
	require.Equal(t, 1, finishes, "post-persistence enrichment panic cannot turn a durable success into UNKNOWN")
	svc.SetPrivacyClientFactory(nil)
	result, err = svc.RefreshToken(context.Background(), "old-refresh", "")
	require.NoError(t, err)
	require.Equal(t, "opaque-new-access", result.AccessToken)
	require.EqualValues(t, 1, client.calls.Load())
}

func TestCredentialLegacyRefreshMissingOperationStoreCannotFallThrough(t *testing.T) {
	svc, _, client, vault := legacyRefreshFixture(t)
	svc.credentialTokenGuard = &CredentialLegacyTokenGuard{store: &legacyCredentialRegistry{}, vault: vault}
	_, err := svc.RefreshToken(context.Background(), "old-refresh", "")
	require.ErrorIs(t, err, ErrCredentialVaultUnavailable)
	require.Zero(t, client.calls.Load())
}

func TestCredentialLegacyRefreshOmittedReplacementRetainsInputOwnership(t *testing.T) {
	svc, store, client, vault := legacyRefreshFixture(t)
	client.refresh = func(context.Context, string, string, string) (*openai.TokenResponse, error) {
		return &openai.TokenResponse{AccessToken: "new-access-without-refresh", ExpiresIn: 3600}, nil
	}
	info, err := svc.RefreshToken(context.Background(), "old-refresh", "")
	require.NoError(t, err)
	require.Empty(t, info.RefreshToken, "provider response must remain unchanged")
	op, _, outputs, _, _, _ := store.snapshot()
	require.Equal(t, "SUCCEEDED", op.State)
	require.Equal(t, legacyRefreshFingerprints(vault, "new-access-without-refresh", "old-refresh"), outputs)
	plain, err := vault.OpenData(op.ID, op.Ciphertext)
	require.NoError(t, err)
	defer clear(plain)
	var receipt credentialLegacyRefreshReceipt
	require.NoError(t, json.Unmarshal(plain, &receipt))
	require.Empty(t, receipt.Response.RefreshToken)
	account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "old-access", "refresh_token": "old-refresh"}}
	credentials, err := NewOpenAITokenRefresher(svc, nil).Refresh(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "old-refresh", credentials["refresh_token"], "existing merge behavior keeps the input refresh token")
	require.EqualValues(t, 1, client.calls.Load())
	receipt.ReceivedAt = time.Now().Add(-2 * time.Hour)
	expired, err := json.Marshal(receipt)
	require.NoError(t, err)
	defer clear(expired)
	sealed, err := vault.SealData(op.ID, expired)
	require.NoError(t, err)
	store.mu.Lock()
	store.op.Ciphertext = sealed
	store.mu.Unlock()
	_, err = svc.RefreshToken(context.Background(), "old-refresh", "")
	require.ErrorIs(t, err, ErrLegacyCredentialRefreshExpired)
	require.EqualValues(t, 1, client.calls.Load(), "an unchanged input refresh token is never automatically replayed")
}
