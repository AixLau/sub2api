package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Ownership is metadata for HTTP identity storage, not part of the protocol ID
// or mapping key. Existing session/thread/cache projections stay unchanged.
type CodexIdentityOwnership struct {
	AccountOwner       string
	UserOwner          string
	ObservedAtMs       int64
	ExpiresAtMs        int64
	HistoryRetentionMs int64
	Legacy             bool
}

type codexIdentityOwnershipContextKey struct{}

var ErrCodexIdentityOwnerRetired = errors.New("Codex identity owner is retired or this request predates its cleanup")

func CodexIdentityAccountOwner(accountScope string) string {
	digest := sha256.Sum256([]byte(accountScope))
	return fmt.Sprintf("account:%x", digest[:])
}

func CodexIdentityUserOwner(userScope string) string {
	return strings.TrimSpace(userScope)
}

func WithCodexIdentityOwnership(ctx context.Context, owner CodexIdentityOwnership) context.Context {
	return context.WithValue(ctx, codexIdentityOwnershipContextKey{}, owner)
}

func CodexIdentityOwnershipFromContext(ctx context.Context) (CodexIdentityOwnership, bool) {
	owner, ok := ctx.Value(codexIdentityOwnershipContextKey{}).(CodexIdentityOwnership)
	return owner, ok && owner.AccountOwner != "" && owner.UserOwner != ""
}

func codexIdentityDeadlineContext(ctx context.Context, deadline time.Time) context.Context {
	if owner, ok := CodexIdentityOwnershipFromContext(ctx); ok {
		owner.ExpiresAtMs = deadline.UnixMilli()
		return WithCodexIdentityOwnership(ctx, owner)
	}
	return ctx
}

func codexHTTPIdentityOwnershipContext(ctx context.Context, c *gin.Context, account *Account, observedAt time.Time, legacy bool) context.Context {
	userScope := codexSessionIdentityDownstreamScope(c, getAPIKeyIDFromContext(c))
	if account == nil || !account.UsesOpenAICodexProtocol() || userScope == "" {
		return ctx
	}
	return WithCodexIdentityOwnership(ctx, CodexIdentityOwnership{
		AccountOwner: CodexIdentityAccountOwner(codexSessionIdentityUpstreamScope(codexAccountIdentitySource(c, account))),
		UserOwner:    CodexIdentityUserOwner(userScope), ObservedAtMs: observedAt.UnixMilli(), Legacy: legacy,
	})
}

// CodexIdentityOwnerCleaner is implemented by the Redis gateway cache. The
// durable deletion outbox retries cleanup; API-key deletion never targets a
// user owner because identity follows authenticated UserID across key changes.
type CodexIdentityOwnerCleaner interface {
	DeleteCodexIdentityOwner(ctx context.Context, owner string) (int64, error)
	ResumeCodexIdentityOwnerCleanup(ctx context.Context, owner string) (int64, error)
	ActivateCodexIdentityOwner(ctx context.Context, owner string) error
}

func (s *OpenAIGatewayService) codexIdentityHistoryRetention() time.Duration {
	if s != nil && s.cfg != nil {
		return s.cfg.Gateway.CodexIdentity.HistoryRetention()
	}
	return (config.CodexIdentityConfig{}).HistoryRetention()
}

type codexHTTPSideLifecycle struct {
	SessionID        string `json:"session_id"`
	CreatedAtMs      int64  `json:"created_at_ms"`
	LastObservedAtMs int64  `json:"last_observed_at_ms"`
}

// Side state has an owner-wide cleanup boundary. No independent TTL is applied
// to lifecycle, session, fork or child records: expiring just one would break a
// still-live tree. Only real HTTP observations update this metadata.
func (s *OpenAIGatewayService) observeCodexHTTPSide(ctx context.Context, sideKey, sessionID string, observedAt time.Time) error {
	store, err := s.codexHTTPIdentityStore()
	if err != nil {
		return err
	}
	cas, ok := store.(codexHTTPIdentityCASStore)
	if !ok {
		return ErrCodexSessionIdentityStoreUnavailable
	}
	parsed, err := uuid.Parse(sessionID)
	if err != nil || !isCodexUUIDv7(sessionID) {
		return fmt.Errorf("invalid Codex side lifecycle session")
	}
	seconds, nanos := parsed.Time().UnixTime()
	key := strings.Replace(sideKey, "v3:side-session:", "v4:side-lifecycle:", 1)
	for attempt := 0; attempt < 16; attempt++ {
		record := codexHTTPSideLifecycle{
			SessionID: sessionID, CreatedAtMs: time.Unix(seconds, nanos).UnixMilli(), LastObservedAtMs: observedAt.UnixMilli(),
		}
		previous, err := store.GetCodexSessionIdentity(ctx, key)
		if err != nil && !errors.Is(err, ErrCodexSessionIdentityNotFound) {
			return fmt.Errorf("read Codex side lifecycle: %w", err)
		}
		if err == nil {
			var current codexHTTPSideLifecycle
			if json.Unmarshal([]byte(previous), &current) != nil || current.SessionID != sessionID {
				return fmt.Errorf("invalid Codex side lifecycle record")
			}
			if current.LastObservedAtMs >= record.LastObservedAtMs {
				RecordCodexIdentityEvent("side_lifecycle", "stale_rejected")
				return nil
			}
			record.CreatedAtMs = current.CreatedAtMs
		} else {
			previous = ""
		}
		encoded, _ := json.Marshal(record)
		updated, err := cas.CompareAndSwapCodexSessionIdentity(ctx, key, previous, string(encoded), 0)
		if err != nil {
			return fmt.Errorf("update Codex side lifecycle: %w", err)
		}
		if updated {
			RecordCodexIdentityEvent("side_lifecycle", "observed")
			return nil
		}
		RecordCodexIdentityEvent("side_lifecycle", "contention")
	}
	return fmt.Errorf("Codex side lifecycle update contention")
}
