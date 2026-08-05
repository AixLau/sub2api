package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

func TestMerchantTokenExchangeRendersLoginToken(t *testing.T) {
	doer := &merchantTestHTTPDoer{responses: []*http.Response{{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"success":true,"data":{"redirect_url":"https://login.example.com/ticket"}}`)),
	}}}
	s := &MerchantSSOService{httpClient: doer}
	endpoint := &dbent.MerchantAPIEndpoint{
		URL: "https://api.example.com/token", Method: http.MethodPost, ContentType: "application/json",
		BodyTemplate: map[string]any{"login_token": "{{login_token}}"}, RetryPolicy: map[string]any{"maxAttempts": 1}, TimeoutMs: 1000,
	}
	result, err := s.callMerchantEndpointWithToken(context.Background(), &dbent.MerchantIntegration{}, endpoint, nil, nil, MerchantRechargeQuery{}, "opaque-token")
	require.NoError(t, err)
	require.Equal(t, "https://login.example.com/ticket", result.Response.RedirectURL)
	body, err := io.ReadAll(doer.requests[0].Body)
	require.NoError(t, err)
	require.Contains(t, string(body), `"login_token":"opaque-token"`)
}

func TestVerifyMerchantCallbackRejectsUnauthenticatedAndNone(t *testing.T) {
	t.Setenv("MERCHANT_CALLBACK_SECRET", "callback-secret")
	request, err := http.NewRequest(http.MethodPost, "https://sub2api.example.com/api/v1/merchant-integrations/1/callback", strings.NewReader(`{"external_user_id":"ext-1"}`))
	require.NoError(t, err)
	require.Error(t, verifyMerchantCallbackAuthentication(request, &dbent.MerchantAPIEndpoint{AuthType: MerchantAuthNone}, []byte(`{"external_user_id":"ext-1"}`)))
	require.Error(t, verifyMerchantCallbackAuthentication(request, &dbent.MerchantAPIEndpoint{AuthType: MerchantAuthBearer, SecretRef: "MERCHANT_CALLBACK_SECRET"}, []byte(`{"external_user_id":"ext-1"}`)))
}

func TestVerifyMerchantCallbackHMAC(t *testing.T) {
	t.Setenv("MERCHANT_CALLBACK_SECRET", "callback-secret")
	body := []byte(`{"external_user_id":"ext-1","status":"active"}`)
	request, err := http.NewRequest(http.MethodPost, "https://sub2api.example.com/api/v1/merchant-integrations/1/callback?x=1", strings.NewReader(string(body)))
	require.NoError(t, err)
	timestamp := merchantFormatInt(time.Now().Unix())
	canonical := request.Method + "\n" + request.URL.RequestURI() + "\n" + timestamp + "\n" + string(body)
	mac := hmac.New(sha256.New, []byte("callback-secret"))
	_, _ = mac.Write([]byte(canonical))
	request.Header.Set("X-Timestamp", timestamp)
	request.Header.Set("X-Signature", hex.EncodeToString(mac.Sum(nil)))
	require.NoError(t, verifyMerchantCallbackAuthentication(request, &dbent.MerchantAPIEndpoint{AuthType: MerchantAuthHMAC, SecretRef: "MERCHANT_CALLBACK_SECRET"}, body))
}
