package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

type AdmissionDecisionCode string

const (
	AdmissionAdmitted       AdmissionDecisionCode = "ADMITTED"
	AdmissionWait           AdmissionDecisionCode = "WAIT"
	AdmissionRejected       AdmissionDecisionCode = "REJECTED"
	AdmissionAlreadyRunning AdmissionDecisionCode = "ALREADY_RUNNING"
	AdmissionConfigStale    AdmissionDecisionCode = "CONFIG_STALE"
)

var ErrAdmissionStoreUnavailable = errors.New("ADMISSION_STORE_UNAVAILABLE")
var ErrAdmissionOwnership = errors.New("ADMISSION_OWNERSHIP_LOST")

type AdmissionInput struct {
	Maintenance                                                bool
	RequestID, IdempotencyKey, PayloadDigest, Node, OwnerNonce string
	PrincipalID, UserID, APIKeyID                              int64
	// IDs are authorized candidate hints; the transaction rechecks group grants.
	CandidateIDs                     []int64
	OriginalSession, Endpoint, Model string
	HasState                         bool
	Deadline                         time.Time
	ExpectedConfigVersion            int64
}
type AdmissionDecision struct {
	Code     AdmissionDecisionCode
	Reason   string
	Snapshot *CredentialExecutionSnapshot
}

// Value fields and cloned ciphertext ensure one attempt cannot see a credential
// or identity mutate underneath it. HTTP adapters never choose another account.
type CredentialExecutionSnapshot struct {
	AccountUpdatedAt                            time.Time
	Proxy                                       *Proxy
	ProxyUpdatedAt                              *time.Time
	Lease                                       LeaseRef
	AccountID, CredentialVersion, ConfigVersion int64
	InstallationID, IdentitySource, SecretAAD   string
	SecretCiphertext                            []byte
	ProxyID                                     *int64
	Endpoint, CallerScope                       string
	Deadline                                    time.Time
}
type LeaseRef struct {
	ID, RequestID, Generation, OwnerNonce  string
	PrincipalID, InstanceID, UserID, Epoch int64
}
type FinishAdmissionInput struct {
	Lease LeaseRef
	// Complete means an upstream terminal response was observed, or the executor
	// proves NOT_SENT before entering transport after an acknowledged dispatch
	// commit. A partial EOF, timeout after send, or uncertain commit is not proof.
	Outcome                   string
	Complete                  bool
	InputTokens, OutputTokens *int64
	UpstreamRequestID         string
}
type PrincipalAdmissionStore interface {
	TryAdmit(context.Context, AdmissionInput) (AdmissionDecision, error)
	BeginDispatch(context.Context, LeaseRef) error
	Heartbeat(context.Context, LeaseRef) error
	Finish(context.Context, FinishAdmissionInput) error
	Cancel(context.Context, LeaseRef) error
}

func CredentialScopeHash(user, key int64) string {
	b, _ := json.Marshal([]int64{DeploymentPrincipalScope, user, key})
	return CredentialDigest(b)
}
func CredentialDigest(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
