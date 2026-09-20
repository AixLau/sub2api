package service

import (
	"context"
	"reflect"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// CredentialAccountEdit identifies an authorized, versioned account-settings
// edit. It is supplied by the admin handler, never decoded from request JSON.
type CredentialAccountEdit struct {
	PrincipalID   int64
	ActorID       int64
	ConfigVersion int64
	Activate      bool
}

type CredentialAccountSettingsRepository interface {
	UpdateCredentialAccountSettings(context.Context, *Account, *UpdateAccountInput) error
}

// These are account policies carried in credentials by the existing editor.
// Authentication and installation identity remain owned by instance lifecycle.
var CredentialAccountPolicyKeys = []string{
	"model_mapping", "compact_model_mapping", "plan_type", "intercept_warmup_requests",
	"account_scheduling_threshold", "temp_unschedulable_enabled", "temp_unschedulable_rules",
}

var CredentialAccountExtraPolicyKeys = []string{
	"openai_passthrough", "openai_oauth_passthrough", "openai_responses_flatten_namespaces",
	"openai_long_context_billing_enabled", "openai_oauth_responses_websockets_v2_mode",
	"openai_oauth_responses_websockets_v2_enabled", "responses_websockets_v2_enabled", "openai_ws_enabled",
	"openai_compact_mode", "auto_pause_5h_threshold", "auto_pause_7d_threshold",
	"auto_pause_5h_disabled", "auto_pause_7d_disabled", "auto_reset_credit_enabled",
	"auto_reset_credit_5h_threshold", "auto_reset_credit_7d_threshold",
	"codex_image_generation_bridge_enabled", "codex_image_generation_bridge",
	"codex_image_generation_explicit_tool_policy", "codex_cli_only", "codex_cli_only_allowed_clients",
	"codex_cli_only_allow_app_server", "codex_fingerprint_mode", "upstream_request_id_header",
}

func credentialAccountPolicyMap(current, incoming map[string]any, keys []string) map[string]any {
	out := make(map[string]any, len(current)+len(incoming))
	for key, value := range current {
		out[key] = value
	}
	for _, key := range keys {
		delete(out, key)
		if value, ok := incoming[key]; ok {
			out[key] = value
		}
	}
	return out
}

func prepareCredentialAccountEdit(account *Account, input *UpdateAccountInput) error {
	edit := input.CredentialEdit
	if edit == nil {
		return nil
	}
	if edit.PrincipalID <= 0 || edit.ActorID <= 0 || edit.ConfigVersion <= 0 ||
		account.Platform != PlatformOpenAI || (account.Type != AccountTypeOAuth && account.Type != AccountTypeSetupToken) {
		return infraerrors.BadRequest("INVALID_CONTROL_CONFIGURATION", "Invalid account settings edit")
	}
	if input.Type != "" && input.Type != account.Type {
		return infraerrors.BadRequest("CONTROLLED_CREDENTIAL_IDENTITY_IMMUTABLE", "Change authorization through the account's credential instances")
	}
	if input.Name != "" && (strings.TrimSpace(input.Name) == "" || len([]rune(input.Name)) > maxAccountNameRunes) {
		return infraerrors.BadRequest("INVALID_CONTROL_CONFIGURATION", "Invalid account name")
	}
	if input.Concurrency != nil && (*input.Concurrency < 0 || *input.Concurrency > 2147483647) {
		return infraerrors.BadRequest("INVALID_CONTROL_CONFIGURATION", "Account concurrency must be between 0 and 2147483647")
	}
	if input.Credentials != nil {
		policies := make(map[string]bool, len(CredentialAccountPolicyKeys))
		for _, key := range CredentialAccountPolicyKeys {
			policies[key] = true
		}
		for key, value := range input.Credentials {
			if IsSensitiveCredentialKey(key) || (!policies[key] && !reflect.DeepEqual(account.Credentials[key], value)) {
				return infraerrors.BadRequest("CONTROLLED_CREDENTIAL_IDENTITY_IMMUTABLE", "Change authorization through the account's credential instances")
			}
		}
		input.Credentials = credentialAccountPolicyMap(account.Credentials, input.Credentials, CredentialAccountPolicyKeys)
	}
	if input.Extra != nil {
		// Ignore reflected runtime values from the form; they may have changed
		// since it was opened and are not account settings.
		input.Extra = credentialAccountPolicyMap(account.Extra, input.Extra, CredentialAccountExtraPolicyKeys)
	}
	return nil
}
