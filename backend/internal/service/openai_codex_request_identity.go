package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const codexSessionIdentityInputContextKey = "codex_session_identity_input"

// codexSessionIdentityInput records the client's original task/turn graph and
// session/cache relationship
// before any account or fingerprint rewrite. It is replaced for every attempt
// and every incoming WS frame; mapped values never become the next turn's input.
// HTTP session v3 preserves the turn graph and maps cache by the original root.
// Modes that do not own sessions retain their existing session/cache binding.
type codexSessionIdentityInput struct {
	sessionID                         string
	originalSessionID                 string
	threadID                          string
	parentThreadID                    string
	forkedFromThreadID                string
	forkedFromOrdinalExclusivePresent bool
	turnID                            string
	parentTurnID                      string
	rootTurnID                        string
	turnStartedAtUnixMs               *int64
	clientRequestID                   string
	promptCacheKey                    string
	promptCacheKeyReferencesSession   bool
	parentReferencePresent            bool
}

func stageCodexSessionIdentityInputMap(c *gin.Context, body map[string]any) {
	if c == nil || c.Request == nil {
		return
	}
	cacheKey := codexIdentityString(body["prompt_cache_key"])
	clientMetadata := codexIdentityMetadataMap(body["client_metadata"])
	identity := resolveCodexRequestIdentity(c.Request.Header, clientMetadata, cacheKey)
	// x-client-request-id is only a last resort task identity, after the
	// explicit thread and session. Do not let the general carrier resolver's
	// thread fallback outrank the original session here.
	taskHeaders := c.Request.Header.Clone()
	taskHeaders.Del("x-client-request-id")
	taskIdentity := resolveCodexRequestIdentity(taskHeaders, clientMetadata, "")
	// Preserve the client's complete turn graph before account scoping can
	// rewrite turn_id. This capture is only projected by HTTP session v3.
	metadataField := func(name string) string {
		return codexFirstIdentityValue(
			codexIdentityString(clientMetadata[name]),
			codexIdentityString(taskIdentity.bodyTurnMetadata[name]),
			codexIdentityString(taskIdentity.headerTurnMetadata[name]),
		)
	}
	metadataFieldPresent := func(name string) bool {
		if _, ok := clientMetadata[name]; ok {
			return true
		}
		if _, ok := taskIdentity.bodyTurnMetadata[name]; ok {
			return true
		}
		if _, ok := taskIdentity.headerTurnMetadata[name]; ok {
			return true
		}
		return false
	}
	c.Set(codexSessionIdentityInputContextKey, &codexSessionIdentityInput{
		sessionID:         identity.sessionID,
		originalSessionID: taskIdentity.sessionID,
		threadID:          taskIdentity.threadID,
		parentThreadID: codexFirstIdentityValue(
			c.Request.Header.Get("x-codex-parent-thread-id"),
			codexIdentityString(clientMetadata["x-codex-parent-thread-id"]),
			codexIdentityString(clientMetadata["parent_thread_id"]),
			codexIdentityString(taskIdentity.bodyTurnMetadata["parent_thread_id"]),
			codexIdentityString(taskIdentity.headerTurnMetadata["parent_thread_id"]),
		),
		forkedFromThreadID:                metadataField("forked_from_thread_id"),
		forkedFromOrdinalExclusivePresent: metadataFieldPresent("forked_from_ordinal_exclusive"),
		turnID: codexFirstIdentityValue(metadataField("turn_id"),
			metadataField("turn-id"), c.Request.Header.Get("turn-id"), c.Request.Header.Get("turn_id")),
		parentTurnID:                    metadataField("parent_turn_id"),
		rootTurnID:                      metadataField("root_turn_id"),
		turnStartedAtUnixMs:             codexOriginalTurnStartedAt(clientMetadata, taskIdentity),
		clientRequestID:                 strings.TrimSpace(c.Request.Header.Get("x-client-request-id")),
		promptCacheKey:                  cacheKey,
		promptCacheKeyReferencesSession: cacheKey != "" && cacheKey == identity.sessionID,
		parentReferencePresent:          identity.parentThreadID != "" || codexFingerprintParentReferencePresent(clientMetadata),
	})
}

func codexOriginalTurnStartedAt(metadata map[string]any, identity codexRequestIdentitySnapshot) *int64 {
	// Read nested JSON directly so capturing the timestamp does not round an
	// integer through the general metadata resolver's float64 representation.
	flat, _ := json.Marshal(metadata["turn_started_at_unix_ms"])
	for _, raw := range []string{
		string(flat),
		gjson.Get(identity.bodyTurnMetadataRaw, "turn_started_at_unix_ms").Raw,
		gjson.Get(identity.headerTurnMetadataRaw, "turn_started_at_unix_ms").Raw,
	} {
		if value, ok := parseCodexTurnStartedAtJSON(raw); ok {
			return &value
		}
	}
	return nil
}

func parseCodexTurnStartedAtJSON(raw string) (int64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return 0, false
	}
	if strings.HasPrefix(raw, `"`) {
		var text string
		if err := json.Unmarshal([]byte(raw), &text); err != nil {
			return 0, false
		}
		raw = strings.TrimSpace(text)
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	return value, err == nil
}

func stageCodexSessionIdentityInputRaw(c *gin.Context, body []byte) {
	metadata := map[string]any{}
	if raw := gjson.GetBytes(body, "client_metadata"); raw.IsObject() {
		_ = decodeOpenAIJSONUseNumber([]byte(raw.Raw), &metadata)
	}
	stageCodexSessionIdentityInputMap(c, map[string]any{
		"client_metadata":  metadata,
		"prompt_cache_key": gjson.GetBytes(body, "prompt_cache_key").Value(),
	})
}

func stagedCodexSessionIdentityInput(c *gin.Context) *codexSessionIdentityInput {
	if c == nil {
		return nil
	}
	value, _ := c.Get(codexSessionIdentityInputContextKey)
	input, _ := value.(*codexSessionIdentityInput)
	return input
}

// codexOriginalSessionID is used only before header isolation. The captured
// input precedes account/body rewrites; the fallback is for constructors with
// no captured body input. A cache key never outranks an explicit session.
func codexOriginalSessionID(c *gin.Context, fallbackSession string) string {
	if input := stagedCodexSessionIdentityInput(c); input != nil && input.sessionID != "" {
		return input.sessionID
	}
	var headers http.Header
	if c != nil && c.Request != nil {
		headers = c.Request.Header
	}
	return resolveCodexRequestIdentity(headers, nil, fallbackSession).sessionID
}

// codexRequestIdentitySnapshot is the one request-scoped identity snapshot shared by
// all Codex carriers. The same values are projected to compatibility headers,
// flat client_metadata, and the nested x-codex-turn-metadata object.
type codexRequestIdentitySnapshot struct {
	installationID        string
	sessionID             string
	threadID              string
	windowID              string
	turnID                string
	parentThreadID        string
	turnMetadataRaw       string
	bodyTurnMetadata      map[string]any
	headerTurnMetadata    map[string]any
	bodyTurnMetadataRaw   string
	headerTurnMetadataRaw string
}

func (i codexRequestIdentitySnapshot) empty() bool {
	return i.installationID == "" &&
		i.sessionID == "" &&
		i.threadID == "" &&
		i.windowID == "" &&
		i.turnID == "" &&
		i.parentThreadID == "" &&
		i.turnMetadataRaw == ""
}

func codexIdentityString(value any) string {
	if value == nil {
		return ""
	}
	valueString, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(valueString)
}

func codexIdentityMetadataMap(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case map[string]string:
		result := make(map[string]any, len(typed))
		for key, value := range typed {
			result[key] = value
		}
		return result
	default:
		return nil
	}
}

func codexIdentityNestedMetadata(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" || !gjson.Valid(raw) {
		return nil
	}
	metadata := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil || metadata == nil {
		return nil
	}
	return metadata
}

func codexFirstIdentityValue(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

// resolveCodexRequestIdentity chooses standard hyphenated headers first,
// then flat client_metadata, legacy underscore headers, nested turn metadata,
// and finally the prompt-cache/session fallback, regardless of UUID version.
// Final coordination uses the captured raw input for pending UUIDv7 mapping;
// it must not guess whether a candidate was mapped from its UUID shape.
func resolveCodexRequestIdentity(headers http.Header, clientMetadata map[string]any, fallbackSession string) codexRequestIdentitySnapshot {
	bodyNestedRaw := codexIdentityString(clientMetadata[openAIWSTurnMetadataHeader])
	bodyNested := codexIdentityNestedMetadata(bodyNestedRaw)
	headerNestedRaw := strings.TrimSpace(headers.Get(openAIWSTurnMetadataHeader))
	headerNested := codexIdentityNestedMetadata(headerNestedRaw)

	sessionCandidates := []string{
		headers.Get("session-id"),
		codexIdentityString(clientMetadata["session_id"]),
		headers.Get("session_id"),
		codexIdentityString(bodyNested["session_id"]),
		codexIdentityString(headerNested["session_id"]),
		fallbackSession,
	}
	sessionID := codexFirstIdentityValue(sessionCandidates...)

	return codexRequestIdentitySnapshot{
		installationID: codexFirstIdentityValue(
			headers.Get("x-codex-installation-id"),
			codexIdentityString(clientMetadata["x-codex-installation-id"]),
			codexIdentityString(bodyNested["installation_id"]),
			codexIdentityString(headerNested["installation_id"]),
		),
		sessionID: sessionID,
		threadID: codexFirstIdentityValue(
			headers.Get("thread-id"),
			codexIdentityString(clientMetadata["thread_id"]),
			headers.Get("thread_id"),
			codexIdentityString(bodyNested["thread_id"]),
			codexIdentityString(headerNested["thread_id"]),
			headers.Get("x-client-request-id"),
		),
		windowID: codexFirstIdentityValue(
			headers.Get("x-codex-window-id"),
			codexIdentityString(clientMetadata["x-codex-window-id"]),
			codexIdentityString(bodyNested["window_id"]),
			codexIdentityString(headerNested["window_id"]),
		),
		turnID: codexFirstIdentityValue(
			codexIdentityString(clientMetadata["turn_id"]),
			codexIdentityString(bodyNested["turn_id"]),
			codexIdentityString(headerNested["turn_id"]),
		),
		parentThreadID: codexFirstIdentityValue(
			headers.Get("x-codex-parent-thread-id"),
			codexIdentityString(clientMetadata["x-codex-parent-thread-id"]),
			codexIdentityString(bodyNested["parent_thread_id"]),
			codexIdentityString(headerNested["parent_thread_id"]),
		),
		turnMetadataRaw:       headerNestedRaw,
		bodyTurnMetadata:      bodyNested,
		headerTurnMetadata:    headerNested,
		bodyTurnMetadataRaw:   bodyNestedRaw,
		headerTurnMetadataRaw: headerNestedRaw,
	}
}

func applyCodexOutboundIdentityToHeaders(headers http.Header, identity codexRequestIdentitySnapshot) {
	if headers == nil || identity.empty() {
		return
	}
	if identity.installationID != "" {
		headers.Set("x-codex-installation-id", identity.installationID)
	}
	if identity.sessionID != "" {
		// Keep both forms during the transition. The hyphenated form is the
		// official Codex header; the underscore form is used by older clients.
		headers.Set("session-id", identity.sessionID)
		headers.Set("session_id", identity.sessionID)
	}
	if identity.threadID != "" {
		headers.Set("thread-id", identity.threadID)
		headers.Set("thread_id", identity.threadID)
		headers.Set("x-client-request-id", identity.threadID)
	}
	if identity.windowID != "" {
		headers.Set("x-codex-window-id", identity.windowID)
	}
	if identity.parentThreadID != "" {
		headers.Set("x-codex-parent-thread-id", identity.parentThreadID)
	}
	if identity.turnMetadataRaw != "" {
		headers.Set(openAIWSTurnMetadataHeader, identity.turnMetadataRaw)
	}
}

func applyCodexOutboundIdentityToClientMetadata(clientMetadata map[string]any, identity codexRequestIdentitySnapshot) (string, string, error) {
	if clientMetadata == nil || identity.empty() {
		return "", "", nil
	}
	if identity.installationID != "" {
		clientMetadata["x-codex-installation-id"] = identity.installationID
	}
	if identity.sessionID != "" {
		clientMetadata["session_id"] = identity.sessionID
	}
	if identity.threadID != "" {
		clientMetadata["thread_id"] = identity.threadID
	}
	if identity.windowID != "" {
		clientMetadata["x-codex-window-id"] = identity.windowID
	}
	if identity.turnID != "" {
		clientMetadata["turn_id"] = identity.turnID
	}
	if identity.parentThreadID != "" {
		clientMetadata["x-codex-parent-thread-id"] = identity.parentThreadID
	}

	bodyNested := cloneCodexIdentityMetadata(identity.bodyTurnMetadata)
	if len(bodyNested) == 0 {
		bodyNested = cloneCodexIdentityMetadata(identity.headerTurnMetadata)
	}
	headerNested := cloneCodexIdentityMetadata(identity.headerTurnMetadata)
	// Body metadata is the current turn's full snapshot. Retain independent
	// header-only extensions, but project current body fields over stale defaults.
	for key, value := range identity.bodyTurnMetadata {
		headerNested[key] = value
	}
	headerNested = codexCompatibilityTurnMetadata(headerNested)
	if len(bodyNested) == 0 && identity.bodyTurnMetadataRaw != "" {
		bodyNested = nil
	}
	if len(headerNested) == 0 && identity.headerTurnMetadataRaw != "" {
		headerNested = nil
	}
	bodyMetadataInvalid := identity.bodyTurnMetadataRaw != "" && identity.bodyTurnMetadata == nil
	headerMetadataInvalid := identity.headerTurnMetadataRaw != "" && identity.headerTurnMetadata == nil
	applyCodexIdentityFieldsToNestedMetadata(bodyNested, identity)
	applyCodexIdentityFieldsToNestedMetadata(headerNested, identity)

	encode := func(metadata map[string]any) (string, error) {
		if len(metadata) == 0 {
			return "", nil
		}
		encoded, err := json.Marshal(metadata)
		if err != nil {
			return "", fmt.Errorf("encode codex turn metadata: %w", err)
		}
		return string(encoded), nil
	}
	bodyRaw, err := encode(bodyNested)
	if err != nil {
		return "", "", err
	}
	if bodyRaw == "" && identity.bodyTurnMetadataRaw != "" {
		bodyRaw = identity.bodyTurnMetadataRaw
	}
	headerRaw, err := encode(headerNested)
	if err != nil {
		return "", "", err
	}
	if headerRaw == "" && identity.headerTurnMetadataRaw != "" {
		headerRaw = identity.headerTurnMetadataRaw
	}
	if bodyMetadataInvalid {
		bodyRaw = identity.bodyTurnMetadataRaw
	}
	if headerMetadataInvalid {
		headerRaw = identity.headerTurnMetadataRaw
	}
	if bodyRaw != "" {
		clientMetadata[openAIWSTurnMetadataHeader] = bodyRaw
	}
	return bodyRaw, headerRaw, nil
}

func cloneCodexIdentityMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}

// codexCompatibilityTurnMetadata projects the selected official metadata
// shape onto the bounded compatibility header. The full body projection keeps
// tool_namespaces_info; the direct header omits it.
func codexCompatibilityTurnMetadata(metadata map[string]any) map[string]any {
	projected := cloneCodexIdentityMetadata(metadata)
	delete(projected, "tool_namespaces_info")
	return projected
}

func applyCodexIdentityFieldsToNestedMetadata(metadata map[string]any, identity codexRequestIdentitySnapshot) {
	if metadata == nil {
		return
	}
	if identity.installationID != "" {
		metadata["installation_id"] = identity.installationID
	}
	if identity.sessionID != "" {
		metadata["session_id"] = identity.sessionID
	}
	if identity.threadID != "" {
		metadata["thread_id"] = identity.threadID
	}
	if identity.windowID != "" {
		metadata["window_id"] = identity.windowID
	}
	if identity.turnID != "" {
		metadata["turn_id"] = identity.turnID
	}
	if identity.parentThreadID != "" {
		metadata["parent_thread_id"] = identity.parentThreadID
	}
}

// normalizeCodexOutboundIdentityMap synchronizes a decoded request body and
// its final outbound headers. It is intentionally called late in each builder,
// after account/fingerprint transforms, so it cannot reintroduce stale values.
type codexSessionIdentityMapper func(string) (string, error)

func normalizeCodexOutboundIdentityMap(headers http.Header, body map[string]any, fallbackSession string) (codexRequestIdentitySnapshot, bool, error) {
	return normalizeCodexOutboundIdentityMapWithSessionMapper(headers, body, fallbackSession, nil)
}

func normalizeCodexOutboundIdentityMapWithSessionMapper(headers http.Header, body map[string]any, fallbackSession string, mapSession codexSessionIdentityMapper) (codexRequestIdentitySnapshot, bool, error) {
	return normalizeCodexOutboundIdentityMapFromInput(headers, body, fallbackSession, nil, mapSession)
}

func (s *OpenAIGatewayService) normalizeCodexOutboundIdentityMap(ctx context.Context, c *gin.Context, account *Account, headers http.Header, body map[string]any, fallbackSession string) (codexRequestIdentitySnapshot, bool, error) {
	if ids := stagedCodexFingerprintIDs(c, account); ids != nil && ids.httpSessionIdentity {
		applyCodexFingerprintHeaders(headers, ids)
		applyCodexFingerprintClientMetadata(body, ids)
		return normalizeCodexOutboundIdentityMapFromInput(headers, body, fallbackSession, nil, nil)
	}
	return normalizeCodexOutboundIdentityMapFromInput(headers, body, fallbackSession, stagedCodexSessionIdentityInput(c), func(raw string) (string, error) {
		return s.resolveCodexMappedSessionIdentity(ctx, c, account, raw)
	})
}

func normalizeCodexOutboundIdentityMapFromInput(headers http.Header, body map[string]any, fallbackSession string, input *codexSessionIdentityInput, mapSession codexSessionIdentityMapper) (codexRequestIdentitySnapshot, bool, error) {
	if body == nil {
		return codexRequestIdentitySnapshot{}, false, nil
	}
	clientMetadata := codexIdentityMetadataMap(body["client_metadata"])
	identity := resolveCodexRequestIdentity(headers, clientMetadata, fallbackSession)
	promptCacheKey, hasPromptCacheKey := body["prompt_cache_key"].(string)
	cacheReferencesSession := strings.TrimSpace(promptCacheKey) != "" && strings.TrimSpace(promptCacheKey) == identity.sessionID
	if input != nil {
		cacheReferencesSession = input.promptCacheKeyReferencesSession
		// Only raw UUIDv7 inputs still need the final session mapper. Preserve
		// the already isolated non-v7 session selected from outbound carriers.
		if isCodexUUIDv7(input.sessionID) {
			identity.sessionID = input.sessionID
		}
	}
	if identity.empty() {
		return identity, false, nil
	}
	if mapSession != nil && identity.sessionID != "" {
		mapped, err := mapSession(identity.sessionID)
		if err != nil {
			return identity, false, err
		}
		identity.sessionID = strings.TrimSpace(mapped)
	}
	if clientMetadata == nil {
		clientMetadata = make(map[string]any)
	}
	_, headerMetadataRaw, err := applyCodexOutboundIdentityToClientMetadata(clientMetadata, identity)
	if err != nil {
		return identity, false, err
	}
	identity.turnMetadataRaw = headerMetadataRaw
	body["client_metadata"] = clientMetadata
	if mapSession != nil && identity.sessionID != "" && hasPromptCacheKey && cacheReferencesSession {
		body["prompt_cache_key"] = identity.sessionID
	}
	applyCodexOutboundIdentityToHeaders(headers, identity)
	return identity, true, nil
}

// normalizeCodexOutboundIdentityRaw performs the same synchronization while
// preserving the rest of a potentially large passthrough body byte-for-byte.
func normalizeCodexOutboundIdentityRaw(headers http.Header, body []byte, fallbackSession string) ([]byte, codexRequestIdentitySnapshot, bool, error) {
	return normalizeCodexOutboundIdentityRawWithSessionMapper(headers, body, fallbackSession, nil)
}

func normalizeCodexOutboundIdentityRawWithSessionMapper(headers http.Header, body []byte, fallbackSession string, mapSession codexSessionIdentityMapper) ([]byte, codexRequestIdentitySnapshot, bool, error) {
	return normalizeCodexOutboundIdentityRawFromInput(headers, body, fallbackSession, nil, mapSession)
}

func (s *OpenAIGatewayService) normalizeCodexOutboundIdentityRaw(ctx context.Context, c *gin.Context, account *Account, headers http.Header, body []byte, fallbackSession string) ([]byte, codexRequestIdentitySnapshot, bool, error) {
	if ids := stagedCodexFingerprintIDs(c, account); ids != nil && ids.httpSessionIdentity {
		applyCodexFingerprintHeaders(headers, ids)
		next, _, err := applyCodexFingerprintClientMetadataRaw(body, ids)
		if err != nil {
			return body, codexRequestIdentitySnapshot{}, false, err
		}
		return normalizeCodexOutboundIdentityRawFromInput(headers, next, fallbackSession, nil, nil)
	}
	return normalizeCodexOutboundIdentityRawFromInput(headers, body, fallbackSession, stagedCodexSessionIdentityInput(c), func(raw string) (string, error) {
		return s.resolveCodexMappedSessionIdentity(ctx, c, account, raw)
	})
}

func normalizeCodexOutboundIdentityRawFromInput(headers http.Header, body []byte, fallbackSession string, input *codexSessionIdentityInput, mapSession codexSessionIdentityMapper) ([]byte, codexRequestIdentitySnapshot, bool, error) {
	if len(body) == 0 {
		return body, codexRequestIdentitySnapshot{}, false, nil
	}
	root := gjson.ParseBytes(body)
	if !root.IsObject() {
		return body, codexRequestIdentitySnapshot{}, false, nil
	}
	clientMetadata := map[string]any{}
	if raw := gjson.GetBytes(body, "client_metadata"); raw.IsObject() {
		if err := json.Unmarshal([]byte(raw.Raw), &clientMetadata); err != nil {
			return body, codexRequestIdentitySnapshot{}, false, fmt.Errorf("decode codex client metadata: %w", err)
		}
	}
	identity := resolveCodexRequestIdentity(headers, clientMetadata, fallbackSession)
	promptCacheKey := gjson.GetBytes(body, "prompt_cache_key")
	cacheReferencesSession := promptCacheKey.Type == gjson.String && strings.TrimSpace(promptCacheKey.String()) != "" && strings.TrimSpace(promptCacheKey.String()) == identity.sessionID
	if input != nil {
		cacheReferencesSession = input.promptCacheKeyReferencesSession
		if isCodexUUIDv7(input.sessionID) {
			identity.sessionID = input.sessionID
		}
	}
	if identity.empty() {
		return body, identity, false, nil
	}
	if mapSession != nil && identity.sessionID != "" {
		mapped, err := mapSession(identity.sessionID)
		if err != nil {
			return body, identity, false, err
		}
		identity.sessionID = strings.TrimSpace(mapped)
	}
	_, headerMetadataRaw, err := applyCodexOutboundIdentityToClientMetadata(clientMetadata, identity)
	if err != nil {
		return body, identity, false, err
	}
	identity.turnMetadataRaw = headerMetadataRaw
	rawMetadata, err := json.Marshal(clientMetadata)
	if err != nil {
		return body, identity, false, fmt.Errorf("encode codex client metadata: %w", err)
	}
	next, err := sjson.SetRawBytes(body, "client_metadata", rawMetadata)
	if err != nil {
		return body, identity, false, fmt.Errorf("splice codex client metadata: %w", err)
	}
	if mapSession != nil && identity.sessionID != "" && promptCacheKey.Type == gjson.String && cacheReferencesSession {
		rewritten, setErr := sjson.SetBytes(next, "prompt_cache_key", identity.sessionID)
		if setErr != nil {
			return body, identity, false, fmt.Errorf("splice codex prompt cache key: %w", setErr)
		}
		next = rewritten
	}
	applyCodexOutboundIdentityToHeaders(headers, identity)
	return next, identity, true, nil
}
