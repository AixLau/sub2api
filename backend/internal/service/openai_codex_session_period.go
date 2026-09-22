package service

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	codexSessionPeriodMappingVersion = "v3"
	codexSessionPeriodMin            = 5 * 24 * time.Hour
	codexSessionPeriodMax            = 7 * 24 * time.Hour
	codexSessionPeriodGrace          = time.Hour
)

type codexSessionPeriod struct {
	key       string
	epoch     int64
	duration  time.Duration
	expiresAt time.Time
}

// Each account/user pair has its own fixed schedule. Activity cannot slide the
// boundary. The v3 namespace is independent of v2's per-client session mapping.
func resolveCodexSessionPeriod(seed, userScope, accountScope string, now time.Time) codexSessionPeriod {
	namespace := strings.Join([]string{codexSessionPeriodMappingVersion, seed, userScope, accountScope}, "\x00")
	digest := sha256.Sum256([]byte(namespace))
	minSeconds := int64(codexSessionPeriodMin / time.Second)
	spanSeconds := uint64((codexSessionPeriodMax-codexSessionPeriodMin)/time.Second) + 1
	seconds := minSeconds + int64(binary.BigEndian.Uint64(digest[:8])%spanSeconds)
	offset := int64(binary.BigEndian.Uint64(digest[8:16]) % uint64(seconds))
	epoch := (now.Unix() + offset) / seconds
	key := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d", namespace, epoch)))
	return codexSessionPeriod{
		key:       fmt.Sprintf("%x", key[:]),
		epoch:     epoch,
		duration:  time.Duration(seconds) * time.Second,
		expiresAt: time.Unix((epoch+1)*seconds-offset, 0).Add(codexSessionPeriodGrace),
	}
}

func codexHTTPPromptCacheKey(namespace string, input *codexSessionIdentityInput, task string) string {
	// spawn_agent keeps the original session/cache root even though its thread
	// changes. /side supplies a new session/cache root. Neither may use the
	// compressed period session or the current child thread as cache affinity.
	root := codexFirstIdentityValue(input.originalSessionID, input.promptCacheKey, task)
	partition := codexFirstIdentityValue(input.promptCacheKey, root)
	// Both root and partition are client strings; encode their boundaries
	// explicitly so embedded delimiters cannot merge distinct cache roots.
	canonical, _ := json.Marshal([]string{namespace, root, partition})
	return deriveStableUUIDv4("sub2api:codex-cache:v3:" + string(canonical))
}

type codexHTTPSessionSelection struct {
	id                   string
	cacheNamespace       string
	preserveV3Threads    bool
	forkThreadID         string
	threadTTL            time.Duration
	threadExpiresAt      time.Time
	sideKey              string
	allowParentBootstrap bool
}

// A fork marker by itself is not enough to identify a new /side root. Older
// ordinary sessions can carry the marker after switching from device mode,
// while a native side root also carries its fork ordinal. Existing side
// registrations remain authoritative regardless of the incoming metadata.
func codexHTTPInputHasSideRootEvidence(input *codexSessionIdentityInput) bool {
	return input != nil && input.forkedFromThreadID != "" && input.forkedFromOrdinalExclusivePresent
}

func (s *OpenAIGatewayService) resolveCodexHTTPSession(ctx context.Context, c *gin.Context, input *codexSessionIdentityInput, userScope, accountScope string, period codexSessionPeriod, now time.Time) (codexHTTPSessionSelection, error) {
	selection := codexHTTPSessionSelection{cacheNamespace: period.key, threadTTL: period.expiresAt.Sub(now), threadExpiresAt: period.expiresAt}
	if input.originalSessionID != "" {
		sideKey := codexHTTPIdentityMappingKey("side-session", userScope, accountScope, input.originalSessionID)
		side, lookupErr := s.lookupCodexHTTPIdentityMapping(ctx, c, sideKey)
		if lookupErr != nil && !errors.Is(lookupErr, ErrCodexSessionIdentityNotFound) {
			return selection, lookupErr
		}
		if lookupErr == nil || codexHTTPInputHasSideRootEvidence(input) {
			if lookupErr == nil {
				RecordCodexIdentityEvent("side_session", "reused")
			}
			if lookupErr != nil && input.parentThreadID != "" {
				RecordCodexIdentityEvent("current_parent", "missing")
				return selection, fmt.Errorf("new Codex side has no registered parent in its session: %w", ErrCodexSessionIdentityNotFound)
			}
			if input.forkedFromThreadID != "" {
				fork, err := s.pinCodexHTTPSideFork(ctx, c, userScope, accountScope, input.originalSessionID, input.forkedFromThreadID, lookupErr == nil)
				if err != nil {
					return selection, err
				}
				selection.forkThreadID = fork.ThreadID
				selection.preserveV3Threads = fork.PreserveV3Threads
			} else {
				store, _ := s.codexHTTPIdentityStore()
				forkKey := codexHTTPThreadKey("side-fork", userScope, accountScope, "", input.originalSessionID)
				fork, err := readCodexHTTPSideFork(ctx, store, forkKey)
				if err != nil && !errors.Is(err, ErrCodexSessionIdentityNotFound) {
					return selection, err
				}
				selection.preserveV3Threads = errors.Is(err, ErrCodexSessionIdentityNotFound) || fork.PreserveV3Threads
			}
			if lookupErr != nil {
				var err error
				side, err = s.resolveCodexSessionIdentityMapping(ctx, c, sideKey, 0)
				if err != nil {
					return selection, err
				}
			}
			if !isCodexUUIDv7(side) {
				return selection, fmt.Errorf("invalid Codex side session mapping value")
			}
			selection.id, selection.cacheNamespace, selection.threadTTL = side, sideKey, 0
			selection.threadExpiresAt, selection.sideKey = time.Time{}, sideKey
			return selection, nil
		}
	} else if codexHTTPInputHasSideRootEvidence(input) {
		return selection, fmt.Errorf("Codex side session requires an original session_id")
	}
	var err error
	if input.parentThreadID != "" {
		// A normal child can only join a period session already established by
		// its parent. Do not allocate a new session on a missing-parent request.
		selection.id, err = s.lookupCodexHTTPIdentityMapping(ctx, c, period.key)
		if err == nil {
			RecordCodexIdentityEvent("period_session", "reused")
		} else if errors.Is(err, ErrCodexSessionIdentityNotFound) {
			if input.forkedFromThreadID != "" && !codexHTTPInputHasSideRootEvidence(input) {
				selection.id, err = s.resolveCodexSessionIdentityMapping(codexIdentityDeadlineContext(ctx, period.expiresAt), c, period.key, selection.threadTTL)
				selection.allowParentBootstrap = err == nil
			}
			if err != nil {
				RecordCodexIdentityEvent("current_parent", "missing")
			}
		}
	} else {
		selection.id, err = s.resolveCodexSessionIdentityMapping(codexIdentityDeadlineContext(ctx, period.expiresAt), c, period.key, selection.threadTTL)
	}
	return selection, err
}

// Existing side threads may already have immutable v3 projections. They belong
// to a still-live side and can be registered under that side's session key.
// Ordinary requests never recover their current thread from these old records.
func (s *OpenAIGatewayService) resolveCodexHTTPCurrentThread(ctx context.Context, c *gin.Context, selection codexHTTPSessionSelection, userScope, accountScope, raw string, reference bool) (string, error) {
	key := codexHTTPThreadKey("thread-current", userScope, accountScope, selection.id, raw)
	mapped, err := s.lookupCodexHTTPIdentityMapping(ctx, c, key)
	if err == nil {
		RecordCodexIdentityEvent("thread_current", "reused")
	}
	if err == nil || !errors.Is(err, ErrCodexSessionIdentityNotFound) {
		return mapped, err
	}
	candidate := codexHTTPSessionThreadID(selection.id, raw)
	if selection.preserveV3Threads {
		previous, lookupErr := s.lookupCodexHTTPIdentityMapping(ctx, c, codexHTTPIdentityMappingKey("thread", userScope, accountScope, raw))
		if lookupErr == nil {
			candidate, reference = previous, false
		} else if !errors.Is(lookupErr, ErrCodexSessionIdentityNotFound) {
			return "", lookupErr
		}
	}
	if reference {
		RecordCodexIdentityEvent("current_parent", "missing")
		return "", fmt.Errorf("Codex parent thread not registered in current session: %w", ErrCodexSessionIdentityNotFound)
	}
	if !selection.threadExpiresAt.IsZero() {
		ctx = codexIdentityDeadlineContext(ctx, selection.threadExpiresAt)
	}
	return s.registerCodexHTTPThread(ctx, c, key, candidate, selection.threadTTL)
}

// resolveCodexHTTPFingerprintIDs is called once per HTTP attempt, before either
// carrier is projected. Only session mode needs authenticated scope and a store.
// Missing task/user identity conservatively retains device convergence.
func (s *OpenAIGatewayService) resolveCodexHTTPFingerprintIDs(ctx context.Context, c *gin.Context, account *Account, now time.Time) (_ *codexFingerprintIDs, resultErr error) {
	defer func() {
		switch {
		case resultErr == nil, errors.Is(resultErr, ErrCodexSessionIdentityNotFound), errors.Is(resultErr, errCodexSideForkConflict):
		case errors.Is(resultErr, ErrCodexIdentityOwnerRetired):
			RecordCodexIdentityEvent("ownership", "stale_rejected")
		default:
			RecordCodexIdentityEvent("identity_store", "error")
		}
	}()
	if account == nil {
		return nil, nil
	}
	mode := account.GetCodexFingerprintMode()
	if mode != codexFingerprintSession {
		var headers http.Header
		if c != nil && c.Request != nil {
			headers = c.Request.Header
		}
		return resolveCodexFingerprintIDsFromRequest(account, headers), nil
	}
	ids := resolveCodexFingerprintIDs(account, "", codexFingerprintDevice)
	if ids == nil {
		return nil, nil
	}
	input := stagedCodexSessionIdentityInput(c)
	userScope := codexSessionIdentityDownstreamScope(c, getAPIKeyIDFromContext(c))
	if input == nil || userScope == "" {
		return ids, nil
	}
	task := codexFirstIdentityValue(input.threadID, input.originalSessionID, input.clientRequestID)
	if task == "" || (input.parentReferencePresent && input.parentThreadID == "") {
		return ids, nil
	}
	accountScope := codexSessionIdentityUpstreamScope(account)
	ctx = codexHTTPIdentityOwnershipContext(ctx, c, account, now, false)
	if owner, ok := CodexIdentityOwnershipFromContext(ctx); ok {
		owner.HistoryRetentionMs = s.codexIdentityHistoryRetention().Milliseconds()
		ctx = WithCodexIdentityOwnership(ctx, owner)
	}
	period := resolveCodexSessionPeriod(ids.seed, userScope, accountScope, now)
	selection, err := s.resolveCodexHTTPSession(ctx, c, input, userScope, accountScope, period, now)
	if err != nil {
		return nil, err
	}
	if input.parentThreadID != "" {
		ids.parentThreadID, err = s.resolveCodexHTTPCurrentThread(ctx, c, selection, userScope, accountScope, input.parentThreadID, !selection.allowParentBootstrap)
		if err != nil {
			return nil, err
		}
	}
	if input.forkedFromThreadID != "" && selection.sideKey == "" {
		// A fork marker without native side evidence belongs to the ordinary
		// period namespace. This migration path keeps device-era sessions from
		// failing when first observed after session mode is enabled.
		ids.forkedFromThreadID, err = s.resolveCodexHTTPCurrentThread(ctx, c, selection, userScope, accountScope, input.forkedFromThreadID, false)
		if err != nil {
			return nil, err
		}
	}
	turnID := input.turnID
	if turnID == "" {
		generated, err := uuid.NewV7()
		if err != nil {
			return nil, fmt.Errorf("generate Codex UUIDv7 turn identity: %w", err)
		}
		turnID = generated.String()
	}
	ids.mode = codexFingerprintSession
	ids.httpSessionIdentity = true
	ids.sessionID = selection.id
	if selection.forkThreadID != "" {
		ids.forkedFromThreadID = selection.forkThreadID
	}
	ids.threadID, err = s.resolveCodexHTTPCurrentThread(ctx, c, selection, userScope, accountScope, task, false)
	if err != nil {
		return nil, err
	}
	historyKey := codexHTTPThreadKey("thread-history", userScope, accountScope, "", task)
	if err := s.recordCodexHTTPThreadHistory(ctx, historyKey, codexHTTPThreadHistory{
		SessionID: selection.id, ThreadID: ids.threadID, ObservedAtMs: now.UnixMilli(),
	}); err != nil {
		return nil, err
	}
	if selection.sideKey != "" {
		if err := s.observeCodexHTTPSide(ctx, selection.sideKey, selection.id, now); err != nil {
			return nil, err
		}
	}
	ids.windowID = ids.threadID + ":0"
	ids.turnID = turnID
	ids.parentTurnID = input.parentTurnID
	ids.rootTurnID = input.rootTurnID
	ids.turnStartedAtUnixMs = now.UnixMilli()
	if input.turnStartedAtUnixMs != nil {
		ids.turnStartedAtUnixMs = *input.turnStartedAtUnixMs
	}
	ids.promptCacheKey = codexHTTPPromptCacheKey(selection.cacheNamespace, input, task)
	return ids, nil
}
