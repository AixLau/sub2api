package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

var (
	ErrLegacyCredentialRefreshUnknown = errors.New("LEGACY_REFRESH_RESULT_UNKNOWN")
	ErrLegacyCredentialRefreshExpired = errors.New("LEGACY_REFRESH_RESULT_EXPIRED")
)

// The operation is an HTTP side-effect claim, never an execution-slot permit.
// SENDING is returned only for an acknowledged new owner. Existing SENDING or
// UNKNOWN operations must be rejected by Begin without repeating the provider.
type CredentialLegacyRefreshOperation struct {
	ID, OwnerNonce, State string
	Ciphertext            []byte
}

type CredentialLegacyRefreshStore interface {
	BeginLegacyCredentialRefresh(context.Context, []string, string) (CredentialLegacyRefreshOperation, error)
	FinishLegacyCredentialRefresh(context.Context, CredentialLegacyRefreshOperation, []string, []byte, bool) error
}

// Keep the received timestamp with the response. Reading the same durable
// result must not extend access-token expiry on every caller retry.
type credentialLegacyRefreshReceipt struct {
	Response   openai.TokenResponse `json:"response"`
	ReceivedAt time.Time            `json:"received_at"`
}

func legacyRefreshFingerprints(vault *CredentialVault, tokens ...string) []string {
	seen := make(map[string]bool)
	for _, token := range tokens {
		trimmed := strings.TrimSpace(token)
		if trimmed == "" {
			continue
		}
		seen[vault.Fingerprint("token", token)] = true
		seen[vault.Fingerprint("token", trimmed)] = true
	}
	values := make([]string, 0, len(seen))
	for value := range seen {
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}

func safeLegacyRefreshError(err error) error {
	switch {
	case errors.Is(err, ErrCredentialLegacyBypass):
		return ErrCredentialLegacyBypass
	case errors.Is(err, ErrLegacyCredentialRefreshUnknown):
		return ErrLegacyCredentialRefreshUnknown
	case errors.Is(err, ErrLegacyCredentialRefreshExpired):
		return ErrLegacyCredentialRefreshExpired
	default:
		return ErrCredentialVaultUnavailable
	}
}

func (receipt credentialLegacyRefreshReceipt) valid() bool {
	return receipt.Response.AccessToken != "" && !receipt.ReceivedAt.IsZero() && receipt.Response.ExpiresIn > 0 && receipt.Response.ExpiresIn <= math.MaxInt64-receipt.ReceivedAt.Unix()
}

func (s *OpenAIOAuthService) refreshLegacyCredentialToken(ctx context.Context, refreshToken, proxyURL, clientID string) (response *openai.TokenResponse, receivedAt time.Time, retErr error) {
	guard := s.credentialTokenGuard
	if guard == nil {
		// Direct unit fixtures create a service without production providers. The
		// production Wire provider always installs the non-nil guard below.
		response, retErr = s.oauthClient.RefreshTokenWithClientID(ctx, refreshToken, proxyURL, clientID)
		return response, time.Now().UTC(), retErr
	}

	var op CredentialLegacyRefreshOperation
	var pendingCipher []byte
	var outputFPs []string
	var store CredentialLegacyRefreshStore
	owned := false
	markUnknown := func() {
		if !owned || store == nil {
			return
		}
		// A compensation failure leaves the durable SENDING claim in place.
		// Neither an error nor a panic permits reuse of its provider token.
		defer func() {
			if recover() != nil {
				slog.Error("legacy_credential_refresh_compensation_panic", "action", "retain_pending_operation")
			}
		}()
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = store.FinishLegacyCredentialRefresh(cleanup, op, outputFPs, pendingCipher, false)
	}
	defer func() {
		if recover() != nil {
			markUnknown()
			response, receivedAt, retErr = nil, time.Time{}, ErrLegacyCredentialRefreshUnknown
			slog.Error("legacy_credential_refresh_panic", "action", "retain_claim_without_provider_retry")
		}
	}()

	if guard.vault == nil {
		return nil, time.Time{}, ErrCredentialVaultUnavailable
	}
	var ok bool
	store, ok = guard.store.(CredentialLegacyRefreshStore)
	if !ok {
		// Retain known-token early rejection for narrow test doubles, but never
		// fall through to an unjournaled provider call on a non-nil guard.
		if err := guard.Check(ctx, refreshToken); err != nil {
			return nil, time.Time{}, safeLegacyRefreshError(err)
		}
		return nil, time.Time{}, ErrCredentialVaultUnavailable
	}
	inputFPs := legacyRefreshFingerprints(guard.vault, refreshToken)
	if len(inputFPs) == 0 {
		return nil, time.Time{}, ErrCredentialVaultUnavailable
	}
	request, _ := json.Marshal([]string{clientID, proxyURL, refreshToken})
	requestHash := guard.vault.Fingerprint("legacy-refresh-request", string(request))
	clear(request)
	var err error
	op, err = store.BeginLegacyCredentialRefresh(ctx, inputFPs, requestHash)
	if err != nil {
		// This includes an uncertain Begin commit. Without an acknowledged owner
		// the process does not call the provider or attempt an ownership repair.
		return nil, time.Time{}, safeLegacyRefreshError(err)
	}
	if op.ID == "" || op.OwnerNonce == "" {
		return nil, time.Time{}, ErrCredentialVaultUnavailable
	}
	if op.State == "SUCCEEDED" {
		plain, err := guard.vault.OpenData(op.ID, op.Ciphertext)
		if err != nil {
			return nil, time.Time{}, ErrCredentialVaultUnavailable
		}
		defer clear(plain)
		var receipt credentialLegacyRefreshReceipt
		if json.Unmarshal(plain, &receipt) != nil || !receipt.valid() {
			return nil, time.Time{}, ErrCredentialVaultUnavailable
		}
		if receipt.ReceivedAt.Unix()+receipt.Response.ExpiresIn <= time.Now().Unix() {
			return nil, time.Time{}, ErrLegacyCredentialRefreshExpired
		}
		return &receipt.Response, receipt.ReceivedAt, nil
	}
	if op.State != "SENDING" {
		return nil, time.Time{}, ErrLegacyCredentialRefreshUnknown
	}
	owned = true
	if ctx.Err() != nil {
		markUnknown()
		return nil, time.Time{}, ErrLegacyCredentialRefreshUnknown
	}
	response, err = s.oauthClient.RefreshTokenWithClientID(ctx, refreshToken, proxyURL, clientID)
	receivedAt = time.Now().UTC()
	if response != nil {
		outputFPs = legacyRefreshFingerprints(guard.vault, response.AccessToken, response.RefreshToken)
		if err == nil && response.AccessToken != "" && response.RefreshToken == "" {
			// Existing account persistence retains the input refresh token when
			// the provider omits a replacement. Record that continuing ownership
			// without inventing a returned field or another provider attempt.
			outputFPs = legacyRefreshFingerprints(guard.vault, response.AccessToken, refreshToken)
		}
		plain, marshalErr := json.Marshal(credentialLegacyRefreshReceipt{Response: *response, ReceivedAt: receivedAt})
		if marshalErr == nil {
			pendingCipher, marshalErr = guard.vault.SealData(op.ID, plain)
		}
		clear(plain)
		if marshalErr != nil {
			markUnknown()
			return nil, time.Time{}, ErrLegacyCredentialRefreshUnknown
		}
	}
	if err != nil || response == nil || !(credentialLegacyRefreshReceipt{Response: *response, ReceivedAt: receivedAt}).valid() {
		markUnknown()
		return nil, time.Time{}, ErrLegacyCredentialRefreshUnknown
	}
	// No parsing, subscriptions, public response or Account write precedes this
	// durable result. Saving fails closed even when the provider already rotated.
	if err = store.FinishLegacyCredentialRefresh(ctx, op, outputFPs, pendingCipher, true); err != nil {
		markUnknown()
		return nil, time.Time{}, ErrLegacyCredentialRefreshUnknown
	}
	owned = false
	return response, receivedAt, nil
}
