package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

func TestMerchantSSORequestAuthenticationContract(t *testing.T) {
	tests := []struct {
		name     string
		authType string
		want     func(*http.Request) string
	}{
		{name: "api key", authType: MerchantAuthAPIKey, want: func(_ *http.Request) string { return "merchant-secret" }},
		{name: "bearer", authType: MerchantAuthBearer, want: func(_ *http.Request) string { return "Bearer merchant-secret" }},
		{name: "basic", authType: MerchantAuthBasic, want: func(_ *http.Request) string {
			return "Basic " + base64.StdEncoding.EncodeToString([]byte("merchant-secret"))
		}},
		{name: "hmac", authType: MerchantAuthHMAC, want: func(request *http.Request) string {
			body := `{"user_id":"42"}`
			canonical := request.Method + "\n" + request.URL.RequestURI() + "\n" + request.Header.Get("X-Timestamp") + "\n" + body
			mac := hmac.New(sha256.New, []byte("merchant-secret"))
			_, _ = mac.Write([]byte(canonical))
			return hex.EncodeToString(mac.Sum(nil))
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("MERCHANT_CONTRACT_SECRET", "merchant-secret")
			doer := &merchantTestHTTPDoer{responses: []*http.Response{{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"success":true}`)),
			}}}
			svc := &MerchantSSOService{httpClient: doer}
			endpoint := &dbent.MerchantAPIEndpoint{
				Type:           MerchantEndpointLogin,
				URL:            "https://api.merchant.example/login?fixed=1",
				Method:         http.MethodPost,
				ContentType:    "application/json",
				BodyTemplate:   map[string]any{"user_id": "42"},
				HeaderTemplate: map[string]any{},
				QueryTemplate:  map[string]any{},
				AuthType:       tt.authType,
				SecretRef:      "MERCHANT_CONTRACT_SECRET",
				RetryPolicy:    map[string]any{"maxAttempts": 1},
			}
			_, err := svc.callMerchantEndpoint(context.Background(), &dbent.MerchantIntegration{}, endpoint, nil, nil, MerchantRechargeQuery{})
			require.NoError(t, err)
			require.Len(t, doer.requests, 1)
			require.Equal(t, tt.want(doer.requests[0]), authenticationHeader(tt.authType, doer.requests[0]))
		})
	}
}

func authenticationHeader(authType string, request *http.Request) string {
	switch authType {
	case MerchantAuthAPIKey:
		return request.Header.Get("X-API-Key")
	case MerchantAuthBearer, MerchantAuthBasic, MerchantAuthHMAC:
		if authType == MerchantAuthHMAC {
			return request.Header.Get("X-Signature")
		}
		return request.Header.Get("Authorization")
	default:
		return ""
	}
}

func TestMerchantSSOLaunchRegisterLoginIsIdempotentPerPlatformUser(t *testing.T) {
	client := newMerchantSSOTestClient(t)
	svc := &MerchantSSOService{client: client}
	doer := &merchantTestHTTPDoer{responses: []*http.Response{
		{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"success":true,"data":{"user_id":"merchant-user-1","account":"alice","redirect_url":"https://login.merchant.example/first"}}`))},
		{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"success":true,"data":{"redirect_url":"https://login.merchant.example/again"}}`))},
	}}
	svc.SetHTTPClient(doer)
	integration := createMerchantIntegration(t, client, "idempotent-merchant", []string{"login.merchant.example"})
	user := createMerchantUser(t, client, "merchant-idempotent@example.com", "merchant-idempotent")
	createMerchantEndpoint(t, client, integration.ID, MerchantEndpointRegisterLogin, "https://api.merchant.example/register-login", nil, nil)
	createMerchantEndpoint(t, client, integration.ID, MerchantEndpointLogin, "https://api.merchant.example/login", nil, nil)
	createMerchantEndpoint(t, client, integration.ID, MerchantEndpointRecharge, "https://api.merchant.example/recharge", nil, nil)

	first, err := svc.Launch(context.Background(), integration.ID, user.ID)
	require.NoError(t, err)
	second, err := svc.Launch(context.Background(), integration.ID, user.ID)
	require.NoError(t, err)
	require.Equal(t, "https://login.merchant.example/first", first.RedirectURL)
	require.Equal(t, "https://login.merchant.example/again", second.RedirectURL)
	require.Equal(t, 1, client.MerchantBinding.Query().CountX(context.Background()))
	require.Equal(t, 2, doer.requestCount())
}

func TestMerchantSSOLaunchRejectsLoginRedirectOutsideAllowlist(t *testing.T) {
	client := newMerchantSSOTestClient(t)
	doer := &merchantTestHTTPDoer{responses: []*http.Response{{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"success":true,"data":{"redirect_url":"https://evil.example/sso"}}`))}}}
	svc := &MerchantSSOService{client: client, httpClient: doer}
	integration := createMerchantIntegration(t, client, "redirect-merchant", []string{"login.merchant.example"})
	user := createMerchantUser(t, client, "merchant-redirect@example.com", "merchant-redirect")
	createMerchantEndpoint(t, client, integration.ID, MerchantEndpointLogin, "https://api.merchant.example/login", nil, nil)
	_, err := client.MerchantBinding.Create().SetIntegrationID(integration.ID).SetUserID(user.ID).SetExternalUserID("merchant-user-2").SetStatus("active").Save(context.Background())
	require.NoError(t, err)

	result, err := svc.Launch(context.Background(), integration.ID, user.ID)
	require.Nil(t, result)
	require.Error(t, err)
}

func TestMerchantSSORechargeRecordsMapFieldsAndDeduplicate(t *testing.T) {
	// The shared merchant helper uses SQLite; its driver returns the Ent
	// timestamptz column as text and cannot scan it back into time.Time. This
	// contract is covered by the PostgreSQL integration suite instead.
	t.Skip("requires PostgreSQL because SQLite cannot scan merchant_created_at timestamptz")
	client := newMerchantSSOTestClient(t)
	recordsResponse := `{"success":true,"data":{"records":[{"order_no":"R-100","user_id":"merchant-user-3","amount":"100.00","currency":"CNY","balance_before":"0.00","balance_after":"100.00","charge_type":"recharge","pay_method":"wechat","status":"success","platform_order_no":"wx-100","created_at":"2026-01-01T10:00:00+08:00"}]}}`
	doer := &merchantTestHTTPDoer{responses: []*http.Response{
		{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(recordsResponse))},
		{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(recordsResponse))},
	}}
	svc := &MerchantSSOService{client: client, httpClient: doer}
	integration := createMerchantIntegration(t, client, "recharge-merchant", []string{"login.merchant.example"})
	user := createMerchantUser(t, client, "merchant-recharge@example.com", "merchant-recharge")
	binding, err := client.MerchantBinding.Create().SetIntegrationID(integration.ID).SetUserID(user.ID).SetExternalUserID("merchant-user-3").SetExternalAccount("alice").SetStatus("active").Save(context.Background())
	require.NoError(t, err)
	createMerchantEndpoint(t, client, integration.ID, MerchantEndpointRecharge, "https://api.merchant.example/recharge", nil, nil)

	first, err := svc.SyncRechargeRecords(context.Background(), user.ID, binding.ID, MerchantRechargeQuery{})
	require.NoError(t, err)
	second, err := svc.SyncRechargeRecords(context.Background(), user.ID, binding.ID, MerchantRechargeQuery{})
	require.NoError(t, err)
	require.Len(t, first.Records, 1)
	require.Len(t, second.Records, 1)
	require.Equal(t, "merchant-user-3", second.Records[0].UserID)
	require.Equal(t, "R-100", second.Records[0].OrderNo)
	require.Equal(t, "100.00", second.Records[0].Amount)
	require.Equal(t, "CNY", second.Records[0].Currency)
	require.Equal(t, "0.00", second.Records[0].BalanceBefore)
	require.Equal(t, "100.00", second.Records[0].BalanceAfter)
	require.Equal(t, "recharge", second.Records[0].ChargeType)
	require.Equal(t, "wechat", second.Records[0].PayMethod)
	require.Equal(t, "success", second.Records[0].Status)
	require.Equal(t, "wx-100", second.Records[0].PlatformOrderNo)
	require.Equal(t, 1, client.MerchantRechargeRecord.Query().CountX(context.Background()))
}

func (d *merchantTestHTTPDoer) requestCount() int { return len(d.requests) }
