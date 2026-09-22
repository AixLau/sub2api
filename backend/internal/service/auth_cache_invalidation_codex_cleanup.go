package service

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const AuthInvalidationEventCodexIdentityOwner = "codex_identity_owner"

const codexIdentityIndexMaintenanceInterval = time.Minute

// Count performs bounded pruning of expired ownership-index members. Reusing
// the existing invalidation loop guarantees pruning even without a metrics
// scraper, and introduces no separate scheduler or identity TTL refresh.
type CodexIdentityIndexMaintainer interface {
	CountCodexIdentityKeys(context.Context) (map[string]int64, error)
}

func (w *AuthCacheInvalidationWorker) maintainCodexIdentityIndexes(parent context.Context, now time.Time) {
	if w.codexIdentityCleanup == nil || parent.Err() != nil || now.Before(w.nextCodexIdentityMaintenance) {
		return
	}
	maintainer, ok := w.codexIdentityCleanup.(CodexIdentityIndexMaintainer)
	if !ok {
		return
	}
	// Advance before IO so an unavailable Redis cannot turn this into a hot
	// retry loop. The repository caps work to a fixed batch per identity kind.
	w.nextCodexIdentityMaintenance = now.Add(codexIdentityIndexMaintenanceInterval)
	ctx, cancel := context.WithTimeout(parent, authInvalidationRedisTimeout)
	defer cancel()
	if _, err := maintainer.CountCodexIdentityKeys(ctx); err != nil && parent.Err() == nil {
		RecordCodexIdentityEvent("ownership", "error")
		w.recordFailure(fmt.Errorf("maintain Codex identity ownership indexes: %w", err))
	}
}

// Owner cleanup is delivered through the existing transactionally populated
// invalidation outbox. A database guard serializes cleanup with credential
// replacement/import and rejects deletion while another row owns the namespace.
type codexIdentityOwnerCleanupGuard interface {
	WithCodexIdentityOwnerCleanup(context.Context, string, func(context.Context, bool) error) error
}

func (w *AuthCacheInvalidationWorker) invalidateEvent(ctx context.Context, event AuthCacheInvalidationEvent) error {
	switch event.EventType {
	case "", "auth":
		if w.local != nil {
			w.local.invalidateLocalAuthCache(event.CacheKey)
		}
		if err := w.cache.DeleteAuthCache(ctx, event.CacheKey); err != nil {
			return err
		}
		return w.cache.PublishAuthCacheInvalidation(ctx, event.CacheKey)
	case AuthInvalidationEventCodexIdentityOwner:
		guard, ok := w.repo.(codexIdentityOwnerCleanupGuard)
		if !ok || w.codexIdentityCleanup == nil {
			return errors.New("Codex identity owner cleanup unavailable")
		}
		return guard.WithCodexIdentityOwnerCleanup(ctx, event.OwnerToken, func(ctx context.Context, live bool) error {
			if live {
				// A previous batch may have timed out before an account was
				// reimported. Finish only that already-fenced cleanup; never
				// begin a new deletion against a live shared namespace.
				if _, err := w.codexIdentityCleanup.ResumeCodexIdentityOwnerCleanup(ctx, event.OwnerToken); err != nil {
					return err
				}
				// Only the database guard may reactivate an owner. A later
				// request timestamp alone cannot revive deleted credentials.
				return w.codexIdentityCleanup.ActivateCodexIdentityOwner(ctx, event.OwnerToken)
			}
			_, err := w.codexIdentityCleanup.DeleteCodexIdentityOwner(ctx, event.OwnerToken)
			return err
		})
	default:
		return fmt.Errorf("unsupported cache invalidation event type %q", event.EventType)
	}
}
