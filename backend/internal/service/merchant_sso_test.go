package service

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
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
			"id":       "{{user.id}}",
			"username": "{{user.username}}",
			"nickname": "{{user.nickname}}",
			"email":    "{{user.email}}",
			"phone":    "{{user.phone}}",
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
	var requestPayload map[string]any
	require.NoError(t, json.Unmarshal(body, &requestPayload))
	require.Equal(t, "42", requestPayload["id"])
	require.Equal(t, "alice", requestPayload["username"])
	require.Equal(t, "alice", requestPayload["nickname"])
	require.Equal(t, "alice@example.com", requestPayload["email"])
	require.Equal(t, "", requestPayload["phone"])
	require.Equal(t, "alice@example.com", requestPayload["email"])
}

func TestCallMerchantEndpointParsesTokenOnlyResponse(t *testing.T) {
	doer := &merchantTestHTTPDoer{
		responses: []*http.Response{{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"success":true,"data":{"login_token":"token-42"}}`)),
		}},
	}
	service := &MerchantSSOService{httpClient: doer}
	endpoint := &dbent.MerchantAPIEndpoint{
		URL:          "https://api.example.com/login",
		Method:       http.MethodPost,
		ContentType:  "application/json",
		BodyTemplate: map[string]any{},
		RetryPolicy:  map[string]any{"maxAttempts": 1},
		TimeoutMs:    1000,
	}

	result, err := service.callMerchantEndpoint(context.Background(), &dbent.MerchantIntegration{}, endpoint, nil, nil, MerchantRechargeQuery{})
	require.NoError(t, err)
	require.True(t, result.Response.Successful)
	require.Equal(t, "token-42", result.Response.LoginToken)
	require.Empty(t, result.Response.RedirectURL)
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

func TestLaunchUsesTokenEndpointExchangeWhenLoginReturnsLoginToken(t *testing.T) {
	client := newMerchantSSOTestClient(t)
	svc := &MerchantSSOService{client: client}
	doer := &merchantTestHTTPDoer{
		responses: []*http.Response{
			{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(`{
					"success": true,
					"data": {
						"login_token": "token-42"
					}
				}`)),
			},
			{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(`{
					"success": true,
					"data": {
						"redirect_url": "https://login.merchant.example/welcome?ticket=abc",
						"external_user_id": "ext-42",
						"account": "alice-merchant"
					}
				}`)),
			},
		},
	}
	svc.SetHTTPClient(doer)

	ctx := context.Background()
	integration := createMerchantIntegration(t, client, "merchant-a", []string{"login.merchant.example"})
	user := createMerchantUser(t, client, "alice@example.com", "alice")
	createMerchantEndpoint(t, client, integration.ID, MerchantEndpointRegisterLogin, "https://merchant.example/register-login", map[string]any{
		"user": "{{user.id}}",
	}, map[string]any{})
	createMerchantEndpoint(t, client, integration.ID, MerchantEndpointLogin, "https://merchant.example/login", map[string]any{
		"login_token": "{{login_token}}",
		"binding":     "{{binding.external_user_id}}",
	}, map[string]any{})
	createMerchantEndpoint(t, client, integration.ID, MerchantEndpointToken, "https://merchant.example/token", map[string]any{
		"login_token": "{{login_token}}",
		"binding":     "{{binding.external_user_id}}",
	}, map[string]any{})
	createMerchantEndpoint(t, client, integration.ID, MerchantEndpointRecharge, "https://merchant.example/recharge", map[string]any{}, map[string]any{})
	_, err := client.MerchantBinding.Create().
		SetIntegrationID(integration.ID).
		SetUserID(user.ID).
		SetExternalUserID("ext-42").
		SetExternalAccount("alice-merchant").
		SetStatus("active").
		Save(ctx)
	require.NoError(t, err)

	result, err := svc.Launch(ctx, integration.ID, user.ID)
	require.NoError(t, err)
	require.Equal(t, "https://login.merchant.example/welcome?ticket=abc", result.RedirectURL)
	require.Equal(t, "ext-42", result.ExternalUserID)
	require.Equal(t, "alice-merchant", result.ExternalAccount)
	require.Len(t, doer.requests, 2)
	body, err := io.ReadAll(doer.requests[1].Body)
	require.NoError(t, err)
	require.Contains(t, string(body), `"login_token":"token-42"`)
	require.Contains(t, string(body), `"binding":"ext-42"`)

	binding, err := client.MerchantBinding.Get(ctx, result.BindingID)
	require.NoError(t, err)
	require.Equal(t, "active", binding.Status)
	require.NotNil(t, binding.LastLoginAt)
}

func TestLaunchRejectsLoginTokenWithoutTokenEndpoint(t *testing.T) {
	client := newMerchantSSOTestClient(t)
	svc := &MerchantSSOService{client: client}
	doer := &merchantTestHTTPDoer{
		responses: []*http.Response{
			{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(`{
					"success": true,
					"data": {
						"login_token": "token-42"
					}
				}`)),
			},
		},
	}
	svc.SetHTTPClient(doer)

	ctx := context.Background()
	integration := createMerchantIntegration(t, client, "merchant-b", []string{"login.merchant.example"})
	user := createMerchantUser(t, client, "bob@example.com", "bob")
	createMerchantEndpoint(t, client, integration.ID, MerchantEndpointRegisterLogin, "https://merchant.example/register-login", map[string]any{}, map[string]any{})
	createMerchantEndpoint(t, client, integration.ID, MerchantEndpointLogin, "https://merchant.example/login", map[string]any{}, map[string]any{})
	createMerchantEndpoint(t, client, integration.ID, MerchantEndpointRecharge, "https://merchant.example/recharge", map[string]any{}, map[string]any{})
	_, err := client.MerchantBinding.Create().
		SetIntegrationID(integration.ID).
		SetUserID(user.ID).
		SetExternalUserID("ext-43").
		SetStatus("active").
		Save(ctx)
	require.NoError(t, err)

	result, err := svc.Launch(ctx, integration.ID, user.ID)
	require.Nil(t, result)
	require.ErrorContains(t, err, "configure a token endpoint for login_token exchange")
}

func TestHandleCallbackUpdatesExistingBinding(t *testing.T) {
	client := newMerchantSSOTestClient(t)
	svc := &MerchantSSOService{client: client}
	ctx := context.Background()
	integration := createMerchantIntegration(t, client, "merchant-c", []string{"callback.merchant.example"})
	t.Setenv("MERCHANT_CALLBACK_SECRET", "callback-secret")
	createMerchantEndpointWithAuth(t, client, integration.ID, MerchantEndpointCallback, "https://merchant.example/callback", MerchantAuthAPIKey, "MERCHANT_CALLBACK_SECRET", map[string]any{}, map[string]any{
		"externalUserId":  "data.identity",
		"externalAccount": "data.account_name",
		"status":          "data.state",
	})
	user := createMerchantUser(t, client, "carol@example.com", "carol")
	_, err := client.MerchantBinding.Create().
		SetIntegrationID(integration.ID).
		SetUserID(user.ID).
		SetExternalUserID("ext-44").
		SetExternalAccount("carol-old").
		SetStatus("active").
		Save(ctx)
	require.NoError(t, err)

	body := []byte(`{"data":{"identity":"ext-44","account_name":"carol-new","state":"inactive"}}`)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://merchant.example/callback", strings.NewReader(string(body)))
	require.NoError(t, err)
	request.Header.Set("X-API-Key", "callback-secret")

	var payload map[string]any
	require.NoError(t, json.Unmarshal(body, &payload))
	result, err := svc.HandleCallback(ctx, integration.ID, request, body, payload)
	require.NoError(t, err)
	require.Equal(t, map[string]any{
		"data": map[string]any{
			"identity":     "ext-44",
			"account_name": "carol-new",
			"state":        "inactive",
		},
	}, result.Response)

	binding, err := client.MerchantBinding.Get(ctx, result.Binding.ID)
	require.NoError(t, err)
	require.Equal(t, "disabled", binding.Status)
	require.Equal(t, "carol-new", binding.ExternalAccount)
	require.NotNil(t, binding.LastSyncAt)
}

func TestBindingActionsSyncBindAndStatus(t *testing.T) {
	t.Run("sync", func(t *testing.T) {
		testMerchantBindingAction(t, MerchantEndpointSync, map[string]any{
			"data": map[string]any{
				"user_id": "ext-sync",
				"account": "alice-sync",
			},
		}, "active")
	})
	t.Run("bind", func(t *testing.T) {
		testMerchantBindingAction(t, MerchantEndpointBind, map[string]any{
			"data": map[string]any{
				"user_id": "ext-bind",
				"account": "alice-bind",
			},
		}, "active")
	})
	t.Run("status", func(t *testing.T) {
		testMerchantBindingAction(t, MerchantEndpointStatus, map[string]any{
			"data": map[string]any{
				"user_id": "ext-status",
				"account": "alice-status",
				"status":  "inactive",
			},
		}, "disabled")
	})
}

func TestTestEndpointLoadsUserParameters(t *testing.T) {
	client := newMerchantSSOTestClient(t)
	svc := &MerchantSSOService{client: client}
	doer := &merchantTestHTTPDoer{
		responses: []*http.Response{
			{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(`{
					"success": true,
					"data": {
						"records": []
					}
				}`)),
			},
		},
	}
	svc.SetHTTPClient(doer)

	ctx := context.Background()
	integration := createMerchantIntegration(t, client, "merchant-d", []string{"test.merchant.example"})
	user := createMerchantUser(t, client, "dana@example.com", "dana")
	endpoint := createMerchantEndpoint(t, client, integration.ID, MerchantEndpointRecharge, "https://merchant.example/recharge", map[string]any{
		"user":  "{{user.id}}",
		"name":  "{{user.username}}",
		"mail":  "{{user.email}}",
		"start": "{{query.start_time}}",
		"end":   "{{query.end_time}}",
	}, map[string]any{
		"recordsPath": "data.records",
	})

	result, err := svc.TestEndpoint(ctx, integration.ID, endpoint.ID, user.ID, MerchantRechargeQuery{
		StartTime: "2026-01-01T00:00:00Z",
		EndTime:   "2026-02-01T00:00:00Z",
	})
	require.NoError(t, err)
	require.True(t, result.Successful)
	require.Len(t, doer.requests, 1)
	body, err := io.ReadAll(doer.requests[0].Body)
	require.NoError(t, err)
	require.Contains(t, string(body), `"user":"`+fmt.Sprint(user.ID)+`"`)
	require.Contains(t, string(body), `"name":"dana"`)
	require.Contains(t, string(body), `"mail":"dana@example.com"`)
	require.Contains(t, string(body), `"start":"2026-01-01T00:00:00Z"`)
	require.Contains(t, string(body), `"end":"2026-02-01T00:00:00Z"`)
}

func newMerchantSSOTestClient(t *testing.T) *dbent.Client {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+sanitizeMerchantSSOTestName(t.Name())+"?mode=memory&cache=shared&_fk=1")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() {
		require.NoError(t, client.Close())
	})
	return client
}

func sanitizeMerchantSSOTestName(name string) string {
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, " ", "_")
	return "merchant_sso_" + name
}

func createMerchantUser(t *testing.T, client *dbent.Client, email, username string) *dbent.User {
	t.Helper()
	user, err := client.User.Create().
		SetEmail(email).
		SetPasswordHash("password-hash").
		SetRole(RoleUser).
		SetBalance(0).
		SetConcurrency(5).
		SetStatus(StatusActive).
		SetUsername(username).
		Save(context.Background())
	require.NoError(t, err)
	return user
}

func createMerchantIntegration(t *testing.T, client *dbent.Client, code string, redirectHosts []string) *dbent.MerchantIntegration {
	t.Helper()
	row, err := client.MerchantIntegration.Create().
		SetName(code).
		SetCode(code).
		SetMode(MerchantAPIMode).
		SetMerchantCode(code + "-merchant").
		SetDescription("test integration").
		SetStatus(MerchantStatusActive).
		SetEnabled(true).
		SetRedirectHosts(redirectHosts).
		Save(context.Background())
	require.NoError(t, err)
	return row
}

func createMerchantEndpoint(t *testing.T, client *dbent.Client, integrationID int64, endpointType, url string, bodyTemplate map[string]any, responseMapping map[string]any) *dbent.MerchantAPIEndpoint {
	t.Helper()
	return createMerchantEndpointWithAuth(t, client, integrationID, endpointType, url, MerchantAuthNone, "", bodyTemplate, responseMapping)
}

func createMerchantEndpointWithAuth(t *testing.T, client *dbent.Client, integrationID int64, endpointType, url, authType, secretRef string, bodyTemplate map[string]any, responseMapping map[string]any) *dbent.MerchantAPIEndpoint {
	t.Helper()
	if bodyTemplate == nil {
		bodyTemplate = map[string]any{}
	}
	if responseMapping == nil {
		responseMapping = map[string]any{}
	}
	row, err := client.MerchantAPIEndpoint.Create().
		SetIntegrationID(integrationID).
		SetType(endpointType).
		SetURL(url).
		SetMethod(http.MethodPost).
		SetContentType("application/json").
		SetQueryTemplate(map[string]any{}).
		SetHeaderTemplate(map[string]any{}).
		SetBodyTemplate(bodyTemplate).
		SetAuthType(authType).
		SetSecretRef(secretRef).
		SetResponseMapping(responseMapping).
		SetSuccessRule(map[string]any{}).
		SetRetryPolicy(map[string]any{"maxAttempts": 1, "backoffMs": 0}).
		SetTimeoutMs(1000).
		SetStatus(MerchantEndpointStatusActive).
		SetEnabled(true).
		Save(context.Background())
	require.NoError(t, err)
	return row
}

func testMerchantBindingAction(t *testing.T, endpointType string, payload map[string]any, expectedStatus string) {
	t.Helper()
	client := newMerchantSSOTestClient(t)
	svc := &MerchantSSOService{client: client}
	doer := &merchantTestHTTPDoer{
		responses: []*http.Response{
			{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(mustJSON(t, map[string]any{
					"success": true,
					"data":    payload["data"],
				}))),
			},
		},
	}
	svc.SetHTTPClient(doer)

	ctx := context.Background()
	integration := createMerchantIntegration(t, client, "merchant-"+endpointType, []string{"action.merchant.example"})
	user := createMerchantUser(t, client, "action-"+endpointType+"@example.com", endpointType)
	createMerchantEndpoint(t, client, integration.ID, endpointType, "https://merchant.example/"+endpointType, map[string]any{}, map[string]any{})
	binding, err := client.MerchantBinding.Create().
		SetIntegrationID(integration.ID).
		SetUserID(user.ID).
		SetExternalUserID("ext-" + endpointType).
		SetExternalAccount("acct-" + endpointType).
		SetStatus("active").
		Save(ctx)
	require.NoError(t, err)

	var result *MerchantBindingActionResult
	switch endpointType {
	case MerchantEndpointSync:
		result, err = svc.SyncBinding(ctx, user.ID, binding.ID)
	case MerchantEndpointBind:
		result, err = svc.BindBinding(ctx, user.ID, binding.ID)
	case MerchantEndpointStatus:
		result, err = svc.StatusBinding(ctx, user.ID, binding.ID)
	default:
		t.Fatalf("unsupported endpoint type %q", endpointType)
	}
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.HTTPStatus)
	require.Equal(t, payload, result.Response)

	stored, err := client.MerchantBinding.Get(ctx, binding.ID)
	require.NoError(t, err)
	require.Equal(t, expectedStatus, stored.Status)
	require.NotNil(t, stored.LastSyncAt)
	require.Equal(t, "ext-"+endpointType, stored.ExternalUserID)
	require.Equal(t, "acct-"+endpointType, stored.ExternalAccount)

	body, err := io.ReadAll(doer.requests[0].Body)
	require.NoError(t, err)
	require.Contains(t, string(body), `"binding":"ext-`+endpointType+`"`)
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	return string(raw)
}
