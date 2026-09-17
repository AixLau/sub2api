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
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrCredentialUnverified       = errors.New("CREDENTIAL_UNVERIFIED")
	ErrCredentialDuplicate        = errors.New("CREDENTIAL_DUPLICATE")
	ErrCredentialConflict         = errors.New("IDEMPOTENCY_PAYLOAD_MISMATCH")
	ErrCredentialImportExpired    = errors.New("CREDENTIAL_IMPORT_EXPIRED")
	ErrCredentialNotFound         = errors.New("CREDENTIAL_NOT_FOUND")
	ErrCredentialVaultUnavailable = errors.New("CREDENTIAL_VAULT_UNAVAILABLE")
)

// CredentialSecret is never a public DTO or an audit payload.
type CredentialSecret struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

type CredentialVault struct {
	aead    cipher.AEAD
	hmacKey []byte
}

func NewCredentialVault(keyHex string) (*CredentialVault, error) {
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
	return &CredentialVault{aead: aead, hmacKey: mac.Sum(nil)}, nil
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
func (s *CredentialImportService) Import(ctx context.Context, owner int64, operation string, secret CredentialSecret) (CredentialImportView, error) {
	if s.vault == nil {
		return CredentialImportView{}, ErrCredentialVaultUnavailable
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
	sealed, err := s.vault.Seal(id, secret)
	if err != nil {
		return CredentialImportView{}, err
	}
	payload, _ := json.Marshal(secret)
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
	Name             string                          `json:"name"`
	TotalConcurrency int                             `json:"total_concurrency"`
	Instances        []CreateCredentialInstanceInput `json:"instances"`
}
type CreateCredentialInstanceInput struct {
	ImportID string  `json:"credential_import_id"`
	Name     string  `json:"name"`
	Weight   float64 `json:"weight"`
	HardMax  *int    `json:"hard_max"`
}
type CredentialPrincipalCreator interface {
	CreateCredentialPrincipal(context.Context, int64, string, CreateCredentialPrincipalInput) (int64, error)
}
