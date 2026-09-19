package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap/zapcore"
)

var (
	ErrCredentialOwnershipMismatch = errors.New("CREDENTIAL_OWNERSHIP_MISMATCH")
	ErrCredentialUnverified        = errors.New("CREDENTIAL_UNVERIFIED")
	ErrCredentialDuplicate         = errors.New("CREDENTIAL_DUPLICATE")
	ErrCredentialConflict          = errors.New("IDEMPOTENCY_PAYLOAD_MISMATCH")
	ErrCredentialImportExpired     = errors.New("CREDENTIAL_IMPORT_EXPIRED")
	ErrCredentialNotFound          = errors.New("CREDENTIAL_NOT_FOUND")
	ErrCredentialVaultUnavailable  = errors.New("CREDENTIAL_VAULT_UNAVAILABLE")
)

type CredentialDuplicateLocation struct{ AccountID, PrincipalID, InstanceID int64 }

func (e *CredentialDuplicateLocation) Error() string { return "CREDENTIAL_DUPLICATE" }
func (e *CredentialDuplicateLocation) Unwrap() error { return ErrCredentialDuplicate }

// CredentialSecret is never a public DTO or an audit payload.
type CredentialSecret struct {
	ClientID       string `json:"client_id,omitempty"`
	AccessToken    string `json:"access_token"`
	RefreshToken   string `json:"refresh_token,omitempty"`
	AccountSubject string `json:"account_subject,omitempty"`
	UserSubject    string `json:"user_subject,omitempty"`
}

type CredentialVault struct {
	aead            cipher.AEAD
	hmacKey         []byte
	encryptionKeyID string
}

func NewCredentialVault(keyHex string) (*CredentialVault, error) {
	return NewCredentialVaultWithFingerprintKey(keyHex, "")
}

// Fingerprint keys stay stable when the encryption key rotates: historical
// token/family aliases and verified subject identifiers must not change.
func NewCredentialVaultWithFingerprintKey(keyHex, fingerprintKeyHex string) (*CredentialVault, error) {
	key, err := hex.DecodeString(strings.TrimSpace(keyHex))
	if err != nil || len(key) != 32 {
		return nil, ErrCredentialVaultUnavailable
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("sub2api:credential-fingerprints:v1"))
	fingerprintKey := mac.Sum(nil)
	if fingerprintKeyHex != "" {
		fingerprintKey, err = hex.DecodeString(strings.TrimSpace(fingerprintKeyHex))
		if err != nil || len(fingerprintKey) != 32 {
			return nil, ErrCredentialVaultUnavailable
		}
	}
	id := sha256.Sum256(append([]byte("sub2api:credential-encryption-key:v1:"), key...))
	return &CredentialVault{aead: aead, hmacKey: fingerprintKey, encryptionKeyID: hex.EncodeToString(id[:])}, nil
}
func (v *CredentialVault) Seal(aad string, secret CredentialSecret) ([]byte, error) {
	if v == nil {
		return nil, ErrCredentialVaultUnavailable
	}
	plain, err := json.Marshal(secret)
	if err != nil {
		return nil, err
	}
	defer clear(plain)
	nonce := make([]byte, v.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return v.aead.Seal(nonce, nonce, plain, []byte(aad)), nil
}
func (v *CredentialVault) Open(aad string, sealed []byte) (CredentialSecret, error) {
	var secret CredentialSecret
	if v == nil {
		return secret, ErrCredentialVaultUnavailable
	}
	n := v.aead.NonceSize()
	if len(sealed) < n {
		return secret, errors.New("invalid credential ciphertext")
	}
	plain, err := v.aead.Open(nil, sealed[:n], sealed[n:], []byte(aad))
	if err != nil {
		return secret, errors.New("credential authentication failed")
	}
	defer clear(plain)
	if err = json.Unmarshal(plain, &secret); err != nil {
		return CredentialSecret{}, errors.New("invalid credential plaintext")
	}
	return secret, nil
}
func (v *CredentialVault) Fingerprint(kind, value string) string {
	mac := hmac.New(sha256.New, v.hmacKey)
	mac.Write([]byte(kind))
	mac.Write([]byte{0})
	mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

type VerifiedCredential struct {
	State          string
	Provider       string
	AccountSubject string
	UserSubject    string
	Family         string
	Capabilities   []string
	ExpiresAt      time.Time
}

// Implementations must authenticate provider evidence. JWT decoding is not a
// verifier. Network calls happen before the control transaction; no arbitrary
// provider URL can be supplied by the importing administrator.
type CredentialVerifier interface {
	Verify(context.Context, CredentialSecret) (VerifiedCredential, error)
}

type CredentialImportRecord struct {
	ID                                    string
	Scope, OwnerID                        int64
	OperationHash, PayloadHash            string
	Ciphertext                            []byte
	AccessFingerprint, RefreshFingerprint string
	State, SubjectKey, Family             string
	Capabilities                          []string
	TokenExpiresAt, ExpiresAt             time.Time
}

type CredentialImportView struct {
	ID         string    `json:"import_id"`
	State      string    `json:"verification_state"`
	CanRefresh bool      `json:"can_refresh"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type CredentialImportStore interface {
	PutImport(context.Context, CredentialImportRecord) (CredentialImportView, error)
	GetImport(context.Context, int64, int64, string) (*CredentialImportView, error)
}

type CredentialImportService struct {
	store    CredentialImportStore
	vault    *CredentialVault
	verifier CredentialVerifier
}

func NewCredentialImportService(store CredentialImportStore, vault *CredentialVault, verifier CredentialVerifier) *CredentialImportService {
	return &CredentialImportService{store: store, vault: vault, verifier: verifier}
}
func (s *CredentialImportService) Import(ctx context.Context, owner int64, operation string, secret CredentialSecret) (view CredentialImportView, retErr error) {
	defer func() {
		if recover() != nil {
			view = CredentialImportView{}
			retErr = ErrCredentialUnverified
			slog.Error("credential_import_panic", "action", "rejected_without_secret_diagnostics")
		}
	}()

	// Reject ambiguous token bytes instead of assuming provider whitespace rules.
	if secret.AccessToken != strings.TrimSpace(secret.AccessToken) || secret.RefreshToken != strings.TrimSpace(secret.RefreshToken) {
		return CredentialImportView{}, ErrCredentialUnverified
	}
	if s.vault == nil {
		return CredentialImportView{}, ErrCredentialVaultUnavailable
	}
	if initializer, ok := s.store.(interface {
		ConfigureCredentialArbitration(context.Context, *CredentialVault) error
	}); ok {
		if err := initializer.ConfigureCredentialArbitration(ctx, s.vault); err != nil {
			return CredentialImportView{}, ErrCredentialVaultUnavailable
		}
	}

	if len(secret.ClientID) > 256 {
		return CredentialImportView{}, errors.New("INVALID_CLIENT_ID")
	}
	if owner <= 0 || len(operation) < 1 || len(operation) > 128 || len(secret.AccessToken) < 1 || len(secret.AccessToken) > 32768 || len(secret.RefreshToken) > 32768 {
		return CredentialImportView{}, errors.New("INVALID_CREDENTIAL_IMPORT")
	}
	verification := VerifiedCredential{State: "UNVERIFIED"}
	if s.verifier != nil {
		var err error
		verification, err = s.verifier.Verify(ctx, secret)
		if err != nil {
			return CredentialImportView{}, ErrCredentialUnverified
		} // provider errors may contain secrets
	}
	if verification.State != "VERIFIED" {
		verification = VerifiedCredential{State: "UNVERIFIED"}
	}
	if verification.State == "VERIFIED" && (verification.Provider != "openai_oauth" || verification.AccountSubject == "" || verification.UserSubject == "" || !verification.ExpiresAt.After(time.Now())) {
		return CredentialImportView{}, ErrCredentialUnverified
	}
	id := uuid.NewString()
	// Provider identity is encrypted with the versioned token; caller-supplied
	// subject fields are ignored. This preserves the established session namespace.
	secret.AccountSubject, secret.UserSubject = "", ""
	payload, _ := json.Marshal(secret)
	secret.AccountSubject = verification.AccountSubject
	secret.UserSubject = verification.UserSubject
	sealed, err := s.vault.Seal(id, secret)
	if err != nil {
		return CredentialImportView{}, err
	}
	record := CredentialImportRecord{ID: id, Scope: DeploymentPrincipalScope, OwnerID: owner,
		OperationHash: s.vault.Fingerprint("import-operation", operation), PayloadHash: s.vault.Fingerprint("import-payload", string(payload)),
		Ciphertext: sealed, AccessFingerprint: s.vault.Fingerprint("token", secret.AccessToken), State: verification.State,
		Capabilities: append([]string{}, verification.Capabilities...), TokenExpiresAt: verification.ExpiresAt, ExpiresAt: time.Now().Add(15 * time.Minute)}
	clear(payload)
	if secret.RefreshToken != "" {
		record.RefreshFingerprint = s.vault.Fingerprint("token", secret.RefreshToken)
	}
	if verification.State == "VERIFIED" {
		// Length-delimited JSON keeps account/user scope unambiguous.
		subject, _ := json.Marshal([]string{verification.Provider, verification.AccountSubject, verification.UserSubject})
		record.SubjectKey = s.vault.Fingerprint("subject", string(subject))
		if verification.Family != "" {
			record.Family = s.vault.Fingerprint("family", verification.Family)
		}
	}
	return s.store.PutImport(ctx, record)
}
func (s *CredentialImportService) Get(ctx context.Context, owner int64, id string) (*CredentialImportView, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, ErrCredentialNotFound
	}
	return s.store.GetImport(ctx, DeploymentPrincipalScope, owner, id)
}

type CreateCredentialPrincipalInput struct {
	Extra            map[string]any                  `json:"extra,omitempty"`
	Name             string                          `json:"name"`
	TotalConcurrency int                             `json:"account_max_concurrency"`
	GroupIDs         []int64                         `json:"group_ids"`
	ProxyID          *int64                          `json:"proxy_id"`
	Priority         int                             `json:"priority"`
	RateMultiplier   *float64                        `json:"rate_multiplier"`
	Activate         bool                            `json:"-"`
	Instances        []CreateCredentialInstanceInput `json:"instances"`
}
type CreateCredentialInstanceInput struct {
	ImportID string  `json:"credential_import_id"`
	Name     string  `json:"name"`
	Weight   float64 `json:"weight"`
	HardMax  *int    `json:"max_concurrency"`
}
type CredentialPrincipalCreator interface {
	CreateCredentialPrincipal(context.Context, int64, string, CreateCredentialPrincipalInput) (int64, error)
}

// SealData/OpenData are also used for encrypted migration snapshots. AAD is a
// purpose-prefixed operation ID; it never contains an access/refresh token.
func (v *CredentialVault) SealData(aad string, plain []byte) ([]byte, error) {
	if v == nil {
		return nil, ErrCredentialVaultUnavailable
	}
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return v.aead.Seal(nonce, nonce, plain, []byte(aad)), nil
}
func (v *CredentialVault) OpenData(aad string, sealed []byte) ([]byte, error) {
	if v == nil {
		return nil, ErrCredentialVaultUnavailable
	}
	n := v.aead.NonceSize()
	if len(sealed) < n {
		return nil, errors.New("invalid ciphertext")
	}
	return v.aead.Open(nil, sealed[:n], sealed[n:], []byte(aad))
}

// FingerprintKeyHex is for the offline rotation command's mode-0600 key bundle.
// It must never be included in admin DTOs, audit records or diagnostic output.
func (v *CredentialVault) FingerprintKeyHex() string { return hex.EncodeToString(v.hmacKey) }
func (v *CredentialVault) EncryptionKeyID() string {
	if v == nil {
		return ""
	}
	return v.encryptionKeyID
}
func (v *CredentialVault) FingerprintKeyID() string {
	if v == nil {
		return ""
	}
	return v.Fingerprint("key-check", "v1")
}

// Prevent accidental structured/debug/panic formatting from expanding secrets.
// JSON marshaling remains internal to the authenticated-encryption boundary.
func (CredentialSecret) Format(state fmt.State, verb rune) {
	_, _ = state.Write([]byte("[credential secret redacted]"))
}
func (CredentialSecret) LogValue() slog.Value {
	return slog.StringValue("[credential secret redacted]")
}
func (CredentialVault) Format(state fmt.State, verb rune) {
	_, _ = state.Write([]byte("[credential vault redacted]"))
}
func (CredentialVault) LogValue() slog.Value { return slog.StringValue("[credential vault redacted]") }

func (CredentialSecret) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddString("secret", "[redacted]")
	return nil
}
func (CredentialVault) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddString("vault", "[redacted]")
	return nil
}

// Reverification uses the retained encrypted authorization. The browser carries
// only its opaque reference; a provider outage never requires token disclosure.
type CredentialImportRevalidator interface {
	PendingCredentialImport(context.Context, int64, string) (CredentialImportRecord, error)
	VerifyCredentialImport(context.Context, int64, CredentialImportRecord) (CredentialImportView, error)
}

func (s *CredentialImportService) Reverify(ctx context.Context, owner int64, id string) (view CredentialImportView, retErr error) {
	defer func() {
		if recover() != nil {
			view = CredentialImportView{}
			retErr = ErrCredentialUnverified
			slog.Error("credential_reverification_panic", "action", "rejected_without_secret_diagnostics")
		}
	}()
	if _, err := uuid.Parse(id); err != nil {
		return CredentialImportView{}, ErrCredentialNotFound
	}
	store, ok := s.store.(CredentialImportRevalidator)
	if !ok || s.vault == nil || s.verifier == nil {
		return CredentialImportView{}, ErrCredentialVaultUnavailable
	}
	rec, err := store.PendingCredentialImport(ctx, owner, id)
	if err != nil {
		return CredentialImportView{}, err
	}
	if rec.State != "UNVERIFIED" {
		view, err := s.Get(ctx, owner, id)
		if err != nil {
			return CredentialImportView{}, err
		}
		return *view, nil
	}
	secret, err := s.vault.Open(rec.ID, rec.Ciphertext)
	if err != nil {
		return CredentialImportView{}, ErrCredentialVaultUnavailable
	}
	verified, err := s.verifier.Verify(ctx, secret)
	if err != nil || verified.State != "VERIFIED" || verified.Provider != "openai_oauth" || verified.AccountSubject == "" || verified.UserSubject == "" || !verified.ExpiresAt.After(time.Now()) {
		return CredentialImportView{ID: id, State: "UNVERIFIED", ExpiresAt: rec.ExpiresAt}, nil
	}
	secret.AccountSubject, secret.UserSubject = verified.AccountSubject, verified.UserSubject
	rec.Ciphertext, err = s.vault.Seal(id, secret)
	if err != nil {
		return CredentialImportView{}, err
	}
	subject, _ := json.Marshal([]string{verified.Provider, verified.AccountSubject, verified.UserSubject})
	rec.SubjectKey = s.vault.Fingerprint("subject", string(subject))
	rec.Capabilities = verified.Capabilities
	rec.TokenExpiresAt = verified.ExpiresAt
	return store.VerifyCredentialImport(ctx, owner, rec)
}
