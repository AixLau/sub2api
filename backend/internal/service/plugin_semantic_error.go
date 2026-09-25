package service

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

// These codes describe a rejected payload, not the health of a proxy or account.
// Keep an explicit set: TOOL_BRIDGE_STREAM_FAILED can still be a real disconnect.
func isPluginSemanticErrorCode(code string) bool {
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "TOOL_BRIDGE_CALL_INVALID", "TOOL_BRIDGE_REQUEST_INVALID", "TOOL_BRIDGE_ENCRYPTED_REPLAY_UNSAFE",
		"PLUGIN_INVALID_REQUEST", "PLUGIN_REQUEST_INVALID", "PLUGIN_TARGET_INVALID", "PLUGIN_UNSUPPORTED_ENDPOINT":
		return true
	}
	return false
}

func pluginSemanticTransportError(err error) (*PluginTransportError, bool) {
	var pluginErr *PluginTransportError
	if errors.As(err, &pluginErr) && pluginErr != nil && isPluginSemanticErrorCode(pluginErr.Code) {
		return pluginErr, true
	}
	return nil, false
}

func isOpenAINonRetryableProtocolFailure(payload []byte) bool {
	code := gjson.GetBytes(payload, "response.error.code").String()
	if code == "" {
		code = gjson.GetBytes(payload, "error.code").String()
	}
	return isPluginSemanticErrorCode(code) || code == "invalid_encrypted_content"
}

func pluginSemanticFailurePayload(responseID, model string, failure *PluginTransportError) []byte {
	if responseID == "" {
		responseID = "resp_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	payload, _ := json.Marshal(map[string]any{
		"type": "response.failed",
		"response": map[string]any{
			"id": responseID, "model": model, "object": "response", "status": "failed", "output": []any{},
			"error": map[string]string{"code": failure.Code, "type": "invalid_request_error", "message": sanitizeUpstreamErrorMessage(failure.Message)},
		},
	})
	return payload
}
