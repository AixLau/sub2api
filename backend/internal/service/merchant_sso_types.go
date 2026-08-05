package service

import (
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
)

const (
	MerchantAPIMode = "dynamic_api"

	MerchantEndpointRegisterLogin = "register_login"
	MerchantEndpointRegister      = "register"
	MerchantEndpointLogin         = "login"
	MerchantEndpointToken         = "token"
	MerchantEndpointSync          = "sync"
	MerchantEndpointBind          = "bind"
	MerchantEndpointStatus        = "status"
	MerchantEndpointCallback      = "callback"
	MerchantEndpointRecharge      = "recharge_records"

	MerchantAuthNone   = "none"
	MerchantAuthAPIKey = "api_key"
	MerchantAuthBearer = "bearer"
	MerchantAuthBasic  = "basic"
	MerchantAuthHMAC   = "hmac"

	MerchantStatusDraft    = "draft"
	MerchantStatusActive   = "active"
	MerchantStatusDisabled = "disabled"

	MerchantEndpointStatusActive   = "active"
	MerchantEndpointStatusDraft    = "draft"
	MerchantEndpointStatusDisabled = "disabled"
)

var (
	merchantTemplatePattern        = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.-]+)\s*\}\}`)
	defaultMerchantResponseMapping = map[string]any{
		"success":         "success",
		"externalUserId":  "data.user_id",
		"externalAccount": "data.account",
		"redirectUrl":     "data.redirect_url",
		"loginToken":      "data.login_token",
		"status":          "data.status",
		"errorCode":       "code",
		"errorMessage":    "message",
	}
)

// MerchantIntegration is the API-safe representation of a configured merchant.
type MerchantIntegration struct {
	ID            int64                 `json:"id"`
	Name          string                `json:"name"`
	Code          string                `json:"code"`
	Mode          string                `json:"mode"`
	MerchantCode  string                `json:"merchant_code"`
	Description   string                `json:"description"`
	Status        string                `json:"status"`
	Enabled       bool                  `json:"enabled"`
	RedirectHosts []string              `json:"redirect_hosts"`
	Endpoints     []MerchantAPIEndpoint `json:"endpoints,omitempty"`
	CreatedAt     time.Time             `json:"created_at"`
	UpdatedAt     time.Time             `json:"updated_at"`
}

type MerchantAPIEndpoint struct {
	ID              int64          `json:"id"`
	IntegrationID   int64          `json:"integration_id"`
	Type            string         `json:"type"`
	URL             string         `json:"url"`
	Method          string         `json:"method"`
	ContentType     string         `json:"content_type"`
	QueryTemplate   map[string]any `json:"query_template"`
	HeaderTemplate  map[string]any `json:"header_template"`
	BodyTemplate    map[string]any `json:"body_template"`
	AuthType        string         `json:"auth_type"`
	SecretRef       string         `json:"secret_ref,omitempty"`
	ResponseMapping map[string]any `json:"response_mapping"`
	SuccessRule     map[string]any `json:"success_rule"`
	RetryPolicy     map[string]any `json:"retry_policy"`
	TimeoutMS       int            `json:"timeout_ms"`
	Status          string         `json:"status"`
	Enabled         bool           `json:"enabled"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type MerchantBinding struct {
	ID                    int64      `json:"id"`
	IntegrationID         int64      `json:"integration_id"`
	IntegrationName       string     `json:"integration_name,omitempty"`
	IntegrationCode       string     `json:"integration_code,omitempty"`
	UserID                int64      `json:"user_id"`
	ExternalUserID        string     `json:"external_user_id"`
	ExternalAccount       string     `json:"external_account"`
	Status                string     `json:"status"`
	LastLoginAt           *time.Time `json:"last_login_at,omitempty"`
	LastSyncAt            *time.Time `json:"last_sync_at,omitempty"`
	LastRechargeSyncAt    *time.Time `json:"last_recharge_sync_at,omitempty"`
	RechargeSyncAvailable bool       `json:"recharge_sync_available"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

type MerchantRechargeRecord struct {
	ID                int64     `json:"id"`
	IntegrationID     int64     `json:"integration_id"`
	UserID            string    `json:"user_id"`
	PlatformUserID    int64     `json:"platform_user_id,omitempty"`
	OrderNo           string    `json:"order_no"`
	Amount            string    `json:"amount"`
	Currency          string    `json:"currency"`
	BalanceBefore     string    `json:"balance_before"`
	BalanceAfter      string    `json:"balance_after"`
	ChargeType        string    `json:"charge_type"`
	PayMethod         string    `json:"pay_method"`
	Status            string    `json:"status"`
	PlatformOrderNo   string    `json:"platform_order_no"`
	MerchantCreatedAt time.Time `json:"created_at"`
	CreatedAt         time.Time `json:"synced_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type MerchantIntegrationInput struct {
	Name          string   `json:"name"`
	Code          string   `json:"code"`
	Mode          string   `json:"mode"`
	MerchantCode  string   `json:"merchant_code"`
	Description   string   `json:"description"`
	Status        string   `json:"status"`
	Enabled       bool     `json:"enabled"`
	RedirectHosts []string `json:"redirect_hosts"`
}

type MerchantAPIEndpointInput struct {
	Type            string         `json:"type"`
	URL             string         `json:"url"`
	Method          string         `json:"method"`
	ContentType     string         `json:"content_type"`
	QueryTemplate   map[string]any `json:"query_template"`
	HeaderTemplate  map[string]any `json:"header_template"`
	BodyTemplate    map[string]any `json:"body_template"`
	AuthType        string         `json:"auth_type"`
	SecretRef       string         `json:"secret_ref"`
	ResponseMapping map[string]any `json:"response_mapping"`
	SuccessRule     map[string]any `json:"success_rule"`
	RetryPolicy     map[string]any `json:"retry_policy"`
	TimeoutMS       int            `json:"timeout_ms"`
	Status          string         `json:"status"`
	Enabled         bool           `json:"enabled"`
}

type MerchantPublicIntegration struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Code        string `json:"code"`
	Description string `json:"description"`
}

type MerchantLaunchResult struct {
	IntegrationID   int64  `json:"integration_id"`
	BindingID       int64  `json:"binding_id"`
	ExternalUserID  string `json:"external_user_id"`
	ExternalAccount string `json:"external_account,omitempty"`
	RedirectURL     string `json:"redirect_url"`
}

type MerchantRechargeQuery struct {
	StartTime string
	EndTime   string
}

type MerchantRechargeSyncResult struct {
	BindingID int64                    `json:"binding_id"`
	Synced    int                      `json:"synced"`
	Records   []MerchantRechargeRecord `json:"records"`
}

type MerchantBindingActionResult struct {
	Binding    MerchantBinding `json:"binding"`
	HTTPStatus int             `json:"http_status"`
	Response   map[string]any  `json:"response,omitempty"`
}

type MerchantCallbackResult struct {
	Binding  MerchantBinding `json:"binding"`
	Response map[string]any  `json:"response,omitempty"`
}

type MerchantTestResult struct {
	EndpointID  int64          `json:"endpoint_id"`
	HTTPStatus  int            `json:"http_status"`
	Successful  bool           `json:"successful"`
	Code        string         `json:"code,omitempty"`
	Message     string         `json:"message,omitempty"`
	RedirectURL string         `json:"redirect_url,omitempty"`
	Response    map[string]any `json:"response,omitempty"`
}

type merchantResponse struct {
	Successful      bool
	Code            string
	Message         string
	ExternalUserID  string
	ExternalAccount string
	RedirectURL     string
	LoginToken      string
	Status          string
	Payload         map[string]any
}

type merchantCallResult struct {
	HTTPStatus int
	Response   merchantResponse
}

var (
	ErrMerchantIntegrationNotFound = infraerrors.New(404, "MERCHANT_INTEGRATION_NOT_FOUND", "merchant integration not found")
	ErrMerchantEndpointNotFound    = infraerrors.New(404, "MERCHANT_ENDPOINT_NOT_FOUND", "merchant API endpoint not found")
	ErrMerchantBindingNotFound     = infraerrors.New(404, "MERCHANT_BINDING_NOT_FOUND", "merchant binding not found")
	ErrMerchantNotReady            = infraerrors.New(409, "MERCHANT_INTEGRATION_NOT_READY", "merchant integration is not ready for use")
	ErrMerchantCallFailed          = infraerrors.New(502, "MERCHANT_API_CALL_FAILED", "merchant API call failed")
	ErrMerchantResponseInvalid     = infraerrors.New(502, "MERCHANT_RESPONSE_INVALID", "merchant API response is invalid")
)

func merchantBadRequest(format string, args ...any) error {
	return infraerrors.Newf(400, "MERCHANT_INVALID_CONFIG", format, args...)
}

func merchantNotFound(err error, fallback *infraerrors.ApplicationError) error {
	if err == nil {
		return nil
	}
	if fallback != nil {
		return fallback.WithCause(err)
	}
	return err
}

func normalizeMerchantResponseMapping(mapping map[string]any) map[string]any {
	result := make(map[string]any, len(defaultMerchantResponseMapping)+len(mapping))
	for key, value := range defaultMerchantResponseMapping {
		result[key] = value
	}
	for key, value := range mapping {
		result[key] = value
	}
	return result
}

func normalizeMerchantEndpointInput(input MerchantAPIEndpointInput) (MerchantAPIEndpointInput, error) {
	input.Type = strings.TrimSpace(strings.ToLower(input.Type))
	if !isMerchantEndpointType(input.Type) {
		return input, merchantBadRequest("unsupported merchant endpoint type %q", input.Type)
	}
	input.URL = strings.TrimSpace(input.URL)
	if _, err := urlvalidator.ValidateHTTPURL(input.URL, true, urlvalidator.ValidationOptions{AllowPrivate: false}); err != nil {
		return input, merchantBadRequest("invalid merchant endpoint URL: %v", err)
	}
	input.Method = strings.ToUpper(strings.TrimSpace(input.Method))
	if input.Method == "" {
		input.Method = "POST"
	}
	if !isMerchantHTTPMethod(input.Method) {
		return input, merchantBadRequest("unsupported merchant endpoint method %q", input.Method)
	}
	input.ContentType = strings.TrimSpace(strings.ToLower(input.ContentType))
	if input.ContentType == "" {
		input.ContentType = "application/json"
	}
	if input.ContentType != "application/json" && input.ContentType != "application/x-www-form-urlencoded" {
		return input, merchantBadRequest("unsupported merchant content type %q", input.ContentType)
	}
	input.AuthType = strings.TrimSpace(strings.ToLower(input.AuthType))
	if input.AuthType == "" {
		input.AuthType = MerchantAuthNone
	}
	if !isMerchantAuthType(input.AuthType) {
		return input, merchantBadRequest("unsupported merchant auth type %q", input.AuthType)
	}
	input.SecretRef = strings.TrimSpace(input.SecretRef)
	if input.AuthType != MerchantAuthNone && input.SecretRef == "" {
		return input, merchantBadRequest("secret_ref is required for auth type %q", input.AuthType)
	}
	if input.TimeoutMS == 0 {
		input.TimeoutMS = 10000
	}
	if input.TimeoutMS < 100 || input.TimeoutMS > 120000 {
		return input, merchantBadRequest("timeout_ms must be between 100 and 120000")
	}
	if input.Status == "" {
		input.Status = MerchantEndpointStatusActive
	}
	if input.Status != MerchantEndpointStatusDraft && input.Status != MerchantEndpointStatusActive && input.Status != MerchantEndpointStatusDisabled {
		return input, merchantBadRequest("unsupported merchant endpoint status %q", input.Status)
	}
	if input.QueryTemplate == nil {
		input.QueryTemplate = map[string]any{}
	}
	if input.HeaderTemplate == nil {
		input.HeaderTemplate = map[string]any{}
	}
	if input.BodyTemplate == nil {
		input.BodyTemplate = map[string]any{}
	}
	if input.ResponseMapping == nil {
		input.ResponseMapping = map[string]any{}
	}
	input.ResponseMapping = normalizeMerchantResponseMapping(input.ResponseMapping)
	if input.SuccessRule == nil {
		input.SuccessRule = map[string]any{}
	}
	if input.RetryPolicy == nil || len(input.RetryPolicy) == 0 {
		input.RetryPolicy = map[string]any{"maxAttempts": 1, "backoffMs": 300}
	}
	if err := validateMerchantRetryPolicy(input.RetryPolicy); err != nil {
		return input, err
	}
	if err := validateMerchantSuccessRule(input.SuccessRule); err != nil {
		return input, err
	}
	return input, nil
}

func normalizeMerchantIntegrationInput(input MerchantIntegrationInput) (MerchantIntegrationInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Code = strings.TrimSpace(input.Code)
	input.Mode = strings.TrimSpace(strings.ToLower(input.Mode))
	input.MerchantCode = strings.TrimSpace(input.MerchantCode)
	input.Description = strings.TrimSpace(input.Description)
	input.Status = strings.TrimSpace(strings.ToLower(input.Status))
	if input.Name == "" || input.Code == "" {
		return input, merchantBadRequest("merchant integration name and code are required")
	}
	if input.Mode == "" {
		input.Mode = MerchantAPIMode
	}
	if input.Mode != MerchantAPIMode {
		return input, merchantBadRequest("unsupported merchant integration mode %q", input.Mode)
	}
	if input.Status == "" {
		input.Status = MerchantStatusDraft
	}
	if input.Status != MerchantStatusDraft && input.Status != MerchantStatusActive && input.Status != MerchantStatusDisabled {
		return input, merchantBadRequest("unsupported merchant integration status %q", input.Status)
	}
	hosts, err := normalizeMerchantRedirectHosts(input.RedirectHosts)
	if err != nil {
		return input, err
	}
	input.RedirectHosts = hosts
	return input, nil
}

func normalizeMerchantRedirectHosts(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, raw := range values {
		raw = strings.ToLower(strings.TrimSpace(raw))
		if raw == "" {
			continue
		}
		if strings.ContainsAny(raw, "/?#@") {
			if parsed, err := url.Parse(raw); err == nil && parsed.Hostname() != "" && parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == "" {
				raw = strings.ToLower(parsed.Hostname())
			} else {
				return nil, merchantBadRequest("redirect_hosts must contain host names")
			}
		}
		host := strings.TrimSuffix(raw, ".")
		if strings.HasPrefix(host, "*.") {
			if len(host) <= 2 || strings.Contains(host[2:], "*") {
				return nil, merchantBadRequest("invalid redirect host %q", raw)
			}
		} else if strings.Contains(host, "*") {
			return nil, merchantBadRequest("invalid redirect host %q", raw)
		}
		plainHost := strings.TrimPrefix(host, "*.")
		if plainHost == "localhost" || strings.HasSuffix(plainHost, ".localhost") {
			return nil, merchantBadRequest("private redirect hosts are not allowed")
		}
		if ip := net.ParseIP(plainHost); ip != nil && isPrivateIP(ip) {
			return nil, merchantBadRequest("private redirect hosts are not allowed")
		}
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		result = append(result, host)
	}
	return result, nil
}

func validateMerchantRetryPolicy(policy map[string]any) error {
	maxAttempts := intValue(policy["maxAttempts"], intValue(policy["max_attempts"], 1))
	backoff := intValue(policy["backoffMs"], intValue(policy["backoff_ms"], 300))
	if maxAttempts < 1 || maxAttempts > 5 {
		return merchantBadRequest("retry maxAttempts must be between 1 and 5")
	}
	if backoff < 0 || backoff > 60000 {
		return merchantBadRequest("retry backoffMs must be between 0 and 60000")
	}
	return nil
}

func validateMerchantSuccessRule(rule map[string]any) error {
	if len(rule) == 0 {
		return nil
	}
	if unmatched, ok := merchantStringValue(rule["unmatched"]); ok && unmatched != "http" && unmatched != "success" && unmatched != "failure" {
		return merchantBadRequest("success rule unmatched must be http, success, or failure")
	}
	for _, key := range []string{"success", "failure"} {
		value, ok := rule[key].(map[string]any)
		if !ok {
			if rule[key] == nil {
				continue
			}
			return merchantBadRequest("success rule %s must be an object", key)
		}
		operator, _ := merchantStringValue(value["operator"])
		if operator == "" {
			operator = "equals"
		}
		switch operator {
		case "in", "not_in", "equals", "exists", "not_exists", "truthy", "falsy":
		default:
			return merchantBadRequest("unsupported success rule operator %q", operator)
		}
		path, _ := merchantStringValue(value["path"])
		if path == "" && operator != "exists" && operator != "not_exists" {
			return merchantBadRequest("success rule %s path is required", key)
		}
	}
	return nil
}

func isMerchantEndpointType(value string) bool {
	switch value {
	case MerchantEndpointRegisterLogin, MerchantEndpointRegister, MerchantEndpointLogin, MerchantEndpointToken,
		MerchantEndpointSync, MerchantEndpointBind, MerchantEndpointStatus, MerchantEndpointCallback, MerchantEndpointRecharge:
		return true
	default:
		return false
	}
}

func isMerchantHTTPMethod(value string) bool {
	switch value {
	case "GET", "POST", "PUT", "PATCH", "DELETE":
		return true
	default:
		return false
	}
}

func isMerchantAuthType(value string) bool {
	switch value {
	case MerchantAuthNone, MerchantAuthAPIKey, MerchantAuthBearer, MerchantAuthBasic, MerchantAuthHMAC:
		return true
	default:
		return false
	}
}

func intValue(value any, fallback int) int {
	switch value := value.(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		if value >= math.MinInt && value <= math.MaxInt {
			return int(value)
		}
	case json.Number:
		if parsed, err := strconv.Atoi(string(value)); err == nil {
			return parsed
		}
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
			return parsed
		}
	}
	return fallback
}

func merchantStringValue(value any) (string, bool) {
	if value == nil {
		return "", false
	}
	switch value := value.(type) {
	case string:
		return value, true
	case json.Number:
		return string(value), true
	case float64:
		return formatMerchantNumber(value), true
	case float32:
		return formatMerchantNumber(float64(value)), true
	case int:
		return strconv.Itoa(value), true
	case int64:
		return strconv.FormatInt(value, 10), true
	case bool:
		return strconv.FormatBool(value), true
	default:
		return fmt.Sprint(value), true
	}
}

func formatMerchantNumber(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return ""
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func merchantTemplateValue(name string, context merchantTemplateContext) string {
	switch strings.TrimSpace(name) {
	case "integration.merchant_code":
		return context.MerchantCode
	case "user.id":
		return strconv.FormatInt(context.UserID, 10)
	case "user.username":
		return context.Username
	case "user.nickname":
		return context.Nickname
	case "user.email":
		return context.Email
	case "user.phone":
		return context.Phone
	case "binding.external_user_id":
		return context.ExternalUserID
	case "binding.external_account":
		return context.ExternalAccount
	case "login_token", "loginToken", "token":
		return context.LoginToken
	case "requestId":
		return context.RequestID
	case "timestamp":
		return context.Timestamp
	case "nonce":
		return context.Nonce
	case "query.start", "query.start_time", "query.startTime":
		return context.Query["start_time"]
	case "query.end", "query.end_time", "query.endTime":
		return context.Query["end_time"]
	default:
		return ""
	}
}

type merchantTemplateContext struct {
	MerchantCode    string
	UserID          int64
	Username        string
	Nickname        string
	Email           string
	Phone           string
	ExternalUserID  string
	ExternalAccount string
	RequestID       string
	Timestamp       string
	Nonce           string
	LoginToken      string
	Query           map[string]string
}

func renderMerchantTemplate(value any, context merchantTemplateContext) any {
	switch value := value.(type) {
	case string:
		return merchantTemplatePattern.ReplaceAllStringFunc(value, func(match string) string {
			parts := merchantTemplatePattern.FindStringSubmatch(match)
			if len(parts) != 2 {
				return match
			}
			return merchantTemplateValue(parts[1], context)
		})
	case map[string]any:
		result := make(map[string]any, len(value))
		for key, item := range value {
			result[key] = renderMerchantTemplate(item, context)
		}
		return result
	case []any:
		result := make([]any, len(value))
		for i, item := range value {
			result[i] = renderMerchantTemplate(item, context)
		}
		return result
	default:
		return value
	}
}

func renderMerchantString(value string, context merchantTemplateContext) string {
	result, _ := renderMerchantTemplate(value, context).(string)
	return result
}

func getMerchantPath(root any, path string) (any, bool) {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "$")
	path = strings.TrimPrefix(path, ".")
	if path == "" {
		return root, true
	}
	var current any = root
	for _, part := range strings.Split(path, ".") {
		if part == "" {
			continue
		}
		switch value := current.(type) {
		case map[string]any:
			var ok bool
			current, ok = value[part]
			if !ok {
				return nil, false
			}
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(value) {
				return nil, false
			}
			current = value[index]
		default:
			return nil, false
		}
	}
	return current, true
}

func merchantStringAt(root any, path string) string {
	value, ok := getMerchantPath(root, path)
	if !ok {
		return ""
	}
	result, _ := merchantStringValue(value)
	return strings.TrimSpace(result)
}

func merchantValueEqual(left, right any) bool {
	leftString, leftOK := merchantStringValue(left)
	rightString, rightOK := merchantStringValue(right)
	if !leftOK || !rightOK {
		return reflect.DeepEqual(left, right)
	}
	return leftString == rightString
}

func merchantValueTruthy(value any) bool {
	if value == nil {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case float64:
		return typed != 0 && !math.IsNaN(typed)
	case int:
		return typed != 0
	case string:
		normalized := strings.ToLower(strings.TrimSpace(typed))
		return normalized != "" && normalized != "0" && normalized != "false" && normalized != "no" && normalized != "null"
	default:
		return true
	}
}

func merchantRuleMatches(payload map[string]any, raw any) bool {
	rule, ok := raw.(map[string]any)
	if !ok || rule == nil {
		return false
	}
	path, _ := merchantStringValue(rule["path"])
	operator, _ := merchantStringValue(rule["operator"])
	if operator == "" {
		operator = "equals"
	}
	value, exists := getMerchantPath(payload, path)
	switch operator {
	case "exists":
		return exists
	case "not_exists":
		return !exists
	case "truthy":
		return exists && merchantValueTruthy(value)
	case "falsy":
		return !exists || !merchantValueTruthy(value)
	case "equals":
		values, _ := rule["values"].([]any)
		if len(values) == 0 {
			if single, exists := rule["value"]; exists {
				return exists && merchantValueEqual(value, single)
			}
			return false
		}
		return exists && merchantValueEqual(value, values[0])
	case "in", "not_in":
		values, _ := rule["values"].([]any)
		matched := false
		if exists {
			for _, candidate := range values {
				if merchantValueEqual(value, candidate) {
					matched = true
					break
				}
			}
		}
		if operator == "not_in" {
			return !matched
		}
		return matched
	default:
		return false
	}
}

func merchantHTTPMatches(status int, raw any) bool {
	rule, ok := raw.(map[string]any)
	if !ok || len(rule) == 0 {
		return status >= 200 && status <= 299
	}
	if statuses, ok := rule["statuses"].([]any); ok && len(statuses) > 0 {
		for _, candidate := range statuses {
			if intValue(candidate, -1) == status {
				return true
			}
		}
		return false
	}
	min := intValue(rule["min"], 200)
	max := intValue(rule["max"], 299)
	return status >= min && status <= max
}

func evaluateMerchantResponse(status int, payload map[string]any, rule map[string]any, successPath string) bool {
	if len(rule) == 0 {
		if raw, exists := getMerchantPath(payload, successPath); exists {
			return merchantValueTruthy(raw) || merchantValueEqual(raw, float64(0)) || merchantValueEqual(raw, "0")
		}
		return status >= 200 && status <= 299
	}
	requireHTTP := true
	if configured, ok := rule["requireHttpSuccess"].(bool); ok {
		requireHTTP = configured
	}
	httpMatches := merchantHTTPMatches(status, rule["http"])
	failureMatches := merchantRuleMatches(payload, rule["failure"])
	successMatches := merchantRuleMatches(payload, rule["success"])
	if failureMatches {
		return false
	}
	if successMatches {
		return !requireHTTP || httpMatches
	}
	unmatched, _ := merchantStringValue(rule["unmatched"])
	switch unmatched {
	case "http":
		return httpMatches
	case "success":
		return !requireHTTP || httpMatches
	case "failure":
		return false
	default:
		return httpMatches
	}
}

func parseMerchantTime(value any) (time.Time, error) {
	if value == nil {
		return time.Time{}, fmt.Errorf("created_at is required")
	}
	if text, ok := value.(string); ok {
		text = strings.TrimSpace(text)
		if text == "" {
			return time.Time{}, fmt.Errorf("created_at is required")
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05Z07:00", "2006-01-02 15:04:05"} {
			if parsed, err := time.Parse(layout, text); err == nil {
				return parsed, nil
			}
		}
		if unix, err := strconv.ParseInt(text, 10, 64); err == nil {
			return time.Unix(unix, 0).UTC(), nil
		}
	}
	if number, ok := value.(float64); ok && number >= 0 && number <= math.MaxInt64 {
		return time.Unix(int64(number), 0).UTC(), nil
	}
	return time.Time{}, fmt.Errorf("created_at must be ISO8601 or Unix seconds")
}

func cloneMerchantMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return map[string]any{}
	}
	var output map[string]any
	if err := json.Unmarshal(raw, &output); err != nil || output == nil {
		return map[string]any{}
	}
	return output
}
