package service

import (
	"context"
	"errors"
	"strconv"
	"time"

	"golang.org/x/sync/singleflight"
)

type CredentialRefreshOperation struct {
	ProxyID                                  *int64
	ID, OwnerNonce, Generation, Family       string
	InstanceID, PrincipalID, ExpectedVersion int64
	SecretAAD                                string
	Ciphertext                               []byte
}
type CredentialRefreshResult struct {
	Ciphertext                                 []byte
	AAD, AccessFingerprint, RefreshFingerprint string
	ExpiresAt                                  time.Time
}
type CredentialRefreshStore interface {
	BeginCredentialRefresh(context.Context, int64) (CredentialRefreshOperation, error)
	CompleteCredentialRefresh(context.Context, CredentialRefreshOperation, CredentialRefreshResult) error
	MarkCredentialRefreshUnknown(context.Context, CredentialRefreshOperation, *CredentialRefreshResult) error
}
type CredentialRefreshProvider interface {
	Refresh(context.Context, CredentialRefreshOperation, CredentialSecret) (CredentialSecret, time.Time, error)
}
type CredentialRefreshCoordinator struct {
	store    CredentialRefreshStore
	vault    *CredentialVault
	provider CredentialRefreshProvider
	local    singleflight.Group
}

func NewCredentialRefreshCoordinator(store CredentialRefreshStore, vault *CredentialVault, provider CredentialRefreshProvider) *CredentialRefreshCoordinator {
	return &CredentialRefreshCoordinator{store: store, vault: vault, provider: provider}
}
func (c *CredentialRefreshCoordinator) Refresh(ctx context.Context, instanceID int64) error {
	if c.provider == nil || c.vault == nil {
		return ErrCredentialUnverified
	}
	// Database family uniqueness is authoritative; singleflight only reduces local work.
	_, err, _ := c.local.Do(strconv.FormatInt(instanceID, 10), func() (any, error) {
		op, err := c.store.BeginCredentialRefresh(ctx, instanceID)
		if err != nil {
			return nil, err
		}
		markUnknown := func(result *CredentialRefreshResult) {
			cleanup, end := context.WithTimeout(context.Background(), 5*time.Second)
			defer end()
			_ = c.store.MarkCredentialRefreshUnknown(cleanup, op, result)
		}
		secret, err := c.vault.Open(op.SecretAAD, op.Ciphertext)
		if err != nil {
			markUnknown(nil)
			return nil, err
		}
		next, expires, err := c.provider.Refresh(ctx, op, secret)
		if err != nil || next.AccessToken == "" || !expires.After(time.Now()) {
			markUnknown(nil)
			return nil, errors.New("REFRESH_RESULT_UNKNOWN")
		}
		if next.RefreshToken == "" {
			next.RefreshToken = secret.RefreshToken
		}
		next.ClientID = secret.ClientID
		next.AccountSubject = secret.AccountSubject
		next.UserSubject = secret.UserSubject
		sealed, err := c.vault.Seal(op.ID, next)
		if err != nil {
			markUnknown(nil)
			return nil, err
		}
		result := CredentialRefreshResult{Ciphertext: sealed, AAD: op.ID, ExpiresAt: expires, AccessFingerprint: c.vault.Fingerprint("token", next.AccessToken), RefreshFingerprint: c.vault.Fingerprint("token", next.RefreshToken)}
		if err = c.store.CompleteCredentialRefresh(ctx, op, result); err != nil {
			markUnknown(&result)
			return nil, errors.New("REFRESH_RESULT_UNKNOWN")
		}
		return nil, nil
	})
	return err
}

// The existing fixed-address OAuth client handles protocol exchange. No account
// enrichment, privacy changes or arbitrary endpoint is part of refresh.
type openAICredentialRefreshProvider struct {
	client  OpenAIOAuthClient
	proxies ProxyRepository
}

func (p *openAICredentialRefreshProvider) Refresh(ctx context.Context, op CredentialRefreshOperation, secret CredentialSecret) (CredentialSecret, time.Time, error) {
	proxyURL := ""
	if op.ProxyID != nil {
		proxy, err := p.proxies.GetByID(ctx, *op.ProxyID)
		if err != nil {
			return CredentialSecret{}, time.Time{}, err
		}
		if proxy == nil {
			return CredentialSecret{}, time.Time{}, errors.New("PROXY_UNAVAILABLE")
		}
		proxyURL = proxy.URL()
	}
	token, err := p.client.RefreshTokenWithClientID(ctx, secret.RefreshToken, proxyURL, secret.ClientID)
	if err != nil {
		return CredentialSecret{}, time.Time{}, err
	}
	return CredentialSecret{AccessToken: token.AccessToken, RefreshToken: token.RefreshToken}, time.Now().Add(time.Duration(token.ExpiresIn) * time.Second), nil
}
