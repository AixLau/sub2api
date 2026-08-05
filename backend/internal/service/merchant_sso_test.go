package service

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

func TestRenderMerchantTemplate(t *testing.T) {
	context := merchantTemplateContext{
		MerchantCode:    "merchant-a",
		UserID:          42,
		Username:        "alice",
		Nickname:        "Alice",
		Email:           "alice@example.com",
		ExternalUserID:  "ext-42",
		ExternalAccount: "alice-merchant",
		RequestID:       "request-1",
		Timestamp:       "1700000000",
		Nonce:           "nonce-1",
		Query:           map[string]string{"start_time": "2026-01-01", "end_time": "2026-02-01"},
	}
	rendered := renderMerchantTemplate(map[string]any{
		"user": map[string]any{
			"id":    "{{user.id}}",
			"email": "{{user.email}}",
		},
		"binding": []any{"{{binding.external_user_id}}", "{{requestId}}"},
		"start":   "{{query.start_time}}",
	}, context)
	require.Equal(t, map[string]any{
		"user":    map[string]any{"id": "42", "email": "alice@example.com"},
		"binding": []any{"ext-42", "request-1"},
		"start":   "2026-01-01",
	}, rendered)
}

func TestEvaluateMerchantResponseFailureRuleWins(t *testing.T) {
	rule := map[string]any{
		"http":    map[string]any{"min": 200, "max": 299},
		"success": map[string]any{"path": "ok", "operator": "truthy"},
		"failure": map[string]any{"path": "blocked", "operator": "truthy"},
	}
	require.False(t, evaluateMerchantResponse(200, map[string]any{"ok": true, "blocked": true}, rule, "ok"))
	require.True(t, evaluateMerchantResponse(200, map[string]any{"ok": true}, rule, "ok"))
	require.False(t, evaluateMerchantResponse(500, map[string]any{"ok": true}, rule, "ok"))
}

func TestMerchantRedirectAllowlist(t *testing.T) {
	redirect, err := validateMerchantRedirectWithAllowlist("https://login.merchant.example/welcome/", []string{"*.merchant.example"})
	require.NoError(t, err)
	require.Equal(t, "https://login.merchant.example/welcome", redirect)
	_, err = validateMerchantRedirectWithAllowlist("https://evil.example/welcome", []string{"*.merchant.example"})
	require.Error(t, err)
	_, err = validateMerchantRedirectWithAllowlist("http://127.0.0.1:8080", []string{"127.0.0.1"})
	require.Error(t, err)
}

type merchantTestHTTPDoer struct {
	responses []*http.Response
	errors    []error
	requests  []*http.Request
}

func (d *merchantTestHTTPDoer) Do(request *http.Request) (*http.Response, error) {
	d.requests = append(d.requests, request)
	index := len(d.requests) - 1
	if index < len(d.errors) && d.errors[index] != nil {
		return nil, d.errors[index]
	}
	return d.responses[index], nil
}

func TestCallMerchantEndpointRendersRequestAndRetries(t *testing.T) {
	doer := &merchantTestHTTPDoer{
		errors: []error{errors.New("temporary network failure"), nil},
		responses: []*http.Response{
			nil,
			{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"success":true,"data":{"user_id":"external-42","account":"alice"}}`)),
			},
		},
	}
	integration := &dbent.MerchantIntegration{MerchantCode: "merchant-a"}
	endpoint := &dbent.MerchantAPIEndpoint{
		Type:          MerchantEndpointRegisterLogin,
		URL:           "https://api.example.com/sso",
		Method:        http.MethodPost,
		ContentType:   "application/json",
		QueryTemplate: map[string]any{"user": "{{user.id}}"},
		HeaderTemplate: map[string]any{
			"X-Merchant": "{{integration.merchant_code}}",
		},
		BodyTemplate: map[string]any{
			"email":    "{{user.email}}",
			"external": "{{requestId}}",
		},
		AuthType:  MerchantAuthBearer,
		SecretRef: "MERCHANT_TEST_SECRET",
		ResponseMapping: map[string]any{
			"success":         "success",
			"externalUserId":  "data.user_id",
			"externalAccount": "data.account",
		},
		RetryPolicy: map[string]any{"maxAttempts": 2, "backoffMs": 0},
		TimeoutMs:   1000,
	}
	t.Setenv("MERCHANT_TEST_SECRET", "secret-value")
	service := &MerchantSSOService{httpClient: doer}
	user := &dbent.User{ID: 42, Username: "alice", Email: "alice@example.com"}
	result, err := service.callMerchantEndpoint(context.Background(), integration, endpoint, user, nil, MerchantRechargeQuery{})
	require.NoError(t, err)
	require.True(t, result.Response.Successful)
	require.Equal(t, "external-42", result.Response.ExternalUserID)
	require.Len(t, doer.requests, 2)
	require.Equal(t, "Bearer secret-value", doer.requests[1].Header.Get("Authorization"))
	require.Equal(t, "merchant-a", doer.requests[1].Header.Get("X-Merchant"))
	require.Contains(t, doer.requests[1].URL.RawQuery, "user=42")
	body, readErr := io.ReadAll(doer.requests[1].Body)
	require.NoError(t, readErr)
	require.Contains(t, string(body), "alice@example.com")
}

func TestCallMerchantEndpointRetriesTransientHTTPStatus(t *testing.T) {
	doer := &merchantTestHTTPDoer{
		responses: []*http.Response{
			{
				StatusCode: http.StatusBadGateway,
				Body:       io.NopCloser(strings.NewReader(`{"success":false,"message":"temporary"}`)),
			},
			{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"success":true}`)),
			},
		},
	}
	endpoint := &dbent.MerchantAPIEndpoint{
		URL:            "https://api.example.com/sso",
		Method:         http.MethodPost,
		ContentType:    "application/json",
		BodyTemplate:   map[string]any{},
		HeaderTemplate: map[string]any{},
		QueryTemplate:  map[string]any{},
		RetryPolicy:    map[string]any{"maxAttempts": 2, "backoffMs": 0},
		TimeoutMs:      1000,
	}
	service := &MerchantSSOService{httpClient: doer}
	result, err := service.callMerchantEndpoint(context.Background(), &dbent.MerchantIntegration{}, endpoint, nil, nil, MerchantRechargeQuery{})
	require.NoError(t, err)
	require.True(t, result.Response.Successful)
	require.Equal(t, http.StatusOK, result.HTTPStatus)
	require.Len(t, doer.requests, 2)
}

func TestAddMerchantAuthenticationBasicUsesConfiguredSecret(t *testing.T) {
	t.Setenv("MERCHANT_BASIC_SECRET", "merchant-user:merchant-password")
	request, err := http.NewRequest(http.MethodGet, "https://api.example.com/sso", nil)
	require.NoError(t, err)
	endpoint := &dbent.MerchantAPIEndpoint{AuthType: MerchantAuthBasic, SecretRef: "MERCHANT_BASIC_SECRET"}

	err = addMerchantAuthentication(request, endpoint, nil, "1700000000")
	require.NoError(t, err)
	require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("merchant-user:merchant-password")), request.Header.Get("Authorization"))
}

func TestNewMerchantHTTPClientDoesNotFollowRedirects(t *testing.T) {
	client, ok := newMerchantHTTPClient().(*http.Client)
	require.True(t, ok)
	require.ErrorIs(t, client.CheckRedirect(nil, nil), http.ErrUseLastResponse)
}
