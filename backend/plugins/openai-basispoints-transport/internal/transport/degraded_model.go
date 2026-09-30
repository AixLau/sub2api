package transport

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// BPS can internally route a request to generated degrade or Codex abuse
// models. A 404 for one of those internal targets does not establish that the
// client's requested model is unavailable on this account.
func isInternalDegradedModelNotFound(status int, requestBody []byte, code, message string) bool {
	if status != http.StatusNotFound || code != "model_not_found" {
		return false
	}
	var request struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(requestBody, &request) != nil || request.Model == "" {
		return false
	}
	_, rest, ok := strings.Cut(message, "The model `")
	if !ok {
		return false
	}
	rejected, rest, ok := strings.Cut(rest, "`")
	if !ok || !strings.HasPrefix(rest, " does not exist or you do not have access to it.") {
		return false
	}
	if suffix, ok := strings.CutPrefix(rejected, request.Model+"-codex-abuse-1p-codexswic-"); ok && isModelNameSuffix(suffix) {
		return true
	}
	suffix, ok := strings.CutPrefix(rejected, request.Model+"-degrade")
	if !ok {
		return false
	}
	digits, separator, ok := strings.Cut(suffix, "-")
	if !ok || digits == "" || separator == "" {
		return false
	}
	return isDecimalModelSuffix(digits)
}

func isDecimalModelSuffix(value string) bool {
	if value == "" {
		return false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func isModelNameSuffix(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' {
			return false
		}
	}
	return true
}

func degradedModelUnavailableResponse(response *http.Response) *http.Response {
	response.Body.Close()
	body := []byte(`{"error":{"code":"bps_degraded_model_unavailable","message":"BPS internal degraded model is temporarily unavailable"}}`)
	response.StatusCode = http.StatusServiceUnavailable
	response.Status = "503 " + http.StatusText(http.StatusServiceUnavailable)
	response.Header = http.Header{"Content-Type": []string{"application/json"}}
	response.Body = io.NopCloser(bytes.NewReader(body))
	response.ContentLength = int64(len(body))
	return response
}
