package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
)

const (
	merchantDefaultTimeout        = 10 * time.Second
	merchantDialTimeout           = 10 * time.Second
	merchantResponseHeaderTimeout = 30 * time.Second
	merchantIdleConnTimeout       = 90 * time.Second
	merchantMaxResponseBytes      = 256 * 1024
	merchantDefaultMaxAttempts    = 1
	merchantDefaultBackoff        = 300 * time.Millisecond
)

type merchantHTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

func newMerchantHTTPClient() merchantHTTPDoer {
	transport := &http.Transport{
		// Merchant URLs are validated and dialed directly so an HTTP proxy cannot
		// bypass the private-address SSRF policy enforced by safeDialContext.
		Proxy:                 nil,
		DialContext:           safeDialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          16,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       merchantIdleConnTimeout,
		ResponseHeaderTimeout: merchantResponseHeaderTimeout,
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (s *MerchantSSOService) callMerchantEndpoint(
	ctx context.Context,
	integration *dbent.MerchantIntegration,
	endpoint *dbent.MerchantAPIEndpoint,
	platformUser *dbent.User,
	binding *dbent.MerchantBinding,
	query MerchantRechargeQuery,
) (merchantCallResult, error) {
	return s.callMerchantEndpointWithToken(ctx, integration, endpoint, platformUser, binding, query, "")
}

func (s *MerchantSSOService) callMerchantEndpointWithToken(
	ctx context.Context,
	integration *dbent.MerchantIntegration,
	endpoint *dbent.MerchantAPIEndpoint,
	platformUser *dbent.User,
	binding *dbent.MerchantBinding,
	query MerchantRechargeQuery,
	loginToken string,
) (merchantCallResult, error) {
	if endpoint == nil || integration == nil {
		return merchantCallResult{}, ErrMerchantCallFailed.WithCause(fmt.Errorf("merchant endpoint is not configured"))
	}
	if s.httpClient == nil {
		s.httpClient = newMerchantHTTPClient()
	}

	templateContext := merchantTemplateContext{
		MerchantCode: integration.MerchantCode,
		RequestID:    "merchant-" + merchantRandomToken(12),
		Timestamp:    merchantFormatInt(time.Now().Unix()),
		Nonce:        merchantRandomToken(16),
		LoginToken:   strings.TrimSpace(loginToken),
		Query: map[string]string{
			"start_time": query.StartTime,
			"end_time":   query.EndTime,
		},
	}
	if platformUser != nil {
		templateContext.UserID = platformUser.ID
		templateContext.Username = platformUser.Username
		templateContext.Nickname = platformUser.Username
		templateContext.Email = platformUser.Email
		templateContext.Nickname, templateContext.Phone = merchantUserProfile(platformUser)
		if templateContext.Nickname == "" {
			templateContext.Nickname = platformUser.Username
		}
	}
	if binding != nil {
		templateContext.ExternalUserID = binding.ExternalUserID
		templateContext.ExternalAccount = binding.ExternalAccount
	}

	rawURL := renderMerchantString(endpoint.URL, templateContext)
	validatedURL, err := urlvalidator.ValidateHTTPURL(rawURL, true, urlvalidator.ValidationOptions{AllowPrivate: false})
	if err != nil {
		return merchantCallResult{}, ErrMerchantCallFailed.WithCause(fmt.Errorf("invalid merchant endpoint URL: %w", err))
	}
	parsedURL, err := url.Parse(validatedURL)
	if err != nil {
		return merchantCallResult{}, ErrMerchantCallFailed.WithCause(err)
	}
	if err := addMerchantQueryValues(parsedURL, renderMerchantTemplate(endpoint.QueryTemplate, templateContext)); err != nil {
		return merchantCallResult{}, ErrMerchantCallFailed.WithCause(err)
	}

	body, err := merchantRequestBody(endpoint.ContentType, renderMerchantTemplate(endpoint.BodyTemplate, templateContext))
	if err != nil {
		return merchantCallResult{}, ErrMerchantCallFailed.WithCause(err)
	}
	request, err := http.NewRequestWithContext(ctx, endpoint.Method, parsedURL.String(), bytes.NewReader(body))
	if err != nil {
		return merchantCallResult{}, ErrMerchantCallFailed.WithCause(err)
	}
	if len(body) == 0 {
		request.Body = nil
		request.GetBody = func() (io.ReadCloser, error) { return http.NoBody, nil }
	}
	if err := addMerchantHeaders(request, renderMerchantTemplate(endpoint.HeaderTemplate, templateContext)); err != nil {
		return merchantCallResult{}, ErrMerchantCallFailed.WithCause(err)
	}
	if request.Header.Get("X-Request-Id") == "" {
		request.Header.Set("X-Request-Id", templateContext.RequestID)
	}
	if request.Header.Get("X-Timestamp") == "" {
		request.Header.Set("X-Timestamp", templateContext.Timestamp)
	}
	if len(body) > 0 && request.Header.Get("Content-Type") == "" {
		request.Header.Set("Content-Type", endpoint.ContentType)
	}
	if err := addMerchantAuthentication(request, endpoint, body, templateContext.Timestamp); err != nil {
		return merchantCallResult{}, ErrMerchantCallFailed.WithCause(err)
	}

	maxAttempts := intValue(endpoint.RetryPolicy["maxAttempts"], intValue(endpoint.RetryPolicy["max_attempts"], merchantDefaultMaxAttempts))
	if maxAttempts < 1 {
		maxAttempts = merchantDefaultMaxAttempts
	}
	if maxAttempts > 5 {
		maxAttempts = 5
	}
	backoffMS := intValue(endpoint.RetryPolicy["backoffMs"], intValue(endpoint.RetryPolicy["backoff_ms"], int(merchantDefaultBackoff/time.Millisecond)))
	if backoffMS < 0 {
		backoffMS = 0
	}
	timeout := merchantDefaultTimeout
	if endpoint.TimeoutMs > 0 {
		timeout = time.Duration(endpoint.TimeoutMs) * time.Millisecond
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 && backoffMS > 0 {
			timer := time.NewTimer(time.Duration(backoffMS*(attempt-1)) * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return merchantCallResult{}, ErrMerchantCallFailed.WithCause(ctx.Err())
			case <-timer.C:
			}
		}

		attemptCtx, cancel := context.WithTimeout(ctx, timeout)
		attemptRequest := request.Clone(attemptCtx)
		if request.GetBody != nil {
			attemptRequest.Body, err = request.GetBody()
			if err != nil {
				cancel()
				return merchantCallResult{}, ErrMerchantCallFailed.WithCause(err)
			}
		}
		response, requestErr := s.httpClient.Do(attemptRequest)
		if requestErr != nil {
			cancel()
			if response != nil && response.Body != nil {
				_ = response.Body.Close()
			}
			lastErr = requestErr
			if attempt < maxAttempts {
				continue
			}
			return merchantCallResult{}, ErrMerchantCallFailed.WithCause(requestErr)
		}
		if response == nil {
			cancel()
			lastErr = fmt.Errorf("empty merchant HTTP response")
			if attempt < maxAttempts {
				continue
			}
			return merchantCallResult{}, ErrMerchantCallFailed.WithCause(lastErr)
		}
		var payloadBytes []byte
		var readErr error
		if response.Body == nil {
			readErr = fmt.Errorf("merchant response body is empty")
		} else {
			payloadBytes, readErr = readMerchantResponse(response.Body)
			_ = response.Body.Close()
		}
		cancel()
		if readErr != nil {
			lastErr = readErr
			if attempt < maxAttempts && merchantRetryableStatus(response.StatusCode) {
				continue
			}
			return merchantCallResult{}, ErrMerchantResponseInvalid.WithCause(readErr)
		}
		var payload map[string]any
		if err := json.Unmarshal(payloadBytes, &payload); err != nil || payload == nil {
			lastErr = fmt.Errorf("merchant response is not a JSON object")
			if attempt < maxAttempts && merchantRetryableStatus(response.StatusCode) {
				continue
			}
			return merchantCallResult{}, ErrMerchantResponseInvalid.WithCause(lastErr)
		}

		mapping := endpointResponseMapping(endpoint)
		successPath := mappingPath(mapping, "success", "success_path", "success")
		merchantResponse := merchantResponse{
			Successful:      evaluateMerchantResponse(response.StatusCode, payload, endpoint.SuccessRule, successPath),
			Code:            merchantStringAt(payload, mappingPath(mapping, "errorCode", "error_code", "code")),
			Message:         merchantStringAt(payload, mappingPath(mapping, "errorMessage", "error_message", "message")),
			ExternalUserID:  merchantStringAt(payload, mappingPath(mapping, "externalUserId", "external_user_id", "data.user_id")),
			ExternalAccount: merchantStringAt(payload, mappingPath(mapping, "externalAccount", "external_account", "data.account")),
			RedirectURL:     merchantStringAt(payload, mappingPath(mapping, "redirectUrl", "redirect_url", "data.redirect_url")),
			LoginToken:      merchantStringAt(payload, mappingPath(mapping, "loginToken", "login_token", "data.login_token")),
			Status:          merchantStringAt(payload, mappingPath(mapping, "status", "status_path", "data.status")),
			Payload:         payload,
		}
		if merchantRetryableStatus(response.StatusCode) && attempt < maxAttempts {
			lastErr = fmt.Errorf("merchant endpoint returned HTTP %d", response.StatusCode)
			continue
		}
		return merchantCallResult{HTTPStatus: response.StatusCode, Response: merchantResponse}, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("merchant HTTP request failed")
	}
	return merchantCallResult{}, ErrMerchantCallFailed.WithCause(lastErr)
}

func merchantUserProfile(platformUser *dbent.User) (nickname, phone string) {
	if platformUser == nil {
		return "", ""
	}
	for _, attribute := range platformUser.Edges.AttributeValues {
		if attribute.Edges.Definition == nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(attribute.Edges.Definition.Key)) {
		case "nickname", "display_name", "name":
			nickname = strings.TrimSpace(attribute.Value)
		case "phone", "mobile", "phone_number", "mobile_phone":
			phone = strings.TrimSpace(attribute.Value)
		}
	}
	return nickname, phone
}

func addMerchantQueryValues(target *url.URL, raw any) error {
	values, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("query_template must be an object")
	}
	query := target.Query()
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		for _, item := range merchantValueStrings(value) {
			query.Add(key, item)
		}
	}
	target.RawQuery = query.Encode()
	return nil
}

func addMerchantHeaders(request *http.Request, raw any) error {
	headers, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("header_template must be an object")
	}
	for key, value := range headers {
		key = strings.TrimSpace(key)
		if key == "" || strings.EqualFold(key, "host") || strings.EqualFold(key, "content-length") || strings.EqualFold(key, "connection") || strings.EqualFold(key, "transfer-encoding") {
			continue
		}
		for _, item := range merchantValueStrings(value) {
			request.Header.Add(key, item)
		}
	}
	return nil
}

func merchantRequestBody(contentType string, raw any) ([]byte, error) {
	values, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("body_template must be an object")
	}
	if len(values) == 0 {
		return nil, nil
	}
	if contentType == "application/x-www-form-urlencoded" {
		form := url.Values{}
		for key, value := range values {
			for _, item := range merchantValueStrings(value) {
				form.Add(key, item)
			}
		}
		return []byte(form.Encode()), nil
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("encode body_template: %w", err)
	}
	return encoded, nil
}

func merchantValueStrings(value any) []string {
	switch typed := value.(type) {
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			result = append(result, merchantValueStrings(item)...)
		}
		return result
	case nil:
		return []string{""}
	case map[string]any:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return []string{fmt.Sprint(typed)}
		}
		return []string{string(encoded)}
	default:
		text, _ := merchantStringValue(typed)
		return []string{text}
	}
}

func addMerchantAuthentication(request *http.Request, endpoint *dbent.MerchantAPIEndpoint, body []byte, timestamp string) error {
	if endpoint.AuthType == "" || endpoint.AuthType == MerchantAuthNone {
		return nil
	}
	secretRef := strings.TrimSpace(endpoint.SecretRef)
	secret, ok := os.LookupEnv(secretRef)
	if !ok || strings.TrimSpace(secret) == "" {
		return fmt.Errorf("merchant auth secret is not configured")
	}
	switch endpoint.AuthType {
	case MerchantAuthAPIKey:
		request.Header.Set("X-API-Key", secret)
	case MerchantAuthBearer:
		request.Header.Set("Authorization", "Bearer "+secret)
	case MerchantAuthBasic:
		request.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(secret)))
	case MerchantAuthHMAC:
		canonical := request.Method + "\n" + request.URL.RequestURI() + "\n" + timestamp + "\n" + string(body)
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write([]byte(canonical))
		request.Header.Set("X-Signature", hex.EncodeToString(mac.Sum(nil)))
	default:
		return fmt.Errorf("unsupported merchant auth type %q", endpoint.AuthType)
	}
	return nil
}

func verifyMerchantCallbackAuthentication(request *http.Request, endpoint *dbent.MerchantAPIEndpoint, body []byte) error {
	if endpoint == nil || endpoint.AuthType == "" || endpoint.AuthType == MerchantAuthNone {
		return fmt.Errorf("callback authentication must be configured")
	}
	secret, ok := os.LookupEnv(strings.TrimSpace(endpoint.SecretRef))
	if !ok || strings.TrimSpace(secret) == "" {
		return fmt.Errorf("merchant callback auth secret is not configured")
	}
	switch endpoint.AuthType {
	case MerchantAuthAPIKey:
		if !hmac.Equal([]byte(request.Header.Get("X-API-Key")), []byte(secret)) {
			return fmt.Errorf("invalid callback api key")
		}
	case MerchantAuthBearer:
		if !hmac.Equal([]byte(request.Header.Get("Authorization")), []byte("Bearer "+secret)) {
			return fmt.Errorf("invalid callback bearer token")
		}
	case MerchantAuthBasic:
		expected := "Basic " + base64.StdEncoding.EncodeToString([]byte(secret))
		if !hmac.Equal([]byte(request.Header.Get("Authorization")), []byte(expected)) {
			return fmt.Errorf("invalid callback basic credentials")
		}
	case MerchantAuthHMAC:
		timestamp := strings.TrimSpace(request.Header.Get("X-Timestamp"))
		unix, err := strconv.ParseInt(timestamp, 10, 64)
		if err != nil || time.Since(time.Unix(unix, 0)) > 5*time.Minute || time.Since(time.Unix(unix, 0)) < -5*time.Minute {
			return fmt.Errorf("invalid or expired callback timestamp")
		}
		canonical := request.Method + "\n" + request.URL.RequestURI() + "\n" + timestamp + "\n" + string(body)
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write([]byte(canonical))
		provided, err := hex.DecodeString(strings.TrimSpace(request.Header.Get("X-Signature")))
		if err != nil || !hmac.Equal(provided, mac.Sum(nil)) {
			return fmt.Errorf("invalid callback signature")
		}
	default:
		return fmt.Errorf("unsupported callback auth type %q", endpoint.AuthType)
	}
	return nil
}

func readMerchantResponse(body io.Reader) ([]byte, error) {
	if body == nil {
		return nil, fmt.Errorf("merchant response body is empty")
	}
	limited := io.LimitReader(body, merchantMaxResponseBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read merchant response: %w", err)
	}
	if len(data) > merchantMaxResponseBytes {
		return nil, fmt.Errorf("merchant response exceeds %d bytes", merchantMaxResponseBytes)
	}
	return data, nil
}

func merchantRetryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

func validateMerchantRedirectWithAllowlist(raw string, hosts []string) (string, error) {
	return urlvalidator.ValidateHTTPURL(raw, true, urlvalidator.ValidationOptions{
		AllowedHosts:     hosts,
		RequireAllowlist: true,
		AllowPrivate:     false,
	})
}

func merchantRandomToken(size int) string {
	if size < 1 {
		size = 8
	}
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err == nil {
		return hex.EncodeToString(buffer)
	}
	digest := sha256.Sum256([]byte(time.Now().String()))
	return hex.EncodeToString(digest[:])[:size*2]
}

func merchantFormatInt(value int64) string {
	return fmt.Sprintf("%d", value)
}

var _ merchantHTTPDoer = (*http.Client)(nil)
