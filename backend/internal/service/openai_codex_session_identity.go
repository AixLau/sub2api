package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const codexSessionIdentityMappingVersion = "v2"

// CodexSessionIdentityMappingV2 is the durable mapping strategy used for
// newly-created UUIDv7 session identifiers. The legacy strategy remains
// available as a configuration rollback value.
const (
	CodexSessionIdentityMappingV2     = "v2"
	CodexSessionIdentityMappingLegacy = "legacy"
)

const codexSessionIdentityContextKey = "codex_session_identity_mappings"

var (
	// ErrCodexSessionIdentityNotFound distinguishes a missing mapping from a
	// failed durable store read. A missing value is only expected immediately
	// before the atomic create path.
	ErrCodexSessionIdentityNotFound         = errors.New("Codex session identity mapping not found")
	ErrCodexSessionIdentityStoreUnavailable = errors.New("Codex session identity store unavailable")
)

// codexSessionIdentityStore is implemented by the durable gateway cache. It is
// intentionally optional in the service interface so existing cache test
// doubles and non-Redis deployments keep compiling; production must provide it
// before enabling the v2 session mapping path.
type codexSessionIdentityStore interface {
	GetCodexSessionIdentity(ctx context.Context, key string) (string, error)
	SetCodexSessionIdentityIfAbsent(ctx context.Context, key, value string) (bool, error)
}

func isCodexUUIDv7(value string) bool {
	parsed, err := uuid.Parse(strings.TrimSpace(value))
	return err == nil && parsed.Version() == uuid.Version(7) && parsed.Variant() == uuid.RFC4122
}

func isCodexUUID(value string) bool {
	parsed, err := uuid.Parse(strings.TrimSpace(value))
	return err == nil && parsed.Variant() == uuid.RFC4122
}

func codexSessionIdentityDownstreamScope(c *gin.Context, apiKeyID int64) string {
	userID := int64(0)
	if c != nil && c.Request != nil {
		userID, _ = c.Request.Context().Value(ctxkey.UserID).(int64)
	}
	if userID <= 0 && c != nil {
		if value, ok := c.Get("api_key"); ok {
			if apiKey, ok := value.(*APIKey); ok && apiKey != nil {
				userID = apiKey.UserID
			}
		}
	}
	return fmt.Sprintf("user:%d:api-key:%d", userID, apiKeyID)
}

func codexSessionIdentityUpstreamScope(account *Account) string {
	if account == nil {
		return ""
	}
	if namespace := strings.TrimSpace(codexAccountIdentityNamespace(account)); namespace != "" {
		return namespace
	}
	// API-key accounts do not expose the ChatGPT namespace. Include the
	// selected account row and platform so a failover cannot reuse a mapping
	// belonging to another upstream credential.
	return fmt.Sprintf("platform:%s:account:%d", strings.TrimSpace(account.Platform), account.ID)
}

func codexSessionIdentityMappingKey(c *gin.Context, account *Account, apiKeyID int64, raw string) string {
	canonical := strings.Join([]string{
		codexSessionIdentityMappingVersion,
		codexSessionIdentityDownstreamScope(c, apiKeyID),
		codexSessionIdentityUpstreamScope(account),
		strings.TrimSpace(raw),
	}, "\x00")
	digest := sha256.Sum256([]byte(canonical))
	return fmt.Sprintf("%x", digest[:])
}

func codexSessionIdentityIsolationRequired(c *gin.Context, account *Account, apiKeyID int64) bool {
	if account == nil {
		return false
	}
	return apiKeyID > 0 || strings.TrimSpace(codexAccountIdentityNamespace(account)) != ""
}

func stagedCodexSessionIdentity(c *gin.Context, key string) string {
	if c == nil {
		return ""
	}
	value, ok := c.Get(codexSessionIdentityContextKey)
	mappings, ok := value.(map[string]string)
	if !ok || mappings == nil {
		return ""
	}
	return strings.TrimSpace(mappings[strings.TrimSpace(key)])
}

func stageCodexSessionIdentity(c *gin.Context, key, mapped string) {
	if c == nil || strings.TrimSpace(key) == "" || strings.TrimSpace(mapped) == "" {
		return
	}
	value, _ := c.Get(codexSessionIdentityContextKey)
	mappings, _ := value.(map[string]string)
	if mappings == nil {
		mappings = make(map[string]string)
		c.Set(codexSessionIdentityContextKey, mappings)
	}
	mappings[strings.TrimSpace(key)] = strings.TrimSpace(mapped)
}

// resolveCodexMappedSessionIdentity returns a UUIDv7 only for newly created
// mappings of official UUIDv7 sessions. Existing non-v7 identifiers continue
// through the legacy mapping so an active pre-migration session cannot change
// identity mid-conversation.
func (s *OpenAIGatewayService) resolveCodexMappedSessionIdentity(ctx context.Context, c *gin.Context, account *Account, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	account = codexAccountIdentitySource(c, account)
	if !s.codexSessionIdentityMappingEnabled() {
		return raw, nil
	}
	if raw == "" || !codexSessionIdentityIsolationRequired(c, account, getAPIKeyIDFromContext(c)) {
		return raw, nil
	}
	if !isCodexUUIDv7(raw) {
		// Non-v7 values belong to the pre-migration path. The surrounding
		// builders already applied the legacy deterministic isolation where it
		// is required; mapping them again here would change an active session.
		return raw, nil
	}
	key := codexSessionIdentityMappingKey(c, codexAccountIdentitySource(c, account), getAPIKeyIDFromContext(c), raw)
	if mapped := stagedCodexSessionIdentity(c, key); mapped != "" {
		return mapped, nil
	}
	if s == nil || s.cache == nil {
		return "", ErrCodexSessionIdentityStoreUnavailable
	}
	store, ok := s.cache.(codexSessionIdentityStore)
	if !ok || store == nil {
		return "", ErrCodexSessionIdentityStoreUnavailable
	}
	if value, err := store.GetCodexSessionIdentity(ctx, key); err == nil {
		value = strings.TrimSpace(value)
		if !isCodexUUIDv7(value) {
			return "", fmt.Errorf("invalid Codex session identity mapping value")
		}
		stageCodexSessionIdentity(c, key, value)
		mappedKey := codexSessionIdentityMappingKey(c, account, getAPIKeyIDFromContext(c), value)
		stageCodexSessionIdentity(c, mappedKey, value)
		return value, nil
	} else if !errors.Is(err, ErrCodexSessionIdentityNotFound) {
		return "", fmt.Errorf("read Codex session identity mapping: %w", err)
	}
	candidate, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("generate Codex UUIDv7 session identity: %w", err)
	}
	created, err := store.SetCodexSessionIdentityIfAbsent(ctx, key, candidate.String())
	if err != nil {
		return "", fmt.Errorf("create Codex session identity mapping: %w", err)
	}
	if created {
		stageCodexSessionIdentity(c, key, candidate.String())
		mappedKey := codexSessionIdentityMappingKey(c, account, getAPIKeyIDFromContext(c), candidate.String())
		stageCodexSessionIdentity(c, mappedKey, candidate.String())
		return candidate.String(), nil
	}
	value, err := store.GetCodexSessionIdentity(ctx, key)
	if err != nil {
		return "", fmt.Errorf("read Codex session identity mapping: %w", err)
	}
	value = strings.TrimSpace(value)
	if !isCodexUUIDv7(value) {
		return "", fmt.Errorf("invalid Codex session identity mapping value")
	}
	stageCodexSessionIdentity(c, key, value)
	mappedKey := codexSessionIdentityMappingKey(c, account, getAPIKeyIDFromContext(c), value)
	stageCodexSessionIdentity(c, mappedKey, value)
	return value, nil
}

func (s *OpenAIGatewayService) codexSessionIdentityMappingEnabled() bool {
	if s == nil || s.cfg == nil {
		// Unit-level builders commonly construct a service without a loaded
		// Config. Keep the explicit v2 path active there; production config
		// loading supplies the default and the rollback value.
		return true
	}
	mode := strings.ToLower(strings.TrimSpace(s.cfg.Gateway.CodexSessionIdentityMapping))
	return mode == "" || mode == CodexSessionIdentityMappingV2
}

func (s *OpenAIGatewayService) normalizeCodexSessionHeaders(ctx context.Context, c *gin.Context, account *Account, headers http.Header) error {
	if headers == nil || account == nil || !account.UsesOpenAICodexProtocol() {
		return nil
	}
	raw := codexFirstIdentityValue(headers.Get("session-id"), headers.Get("session_id"))
	if raw == "" {
		return nil
	}
	mapped, err := s.resolveCodexMappedSessionIdentity(ctx, c, account, raw)
	if err != nil {
		return err
	}
	if mapped == "" {
		return nil
	}
	headers.Set("session-id", mapped)
	headers.Set("session_id", mapped)
	return nil
}
