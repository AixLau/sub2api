package service

import (
	"context"
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

var ErrCredentialLegacyBypass = errors.New("CONTROLLED_CREDENTIAL_REQUIRES_PRINCIPAL_API")

// Equality with a server-keyed fingerprint only proves a previously registered
// token. It does not verify a provider identity or certify unknown tokens.
type CredentialKnownTokenStore interface {
	KnownCredentialTokenFingerprints(context.Context, []string) (bool, error)
}
type CredentialLegacyTokenGuard struct {
	store CredentialKnownTokenStore
	vault *CredentialVault
}

func NewCredentialLegacyTokenGuard(repo any, cfg *config.Config) *CredentialLegacyTokenGuard {
	store, _ := repo.(CredentialKnownTokenStore)
	var vault *CredentialVault
	if cfg != nil {
		vault, _ = NewCredentialVaultWithFingerprintKey(cfg.Gateway.CredentialVaultKey, cfg.Gateway.CredentialFingerprintKey)
	}
	return &CredentialLegacyTokenGuard{store: store, vault: vault}
}
func (g *CredentialLegacyTokenGuard) Check(ctx context.Context, tokens ...string) error {
	if g == nil {
		return nil
	} // direct unit constructors; production providers always install a guard
	var fingerprints []string
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		if g.vault != nil {
			fingerprints = append(fingerprints, g.vault.Fingerprint("token", token))
		} else {
			fingerprints = append(fingerprints, "")
		}
	}
	if len(fingerprints) == 0 {
		return nil
	}
	if g.store == nil {
		return ErrCredentialVaultUnavailable
	}
	// A missing key may only pass when there are no registered controlled tokens.
	if g.vault == nil {
		fingerprints = nil
	}
	known, err := g.store.KnownCredentialTokenFingerprints(ctx, fingerprints)
	if err != nil {
		return ErrCredentialVaultUnavailable
	}
	if known {
		return ErrCredentialLegacyBypass
	}
	return nil
}
func (g *CredentialLegacyTokenGuard) CheckCredentials(ctx context.Context, credentials map[string]any) error {
	var tokens []string
	for _, key := range []string{"access_token", "refresh_token", "api_key", "auth_token"} {
		if value, ok := credentials[key].(string); ok {
			tokens = append(tokens, value)
		}
	}
	return g.Check(ctx, tokens...)
}
