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

func (p codexSessionPeriod) threadID(task string) string {
	if task == "" {
		return ""
	}
	return deriveStableUUIDv4("sub2api:codex-thread:v3:" + p.key + "\x00" + task)
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

func (s *OpenAIGatewayService) resolveCodexHTTPSession(ctx context.Context, c *gin.Context, input *codexSessionIdentityInput, userScope, accountScope string, period codexSessionPeriod, now time.Time) (sessionID, cacheNamespace string, err error) {
	if input.originalSessionID != "" {
		sideKey := codexHTTPIdentityMappingKey("side-session", userScope, accountScope, input.originalSessionID)
		// Continuations and children often omit forked_from_thread_id. An
		// existing side registration always takes precedence over compression.
		side, lookupErr := s.lookupCodexHTTPIdentityMapping(ctx, c, sideKey)
		if lookupErr == nil {
			if !isCodexUUIDv7(side) {
				return "", "", fmt.Errorf("invalid Codex side session mapping value")
			}
			return side, sideKey, nil
		}
		if !errors.Is(lookupErr, ErrCodexSessionIdentityNotFound) {
			return "", "", lookupErr
		}
		if input.forkedFromThreadID != "" {
			side, err := s.resolveCodexSessionIdentityMapping(ctx, c, sideKey, 0)
			return side, sideKey, err
		}
	} else if input.forkedFromThreadID != "" {
		return "", "", fmt.Errorf("Codex side session requires an original session_id")
	}
	session, err := s.resolveCodexSessionIdentityMapping(ctx, c, period.key, period.expiresAt.Sub(now))
	return session, period.key, err
}

// resolveCodexHTTPFingerprintIDs is called once per HTTP attempt, before either
// carrier is projected. Only session mode needs authenticated scope and a store.
// Missing task/user identity conservatively retains device convergence.
func (s *OpenAIGatewayService) resolveCodexHTTPFingerprintIDs(ctx context.Context, c *gin.Context, account *Account, now time.Time) (*codexFingerprintIDs, error) {
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
	period := resolveCodexSessionPeriod(ids.seed, userScope, accountScope, now)
	for _, reference := range []struct {
		raw    string
		target *string
	}{{input.parentThreadID, &ids.parentThreadID}, {input.forkedFromThreadID, &ids.forkedFromThreadID}} {
		if reference.raw == "" {
			continue
		}
		key := codexHTTPIdentityMappingKey("thread", userScope, accountScope, reference.raw)
		mapped, err := s.lookupCodexHTTPIdentityMapping(ctx, c, key)
		if err != nil {
			// References only read records created for actual request threads.
			// Do not reserve a guessed parent ID that a later /side could
			// mistake for a previously used thread.
			return nil, fmt.Errorf("resolve Codex referenced thread: %w", err)
		}
		*reference.target = mapped
	}
	sessionID, cacheNamespace, err := s.resolveCodexHTTPSession(ctx, c, input, userScope, accountScope, period, now)
	if err != nil {
		return nil, err
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
	ids.sessionID = sessionID
	key := codexHTTPIdentityMappingKey("thread", userScope, accountScope, task)
	ids.threadID, err = s.resolveCodexHTTPThreadMapping(ctx, c, key, period.threadID(task))
	if err != nil {
		return nil, err
	}
	ids.windowID = ids.threadID + ":0"
	ids.turnID = turnID
	ids.parentTurnID = input.parentTurnID
	ids.rootTurnID = input.rootTurnID
	ids.turnStartedAtUnixMs = now.UnixMilli()
	if input.turnStartedAtUnixMs != nil {
		ids.turnStartedAtUnixMs = *input.turnStartedAtUnixMs
	}
	ids.promptCacheKey = codexHTTPPromptCacheKey(cacheNamespace, input, task)
	return ids, nil
}
