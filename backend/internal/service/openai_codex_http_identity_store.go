package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// The existing side registrations and immutable v3 thread records remain
// evidence of identities already selected. New thread state has distinct keys.
func codexHTTPIdentityMappingKey(kind, userScope, accountScope, raw string) string {
	canonical, _ := json.Marshal([]string{userScope, accountScope, raw})
	digest := sha256.Sum256(canonical)
	return fmt.Sprintf("v3:%s:%x", kind, digest[:])
}

func codexHTTPThreadKey(kind, userScope, accountScope, sessionID, raw string) string {
	canonical, _ := json.Marshal([]string{userScope, accountScope, sessionID, raw})
	digest := sha256.Sum256(canonical)
	return fmt.Sprintf("v4:%s:%x", kind, digest[:])
}

func codexHTTPSessionThreadID(sessionID, raw string) string {
	canonical, _ := json.Marshal([]string{sessionID, raw})
	return deriveStableUUIDv4("sub2api:codex-thread:v4:" + string(canonical))
}

// CAS is needed only for the latest historical projection, never for v2 or the
// fixed-period UUIDv7 session mapping. Stale requests must not move history back.
type codexHTTPIdentityCASStore interface {
	codexSessionIdentityStore
	CompareAndSwapCodexSessionIdentity(ctx context.Context, key, expected, value string, ttl time.Duration) (bool, error)
}

func (s *OpenAIGatewayService) codexHTTPIdentityStore() (codexSessionIdentityStore, error) {
	if s != nil {
		if store, ok := s.cache.(codexSessionIdentityStore); ok && store != nil {
			return store, nil
		}
	}
	return nil, ErrCodexSessionIdentityStoreUnavailable
}

func (s *OpenAIGatewayService) lookupCodexHTTPIdentityMapping(ctx context.Context, c *gin.Context, key string) (string, error) {
	if mapped := stagedCodexSessionIdentity(c, key); mapped != "" {
		return mapped, nil
	}
	store, err := s.codexHTTPIdentityStore()
	if err != nil {
		return "", err
	}
	value, err := store.GetCodexSessionIdentity(ctx, key)
	if err != nil {
		return "", fmt.Errorf("read Codex HTTP identity mapping: %w", err)
	}
	value = strings.TrimSpace(value)
	if !isCodexUUID(value) {
		return "", fmt.Errorf("invalid Codex HTTP identity mapping value")
	}
	stageCodexSessionIdentity(c, key, value)
	return value, nil
}

// A presence record belongs to one final session. Normal records expire with
// that period; side records remain available for that side's children.
func (s *OpenAIGatewayService) registerCodexHTTPThread(ctx context.Context, c *gin.Context, key, candidate string, ttl time.Duration) (string, error) {
	value, err := s.lookupCodexHTTPIdentityMapping(ctx, c, key)
	if err == nil || !errors.Is(err, ErrCodexSessionIdentityNotFound) {
		return value, err
	}
	store, _ := s.codexHTTPIdentityStore()
	created, err := store.SetCodexSessionIdentityIfAbsent(ctx, key, candidate, ttl)
	if err != nil {
		return "", fmt.Errorf("register Codex HTTP thread: %w", err)
	}
	if created {
		stageCodexSessionIdentity(c, key, candidate)
		return candidate, nil
	}
	return s.lookupCodexHTTPIdentityMapping(ctx, c, key)
}

type codexHTTPThreadHistory struct {
	SessionID    string `json:"session_id"`
	ThreadID     string `json:"thread_id"`
	ObservedAtMs int64  `json:"observed_at_ms"`
}

func decodeCodexHTTPThreadHistory(raw string) (codexHTTPThreadHistory, error) {
	var record codexHTTPThreadHistory
	if err := json.Unmarshal([]byte(raw), &record); err != nil || !isCodexUUIDv7(record.SessionID) || !isCodexUUID(record.ThreadID) {
		return record, fmt.Errorf("invalid Codex thread history record")
	}
	return record, nil
}

// Keep only the newest resolved projection per raw thread, not an unbounded
// list of epochs. The request's captured clock is its ordering token: an old
// epoch's delayed request cannot overwrite the next epoch's history.
func (s *OpenAIGatewayService) recordCodexHTTPThreadHistory(ctx context.Context, key string, record codexHTTPThreadHistory) error {
	store, err := s.codexHTTPIdentityStore()
	if err != nil {
		return err
	}
	cas, ok := store.(codexHTTPIdentityCASStore)
	if !ok {
		return ErrCodexSessionIdentityStoreUnavailable
	}
	encoded, _ := json.Marshal(record)
	for attempt := 0; attempt < 16; attempt++ {
		previous, err := store.GetCodexSessionIdentity(ctx, key)
		if err != nil && !errors.Is(err, ErrCodexSessionIdentityNotFound) {
			return fmt.Errorf("read Codex thread history: %w", err)
		}
		if err == nil {
			current, err := decodeCodexHTTPThreadHistory(previous)
			if err != nil {
				return err
			}
			if current.ObservedAtMs >= record.ObservedAtMs || (current.SessionID == record.SessionID && current.ThreadID == record.ThreadID) {
				return nil
			}
		} else {
			previous = ""
		}
		updated, err := cas.CompareAndSwapCodexSessionIdentity(ctx, key, previous, string(encoded), 0)
		if err != nil {
			return fmt.Errorf("update Codex thread history: %w", err)
		}
		if updated {
			return nil
		}
	}
	return fmt.Errorf("Codex thread history update contention")
}

func (s *OpenAIGatewayService) lookupCodexHTTPForkSource(ctx context.Context, c *gin.Context, userScope, accountScope, raw string) (string, error) {
	store, err := s.codexHTTPIdentityStore()
	if err != nil {
		return "", err
	}
	key := codexHTTPThreadKey("thread-history", userScope, accountScope, "", raw)
	value, err := store.GetCodexSessionIdentity(ctx, key)
	if err == nil {
		record, err := decodeCodexHTTPThreadHistory(value)
		return record.ThreadID, err
	}
	if !errors.Is(err, ErrCodexSessionIdentityNotFound) {
		return "", fmt.Errorf("read Codex fork history: %w", err)
	}
	// These immutable records are historical evidence, never candidates for a
	// normal request's current thread. No record means no guessed fork target.
	return s.lookupCodexHTTPIdentityMapping(ctx, c, codexHTTPIdentityMappingKey("thread", userScope, accountScope, raw))
}

type codexHTTPSideFork struct {
	SourceKey         string `json:"source_key"`
	ThreadID          string `json:"thread_id"`
	PreserveV3Threads bool   `json:"preserve_v3_threads,omitempty"`
}

func readCodexHTTPSideFork(ctx context.Context, store codexSessionIdentityStore, key string) (codexHTTPSideFork, error) {
	value, err := store.GetCodexSessionIdentity(ctx, key)
	if err != nil {
		return codexHTTPSideFork{}, err
	}
	var fork codexHTTPSideFork
	if json.Unmarshal([]byte(value), &fork) != nil || !isCodexUUID(fork.ThreadID) || fork.SourceKey == "" {
		return fork, fmt.Errorf("invalid Codex side fork record")
	}
	return fork, nil
}

func (s *OpenAIGatewayService) pinCodexHTTPSideFork(ctx context.Context, c *gin.Context, userScope, accountScope, rawSession, rawFork string, existingSide bool) (codexHTTPSideFork, error) {
	store, err := s.codexHTTPIdentityStore()
	if err != nil {
		return codexHTTPSideFork{}, err
	}
	key := codexHTTPThreadKey("side-fork", userScope, accountScope, "", rawSession)
	sourceKey := codexHTTPIdentityMappingKey("thread", userScope, accountScope, rawFork)
	read := func() (codexHTTPSideFork, error) {
		fork, err := readCodexHTTPSideFork(ctx, store, key)
		if err == nil && fork.SourceKey != sourceKey {
			return fork, fmt.Errorf("conflicting Codex side fork source")
		}
		return fork, err
	}
	if value, err := read(); err == nil || !errors.Is(err, ErrCodexSessionIdentityNotFound) {
		return value, err
	}
	var target string
	if existingSide {
		// A side created before fork pinning used the immutable v3 target.
		// Newer ordinary history must not retarget that already existing side.
		target, err = s.lookupCodexHTTPIdentityMapping(ctx, c, sourceKey)
	} else {
		target, err = s.lookupCodexHTTPForkSource(ctx, c, userScope, accountScope, rawFork)
	}
	if err != nil {
		return codexHTTPSideFork{}, fmt.Errorf("resolve Codex fork source: %w", err)
	}
	fork := codexHTTPSideFork{SourceKey: sourceKey, ThreadID: target, PreserveV3Threads: existingSide}
	encoded, _ := json.Marshal(fork)
	created, err := store.SetCodexSessionIdentityIfAbsent(ctx, key, string(encoded), 0)
	if err != nil {
		return codexHTTPSideFork{}, fmt.Errorf("pin Codex side fork: %w", err)
	}
	if created {
		return fork, nil
	}
	return read()
}
