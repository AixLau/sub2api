package service

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var codexSandboxOSPattern = regexp.MustCompile(`(?i)^(macos|mac os(?: x)?|macintosh|darwin|linux|ubuntu|windows(?: nt)?)(?:\s+[0-9][0-9a-z._+-]*)?$`)

// resolveCodexSandboxForUserAgent reads only the OS field in Codex's
// "client/version (OS version; architecture) terminal" format. Client names,
// terminals (such as WindowsTerminal), and trailing identity tags are not OS
// evidence. Unrecognized or ambiguous platform descriptions remain unchanged.
func resolveCodexSandboxForUserAgent(userAgent string) string {
	_, suffix, ok := strings.Cut(strings.TrimSpace(userAgent), "/")
	if !ok {
		return ""
	}
	_, suffix, ok = strings.Cut(suffix, " ")
	if !ok {
		return ""
	}
	suffix = strings.TrimSpace(suffix)
	if !strings.HasPrefix(suffix, "(") {
		return ""
	}
	platform, _, ok := strings.Cut(suffix[1:], ")")
	if !ok {
		return ""
	}
	os, _, _ := strings.Cut(platform, ";")
	match := codexSandboxOSPattern.FindStringSubmatch(strings.TrimSpace(os))
	if match == nil {
		return ""
	}
	switch strings.ToLower(match[1]) {
	case "macos", "mac os", "mac os x", "macintosh", "darwin":
		return "seatbelt"
	case "linux", "ubuntu":
		return "seccomp"
	case "windows", "windows nt":
		return "windows_sandbox"
	default:
		return ""
	}
}

// applyCodexFingerprintSandboxMetadataRaw uses the already finalized outbound
// User-Agent once for both carriers. Only existing sandbox fields are changed;
// execution policy and all session identities stay outside this projection.
func applyCodexFingerprintSandboxMetadataRaw(headers http.Header, body []byte, ids *codexFingerprintIDs) ([]byte, bool, error) {
	if ids == nil || (ids.mode != codexFingerprintDevice && !ids.httpSessionIdentity) {
		return body, false, nil
	}
	sandbox := resolveCodexSandboxForUserAgent(headers.Get("User-Agent"))
	if sandbox == "" {
		return body, false, nil
	}
	fields := map[string]any{"sandbox": sandbox}
	next := body
	bodyChanged := false
	if raw := gjson.GetBytes(body, "client_metadata.x-codex-turn-metadata"); raw.Type == gjson.String {
		if rewritten, changed := rewriteCodexMetadataJSONFields(raw.String(), fields); changed {
			var err error
			next, err = sjson.SetBytes(body, "client_metadata.x-codex-turn-metadata", rewritten)
			if err != nil {
				return body, false, fmt.Errorf("splice codex sandbox metadata: %w", err)
			}
			bodyChanged = true
		}
	}
	rewriteCodexTurnMetadataFields(headers, fields)
	return next, bodyChanged, nil
}

// normalizeCodexHTTPOutboundIdentityRaw runs after all HTTP User-Agent overrides
// and identity enforcement. Resolve sandbox before merging header/body metadata
// so both the final request and its identity snapshot share the same platform.
func (s *OpenAIGatewayService) normalizeCodexHTTPOutboundIdentityRaw(ctx context.Context, c *gin.Context, account *Account, headers http.Header, body []byte, fallbackSession string) ([]byte, codexRequestIdentitySnapshot, bool, error) {
	// Adopt existing HTTP isolation keys into owner indexes without changing
	// their mapping or lifetime. WS and compact do not enter this function.
	if !isOpenAICompatMessagesBridgeContext(c) && !isOpenAICompatMessagesBridgeBody(body) {
		ctx = codexHTTPIdentityOwnershipContext(ctx, c, account, CaptureCodexIdentityObservedAt(c), true)
	}
	next, sandboxChanged, err := applyCodexFingerprintSandboxMetadataRaw(headers, body, stagedCodexFingerprintIDs(c, account))
	if err != nil {
		return body, codexRequestIdentitySnapshot{}, false, err
	}
	next, identity, changed, err := s.normalizeCodexOutboundIdentityRaw(ctx, c, account, headers, next, fallbackSession)
	return next, identity, changed || sandboxChanged, err
}
