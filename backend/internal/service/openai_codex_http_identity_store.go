package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
)

// Side sessions and thread identities outlive period sessions. Their keys
// deliberately exclude epoch and API key when an authenticated user is known.
func codexHTTPIdentityMappingKey(kind, userScope, accountScope, raw string) string {
	canonical, _ := json.Marshal([]string{userScope, accountScope, raw})
	digest := sha256.Sum256(canonical)
	return fmt.Sprintf("v3:%s:%x", kind, digest[:])
}

// Missing records are not cached in the request context: another instance may
// register a side root or win a thread SetNX while this request is in flight.
func (s *OpenAIGatewayService) lookupCodexHTTPIdentityMapping(ctx context.Context, c *gin.Context, key string) (string, error) {
	if mapped := stagedCodexSessionIdentity(c, key); mapped != "" {
		return mapped, nil
	}
	if s == nil {
		return "", ErrCodexSessionIdentityStoreUnavailable
	}
	store, ok := s.cache.(codexSessionIdentityStore)
	if !ok || store == nil {
		return "", ErrCodexSessionIdentityStoreUnavailable
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

// Preserve the first selected thread projection, including across period
// boundaries and independent gateway instances. A parent/fork reference uses
// exactly this same registry as the thread itself; it never recalculates an
// existing parent's identity from the caller's current epoch.
func (s *OpenAIGatewayService) resolveCodexHTTPThreadMapping(ctx context.Context, c *gin.Context, key, candidate string) (string, error) {
	value, err := s.lookupCodexHTTPIdentityMapping(ctx, c, key)
	if err == nil || !errors.Is(err, ErrCodexSessionIdentityNotFound) {
		return value, err
	}
	store := s.cache.(codexSessionIdentityStore)
	created, err := store.SetCodexSessionIdentityIfAbsent(ctx, key, candidate, 0)
	if err != nil {
		return "", fmt.Errorf("create Codex HTTP thread mapping: %w", err)
	}
	if created {
		stageCodexSessionIdentity(c, key, candidate)
		return candidate, nil
	}
	return s.lookupCodexHTTPIdentityMapping(ctx, c, key)
}
