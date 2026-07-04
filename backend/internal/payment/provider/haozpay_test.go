//go:build unit

package provider

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
)

func TestHaozPayParseRSAPublicKeyAcceptsRawBase64DER(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}

	rawBase64 := base64.StdEncoding.EncodeToString(pubDER)
	parsed, err := parseRSAPublicKey(rawBase64)
	if err != nil {
		t.Fatalf("parse raw base64 public key: %v", err)
	}

	if parsed.N.Cmp(key.PublicKey.N) != 0 || parsed.E != key.PublicKey.E {
		t.Fatal("parsed public key does not match original")
	}
}

func TestHaozPayParseRSAPrivateKeyAcceptsRawBase64DER(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}

	rawBase64 := base64.StdEncoding.EncodeToString(privDER)
	parsed, err := parseRSAPrivateKey(rawBase64)
	if err != nil {
		t.Fatalf("parse raw base64 private key: %v", err)
	}

	if parsed.N.Cmp(key.N) != 0 || parsed.E != key.E || parsed.D.Cmp(key.D) != 0 {
		t.Fatal("parsed private key does not match original")
	}
}

func TestHaozPayParseRSAKeysStillAcceptPEM(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}

	privPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER}))
	pubPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}))

	if _, err := parseRSAPrivateKey(privPEM); err != nil {
		t.Fatalf("parse PEM private key: %v", err)
	}
	if _, err := parseRSAPublicKey(pubPEM); err != nil {
		t.Fatalf("parse PEM public key: %v", err)
	}
}

func TestHaozPaySignRequestEncryptsSHA256Hex(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	h := &HaozPay{privateKey: key}
	reqBody := map[string]interface{}{
		"merchantNo": "M123456",
		"timestamp":  int64(1710000000123),
		"bizBody": map[string]interface{}{
			"orderAmount":       "10.00",
			"orderTitle":        "Balance recharge",
			"payType":           0,
			"useHaozPayCashier": true,
			"notifyUrl":         "https://example.com/api/v1/payment/webhook/haozpay",
		},
	}

	sign, err := h.signRequest(reqBody)
	if err != nil {
		t.Fatalf("sign request: %v", err)
	}
	signature, err := base64.StdEncoding.DecodeString(sign)
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	decrypted, err := haozPayTestPublicDecrypt(&key.PublicKey, signature)
	if err != nil {
		t.Fatalf("public decrypt signature: %v", err)
	}
	expectedHex := fmt.Sprintf("%x", sha256.Sum256([]byte(haozPayTestSignString(reqBody))))

	if string(decrypted) != expectedHex {
		t.Fatalf("decrypted signature = %q, want %q", string(decrypted), expectedHex)
	}
}

func TestHaozPayCreatePaymentSendsBizBodyAsJSONString(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}

	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request: %v", err)
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"seqId":"HP123","payInfo":"https://cashier.example/pay","merchantOrderNo":"ORD123","orderAmount":10.00}}`))
	}))
	defer server.Close()

	h := &HaozPay{
		config: map[string]string{
			"notifyUrl":   "https://example.com/api/v1/payment/webhook/haozpay",
			"redirectUrl": "https://example.com/payment/result",
		},
		httpClient: &http.Client{Transport: rewriteHostTransport(server.URL)},
		merchantNo: "M123456",
		privateKey: key,
	}

	resp, err := h.CreatePayment(context.Background(), payment.CreatePaymentRequest{
		Amount:      "10.00",
		OrderID:     "sub2_haozpay_123",
		PaymentType: payment.TypeAlipay,
		Subject:     "Balance recharge",
	})
	if err != nil {
		t.Fatalf("create payment: %v", err)
	}
	if resp.TradeNo != "HP123" || resp.QRCode != "https://cashier.example/pay" || resp.PayURL != "" {
		t.Fatalf("response = %+v", resp)
	}

	bizBodyString, ok := captured["bizBody"].(string)
	if !ok {
		t.Fatalf("bizBody type = %T, want string", captured["bizBody"])
	}
	var bizBody map[string]interface{}
	bizDecoder := json.NewDecoder(strings.NewReader(bizBodyString))
	bizDecoder.UseNumber()
	if err := bizDecoder.Decode(&bizBody); err != nil {
		t.Fatalf("decode bizBody string: %v", err)
	}
	if got := fmt.Sprint(bizBody["orderAmount"]); got != "10.00" {
		t.Fatalf("orderAmount = %#v, want numeric 10.00", bizBody["orderAmount"])
	}
	if bizBody["orderNo"] != "sub2_haozpay_123" {
		t.Fatalf("orderNo = %#v, want sub2_haozpay_123", bizBody["orderNo"])
	}
	if bizBody["useHaozPayCashier"] != true {
		t.Fatalf("useHaozPayCashier = %#v, want true", bizBody["useHaozPayCashier"])
	}
	if bizBody["notifyUrl"] != "https://example.com/api/v1/payment/webhook/haozpay" {
		t.Fatalf("notifyUrl = %#v", bizBody["notifyUrl"])
	}

	signature, err := base64.StdEncoding.DecodeString(fmt.Sprint(captured["sign"]))
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	decrypted, err := haozPayTestPublicDecrypt(&key.PublicKey, signature)
	if err != nil {
		t.Fatalf("public decrypt signature: %v", err)
	}
	expectedHex := fmt.Sprintf("%x", sha256.Sum256([]byte(haozPayTestSignString(captured))))
	if string(decrypted) != expectedHex {
		t.Fatalf("decrypted signature = %q, want %q", string(decrypted), expectedHex)
	}
}

func TestHaozPayCreatePaymentExtractsPlatformOrderNoFromCashierURL(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"seqId":"HP_REQUEST_SEQ","payInfo":"https://cashier.haozpay.com/cashier-pc?orderNo=HZHT202607042073263983919542272&merchantNo=HZ2072997091048607744","merchantOrderNo":"20260704BIdEeZRA","orderAmount":1.01}}`))
	}))
	defer server.Close()

	h := &HaozPay{
		config: map[string]string{
			"notifyUrl": "https://example.com/api/v1/payment/webhook/haozpay",
		},
		httpClient: &http.Client{Transport: rewriteHostTransport(server.URL)},
		merchantNo: "M123456",
		privateKey: key,
	}

	resp, err := h.CreatePayment(context.Background(), payment.CreatePaymentRequest{
		Amount:      "1.01",
		OrderID:     "20260704BIdEeZRA",
		PaymentType: payment.TypeAlipay,
		Subject:     "Balance recharge",
	})
	if err != nil {
		t.Fatalf("create payment: %v", err)
	}
	if resp.TradeNo != "HZHT202607042073263983919542272" {
		t.Fatalf("TradeNo = %q, want cashier orderNo", resp.TradeNo)
	}
}

func TestHaozPayVerifyNotificationAcceptsFlatPaymentCallback(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	h := &HaozPay{
		merchantNo:     "M123456",
		platformPubKey: &key.PublicKey,
	}

	params := map[string]interface{}{
		"orderNo":     "ORDER123",
		"merchantNo":  "M123456",
		"orderAmount": "100.00",
		"payAmount":   "100.00",
		"payType":     "2",
		"payChannel":  "HFDG",
		"payStatus":   2,
		"feeAmount":   "0.60",
		"payTime":     "2023-11-28 12:34:56",
		"createTime":  "2023-11-28 12:30:00",
		"timestamp":   int64(1701148496000),
	}
	sign, err := haozPayTestPrivateEncryptSign(key, params)
	if err != nil {
		t.Fatalf("sign callback: %v", err)
	}
	params["sign"] = sign
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal callback: %v", err)
	}

	notification, err := h.VerifyNotification(context.Background(), string(raw), nil)
	if err != nil {
		t.Fatalf("verify notification: %v", err)
	}
	if notification.OrderID != "ORDER123" {
		t.Fatalf("OrderID = %q, want ORDER123", notification.OrderID)
	}
	if notification.TradeNo != "ORDER123" {
		t.Fatalf("TradeNo = %q, want ORDER123 fallback", notification.TradeNo)
	}
	if notification.Status != payment.ProviderStatusSuccess {
		t.Fatalf("Status = %q, want success", notification.Status)
	}
	if notification.Amount != 100 {
		t.Fatalf("Amount = %v, want 100", notification.Amount)
	}
}

func TestHaozPayVerifyNotificationUsesOrderAmountWhenPayAmountIsNet(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	h := &HaozPay{
		merchantNo:     "M123456",
		platformPubKey: &key.PublicKey,
	}

	params := map[string]interface{}{
		"orderNo":         "HZHT202607042073412271199350784",
		"merchantOrderNo": "20260704naCD4hyY",
		"merchantNo":      "M123456",
		"orderAmount":     "10.00",
		"payAmount":       "9.92",
		"payType":         "0",
		"payChannel":      "HFDG",
		"payStatus":       2,
		"feeAmount":       "0.08",
		"payTime":         "2026-07-04 22:23:26",
		"createTime":      "2026-07-04 22:23:02",
		"timestamp":       int64(1783175006000),
	}
	sign, err := haozPayTestPrivateEncryptSign(key, params)
	if err != nil {
		t.Fatalf("sign callback: %v", err)
	}
	params["sign"] = sign
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal callback: %v", err)
	}

	notification, err := h.VerifyNotification(context.Background(), string(raw), nil)
	if err != nil {
		t.Fatalf("verify notification: %v", err)
	}
	if notification.Amount != 10 {
		t.Fatalf("Amount = %v, want gross order amount 10", notification.Amount)
	}
}

func TestHaozPayVerifyNotificationUsesMerchantOrderNoAsLocalOrderID(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	h := &HaozPay{
		merchantNo:     "M123456",
		platformPubKey: &key.PublicKey,
	}

	params := map[string]interface{}{
		"orderNo":         "HZHT202607042073255420652335104",
		"merchantOrderNo": "20260704d7HL7QV0",
		"merchantNo":      "M123456",
		"orderAmount":     "1.01",
		"payAmount":       "1.01",
		"payType":         "0",
		"payChannel":      "HFDG",
		"payStatus":       2,
		"payTime":         "2026-07-04 12:00:09",
		"createTime":      "2026-07-04 11:59:45",
		"timestamp":       int64(1783137609000),
	}
	sign, err := haozPayTestPrivateEncryptSign(key, params)
	if err != nil {
		t.Fatalf("sign callback: %v", err)
	}
	params["sign"] = sign
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal callback: %v", err)
	}

	notification, err := h.VerifyNotification(context.Background(), string(raw), nil)
	if err != nil {
		t.Fatalf("verify notification: %v", err)
	}
	if notification.OrderID != "20260704d7HL7QV0" {
		t.Fatalf("OrderID = %q, want merchantOrderNo", notification.OrderID)
	}
	if notification.TradeNo != "HZHT202607042073255420652335104" {
		t.Fatalf("TradeNo = %q, want platform orderNo", notification.TradeNo)
	}
	if notification.Status != payment.ProviderStatusSuccess {
		t.Fatalf("Status = %q, want success", notification.Status)
	}
}

func TestHaozPayVerifyNotificationDoesNotTreatRefundCallbackAsPaymentSuccess(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	h := &HaozPay{
		merchantNo:     "M123456",
		platformPubKey: &key.PublicKey,
	}

	params := map[string]interface{}{
		"merchantNo":        "M123456",
		"orderNo":           "ORDER123",
		"reqSeqId":          "R20231128123456",
		"transStat":         "S",
		"ordAmt":            "100.00",
		"transDate":         "20231128",
		"transTime":         "123456",
		"transFinishTime":   "2023-11-28 12:35:00",
		"refundStatusField": "documents refund callbacks are not payment callbacks",
		"timestamp":         int64(1701148500000),
	}
	sign, err := haozPayTestPrivateEncryptSign(key, params)
	if err != nil {
		t.Fatalf("sign callback: %v", err)
	}
	params["sign"] = sign
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal callback: %v", err)
	}

	notification, err := h.VerifyNotification(context.Background(), string(raw), nil)
	if err != nil {
		t.Fatalf("verify notification: %v", err)
	}
	if notification.Status == payment.ProviderStatusSuccess {
		t.Fatalf("Status = %q, refund callback must not be treated as payment success", notification.Status)
	}
}

func TestHaozPayQueryOrderMapsSuccessfulStatusToPaid(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}

	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request: %v", err)
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"seqId":"HP123","orderNo":"sub2_haozpay_123","partyOrderId":"HP123","payStatus":2,"payAmount":10.00}}`))
	}))
	defer server.Close()

	h := &HaozPay{
		httpClient: &http.Client{Transport: rewriteHostTransport(server.URL)},
		merchantNo: "M123456",
		privateKey: key,
	}

	resp, err := h.QueryOrder(context.Background(), "sub2_haozpay_123")
	if err != nil {
		t.Fatalf("query order: %v", err)
	}
	if resp.Status != payment.ProviderStatusPaid {
		t.Fatalf("Status = %q, want paid", resp.Status)
	}
	if resp.TradeNo != "HP123" {
		t.Fatalf("TradeNo = %q, want HP123", resp.TradeNo)
	}

	bizBodyString, ok := captured["bizBody"].(string)
	if !ok {
		t.Fatalf("bizBody type = %T, want string", captured["bizBody"])
	}
	var bizBody map[string]interface{}
	bizDecoder := json.NewDecoder(strings.NewReader(bizBodyString))
	bizDecoder.UseNumber()
	if err := bizDecoder.Decode(&bizBody); err != nil {
		t.Fatalf("decode bizBody string: %v", err)
	}
	if bizBody["orderNo"] != "sub2_haozpay_123" {
		t.Fatalf("orderNo = %#v, want sub2_haozpay_123", bizBody["orderNo"])
	}
}

func TestHaozPayQueryOrderUsesOrderAmountWhenPayAmountIsNet(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"seqId":"HZHT202607042073412271199350784","merchantOrderNo":"20260704naCD4hyY","payStatus":2,"orderAmount":10.00,"payAmount":9.92}}`))
	}))
	defer server.Close()

	h := &HaozPay{
		httpClient: &http.Client{Transport: rewriteHostTransport(server.URL)},
		merchantNo: "M123456",
		privateKey: key,
	}

	resp, err := h.QueryOrder(context.Background(), "20260704naCD4hyY")
	if err != nil {
		t.Fatalf("query order: %v", err)
	}
	if resp.Amount != 10 {
		t.Fatalf("Amount = %v, want gross order amount 10", resp.Amount)
	}
}

func TestHaozPayQueryOrderReadsNestedHostedOrderResponse(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code":0,
			"message":"成功",
			"data":{
				"hostOrderInfo":{
					"orderNo":"HZHT202607042073415216105959424",
					"orderAmount":5,
					"orderStatus":2,
					"orderStatusDesc":"支付成功",
					"payAmount":5
				},
				"payInfo":{
					"orderNo":"HZHT202607042073415216105959424",
					"orderAmount":5,
					"payStatus":2,
					"payStatusDesc":"支付成功",
					"payAmount":5
				},
				"tradeConfirmRecord":{
					"payAmount":5
				}
			}
		}`))
	}))
	defer server.Close()

	h := &HaozPay{
		httpClient: &http.Client{Transport: rewriteHostTransport(server.URL)},
		merchantNo: "M123456",
		privateKey: key,
	}

	resp, err := h.QueryOrder(context.Background(), "HZHT202607042073415216105959424")
	if err != nil {
		t.Fatalf("query order: %v", err)
	}
	if resp.Status != payment.ProviderStatusPaid {
		t.Fatalf("Status = %q, want paid", resp.Status)
	}
	if resp.Amount != 5 {
		t.Fatalf("Amount = %v, want 5", resp.Amount)
	}
	if resp.TradeNo != "HZHT202607042073415216105959424" {
		t.Fatalf("TradeNo = %q, want upstream order no", resp.TradeNo)
	}
}

func haozPayTestSignString(reqBody map[string]interface{}) string {
	signParams := make(map[string]string)
	if bizBody, ok := reqBody["bizBody"].(map[string]interface{}); ok {
		for k, v := range bizBody {
			if v != nil {
				signParams[k] = fmt.Sprintf("%v", v)
			}
		}
	} else if bizBodyString, ok := reqBody["bizBody"].(string); ok {
		var bizBody map[string]interface{}
		decoder := json.NewDecoder(strings.NewReader(bizBodyString))
		decoder.UseNumber()
		if err := decoder.Decode(&bizBody); err == nil {
			for k, v := range bizBody {
				if v != nil {
					signParams[k] = fmt.Sprintf("%v", v)
				}
			}
		}
	}
	for k, v := range reqBody {
		if k == "bizBody" || k == "sign" || v == nil {
			continue
		}
		switch value := v.(type) {
		case int64:
			signParams[k] = strconv.FormatInt(value, 10)
		case float64:
			signParams[k] = strconv.FormatFloat(value, 'f', -1, 64)
		default:
			signParams[k] = fmt.Sprintf("%v", value)
		}
	}

	keys := make([]string, 0, len(signParams))
	for k := range signParams {
		if signParams[k] != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	var buf strings.Builder
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte('&')
		}
		buf.WriteString(k)
		buf.WriteByte('=')
		buf.WriteString(signParams[k])
	}
	return buf.String()
}

func haozPayTestPrivateEncryptSign(key *rsa.PrivateKey, params map[string]interface{}) (string, error) {
	signString := haozPayTestSignString(params)
	hashHex := fmt.Sprintf("%x", sha256.Sum256([]byte(signString)))
	encrypted, err := haozPayTestPrivateEncrypt(key, []byte(hashHex))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(encrypted), nil
}

func haozPayTestPrivateEncrypt(key *rsa.PrivateKey, message []byte) ([]byte, error) {
	k := key.Size()
	if len(message) > k-11 {
		return nil, fmt.Errorf("message too long")
	}
	em := make([]byte, k)
	em[0] = 0
	em[1] = 1
	for i := 2; i < k-len(message)-1; i++ {
		em[i] = 0xff
	}
	copy(em[k-len(message):], message)

	m := new(big.Int).SetBytes(em)
	c := new(big.Int).Exp(m, key.D, key.N)
	return leftPad(c.Bytes(), k), nil
}

func haozPayTestPublicDecrypt(pub *rsa.PublicKey, ciphertext []byte) ([]byte, error) {
	if len(ciphertext) != pub.Size() {
		return nil, fmt.Errorf("invalid ciphertext size")
	}
	c := new(big.Int).SetBytes(ciphertext)
	m := new(big.Int).Exp(c, big.NewInt(int64(pub.E)), pub.N)
	em := leftPad(m.Bytes(), pub.Size())
	if len(em) < 11 || em[0] != 0 || em[1] != 1 {
		return nil, fmt.Errorf("invalid block")
	}
	i := 2
	for i < len(em) && em[i] == 0xff {
		i++
	}
	if i < 10 || i >= len(em) || em[i] != 0 {
		return nil, fmt.Errorf("invalid padding")
	}
	return em[i+1:], nil
}

func leftPad(in []byte, size int) []byte {
	if len(in) >= size {
		return in
	}
	out := make([]byte, size)
	copy(out[size-len(in):], in)
	return out
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func rewriteHostTransport(rawTarget string) http.RoundTripper {
	target, err := url.Parse(rawTarget)
	if err != nil {
		panic(err)
	}
	return roundTripFunc(func(req *http.Request) (*http.Response, error) {
		cloned := req.Clone(req.Context())
		cloned.URL.Scheme = target.Scheme
		cloned.URL.Host = target.Host
		return http.DefaultTransport.RoundTrip(cloned)
	})
}
