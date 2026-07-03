// Package provider contains concrete payment provider implementations.
package provider

import (
	"bytes"
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
	"math"
	"math/big"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
)

// HaozPay constants.
const (
	haozpayAPIBase         = "https://gate.haozpay.com"
	haozpayCreatePath      = "/pay-core/payment/order"
	haozpayQueryPath       = "/pay-core/payment/order/query"
	haozpayRefundPath      = "/pay-core/payment/refund"
	haozpayHTTPTimeout     = 30 * time.Second
	maxHaozpayResponseSize = 2 << 20 // 2MB

	haozpayCodeSuccess   = 0
	haozpayStatusSuccess = "SUCCESS"
	haozpayStatusPaid    = "PAID"
)

// HaozPay payment types (mapped from our internal types).
const (
	haozpayTypeAlipay = 0 // 支付宝正扫
	haozpayTypeWxpay  = 2 // 微信JSAPI支付
)

// HaozPay implements payment.Provider for 皓臻支付 (HaozPay) platform.
type HaozPay struct {
	instanceID     string
	config         map[string]string
	httpClient     *http.Client
	merchantNo     string
	privateKey     *rsa.PrivateKey
	platformPubKey *rsa.PublicKey
}

// NewHaozPay creates a new HaozPay provider.
// Required config keys: merchantNo, privateKey, platformPublicKey, notifyUrl
func NewHaozPay(instanceID string, config map[string]string) (*HaozPay, error) {
	// Validate required config
	required := []string{"merchantNo", "privateKey", "platformPublicKey", "notifyUrl"}
	for _, k := range required {
		if strings.TrimSpace(config[k]) == "" {
			return nil, fmt.Errorf("haozpay config missing required key: %s", k)
		}
	}

	// Parse merchant private key (PKCS8 format)
	privateKey, err := parseRSAPrivateKey(config["privateKey"])
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}

	// Parse platform public key
	platformPubKey, err := parseRSAPublicKey(config["platformPublicKey"])
	if err != nil {
		return nil, fmt.Errorf("parse platform public key: %w", err)
	}

	return &HaozPay{
		instanceID:     instanceID,
		config:         config,
		httpClient:     &http.Client{Timeout: haozpayHTTPTimeout},
		merchantNo:     config["merchantNo"],
		privateKey:     privateKey,
		platformPubKey: platformPubKey,
	}, nil
}

func (h *HaozPay) Name() string        { return "HaozPay" }
func (h *HaozPay) ProviderKey() string { return payment.TypeHaozPay }
func (h *HaozPay) SupportedTypes() []payment.PaymentType {
	return []payment.PaymentType{payment.TypeAlipay, payment.TypeWxpay}
}

func (h *HaozPay) MerchantIdentityMetadata() map[string]string {
	if h == nil {
		return nil
	}
	return map[string]string{"merchantNo": h.merchantNo}
}

// CreatePayment creates a payment order with HaozPay.
func (h *HaozPay) CreatePayment(ctx context.Context, req payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	// Map payment type
	payType, err := h.mapPaymentType(req.PaymentType)
	if err != nil {
		return nil, err
	}

	// Build business parameters
	bizBody := map[string]interface{}{
		"orderNo":           req.OrderID,
		"orderTitle":        req.Subject,
		"orderAmount":       json.Number(req.Amount),
		"payType":           payType,
		"useHaozPayCashier": true,
		"notifyUrl":         h.notifyURL(req.NotifyURL),
	}

	// Add redirect URL if provided
	if redirectURL := strings.TrimSpace(req.ReturnURL); redirectURL != "" {
		bizBody["redirectUrl"] = redirectURL
	} else if redirectURL := strings.TrimSpace(h.config["redirectUrl"]); redirectURL != "" {
		bizBody["redirectUrl"] = redirectURL
	}

	// Build request with signature
	reqBody, err := h.signedRequestBody(bizBody)
	if err != nil {
		return nil, fmt.Errorf("sign request: %w", err)
	}

	// Send request
	respBody, err := h.postJSON(ctx, haozpayAPIBase+haozpayCreatePath, reqBody)
	if err != nil {
		return nil, err
	}

	// Parse response
	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			MerchantNo      string  `json:"merchantNo"`
			SeqID           string  `json:"seqId"`
			PayInfo         string  `json:"payInfo"`
			MerchantOrderNo string  `json:"merchantOrderNo"`
			OrderAmount     float64 `json:"orderAmount"`
		} `json:"data"`
	}

	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if resp.Code != haozpayCodeSuccess {
		return nil, fmt.Errorf("haozpay error: %s (code: %d)", resp.Message, resp.Code)
	}

	return &payment.CreatePaymentResponse{
		TradeNo: resp.Data.SeqID,
		PayURL:  resp.Data.PayInfo,
	}, nil
}

// VerifyNotification verifies and parses HaozPay async notification.
func (h *HaozPay) VerifyNotification(_ context.Context, rawBody string, _ map[string]string) (*payment.PaymentNotification, error) {
	notify, err := decodeHaozPayJSONObject(rawBody)
	if err != nil {
		return nil, fmt.Errorf("parse notification: %w", err)
	}

	sign, _ := notify["sign"].(string)
	if strings.TrimSpace(sign) == "" {
		return nil, fmt.Errorf("missing sign")
	}
	merchantNo := haozpayStringValue(notify["merchantNo"])
	if h.merchantNo != "" && merchantNo != h.merchantNo {
		return nil, fmt.Errorf("merchantNo mismatch")
	}

	// Verify signature
	if err := h.verifyNotificationSignature(notify, sign); err != nil {
		return nil, fmt.Errorf("verify signature: %w", err)
	}

	// Extract business data
	bizParams, err := haozpayBusinessParams(notify)
	if err != nil {
		return nil, fmt.Errorf("parse business params: %w", err)
	}
	orderID := firstHaozPayString(bizParams, "orderNo", "merchantOrderNo")
	tradeNo := firstHaozPayString(bizParams, "seqId", "paySeqId", "orderNo", "merchantOrderNo")
	amount := firstHaozPayFloat(bizParams, "payAmount", "orderAmount", "ordAmt")

	// Map status
	providerStatus := payment.ProviderStatusFailed
	if haozpayPaymentSucceeded(bizParams) {
		providerStatus = payment.ProviderStatusSuccess
	}

	return &payment.PaymentNotification{
		TradeNo:  tradeNo,
		OrderID:  orderID,
		Amount:   amount,
		Status:   providerStatus,
		RawData:  rawBody,
		Metadata: h.MerchantIdentityMetadata(),
	}, nil
}

// QueryOrder queries payment status from HaozPay.
func (h *HaozPay) QueryOrder(ctx context.Context, tradeNo string) (*payment.QueryOrderResponse, error) {
	// Build request
	bizBody := map[string]interface{}{
		"seqId": tradeNo,
	}

	reqBody, err := h.signedRequestBody(bizBody)
	if err != nil {
		return nil, fmt.Errorf("sign request: %w", err)
	}

	// Send request
	respBody, err := h.postJSON(ctx, haozpayAPIBase+haozpayQueryPath, reqBody)
	if err != nil {
		return nil, err
	}

	// Parse response
	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Status      string  `json:"status"`
			SeqID       string  `json:"seqId"`
			OrderAmount float64 `json:"orderAmount"`
		} `json:"data"`
	}

	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if resp.Code != haozpayCodeSuccess {
		return nil, fmt.Errorf("haozpay query error: %s (code: %d)", resp.Message, resp.Code)
	}

	// Map status
	status := payment.ProviderStatusPending
	if resp.Data.Status == haozpayStatusSuccess || resp.Data.Status == haozpayStatusPaid {
		status = payment.ProviderStatusPaid
	}

	return &payment.QueryOrderResponse{
		TradeNo:  resp.Data.SeqID,
		Status:   status,
		Amount:   resp.Data.OrderAmount,
		Metadata: h.MerchantIdentityMetadata(),
	}, nil
}

// Refund initiates a refund with HaozPay.
func (h *HaozPay) Refund(ctx context.Context, req payment.RefundRequest) (*payment.RefundResponse, error) {
	// Build business parameters
	bizBody := map[string]interface{}{
		"orderNo":      firstNonEmpty(req.OrderID, req.TradeNo),
		"refundAmount": json.Number(req.Amount),
		"refundReason": req.Reason,
	}

	reqBody, err := h.signedRequestBody(bizBody)
	if err != nil {
		return nil, fmt.Errorf("sign request: %w", err)
	}

	// Send request
	respBody, err := h.postJSON(ctx, haozpayAPIBase+haozpayRefundPath, reqBody)
	if err != nil {
		return nil, err
	}

	// Parse response
	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			RefundID     string `json:"seqId"`
			RefundStatus int    `json:"refundStatus"`
		} `json:"data"`
	}

	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if resp.Code != haozpayCodeSuccess {
		return nil, fmt.Errorf("haozpay refund error: %s (code: %d)", resp.Message, resp.Code)
	}

	return &payment.RefundResponse{
		RefundID: resp.Data.RefundID,
		Status:   haozpayRefundProviderStatus(resp.Data.RefundStatus),
	}, nil
}

func (h *HaozPay) signedRequestBody(bizBody map[string]interface{}) (map[string]interface{}, error) {
	bizBodyJSON, err := marshalHaozPayBizBody(bizBody)
	if err != nil {
		return nil, err
	}
	reqBody := map[string]interface{}{
		"merchantNo": h.merchantNo,
		"timestamp":  time.Now().UnixMilli(),
		"bizBody":    bizBodyJSON,
	}
	sign, err := h.signRequest(reqBody)
	if err != nil {
		return nil, err
	}
	reqBody["sign"] = sign
	return reqBody, nil
}

// signRequest signs a HaozPay API request following their specification:
// 1. Extract and flatten bizBody parameters
// 2. Sort all parameters by key (dictionary order)
// 3. Build sign string: key1=value1&key2=value2...
// 4. SHA256 hash the sign string to a 64-byte lowercase hex string
// 5. RSA private-key encrypt/sign the hex string bytes with PKCS#1 v1.5
// 6. Base64 encode
func (h *HaozPay) signRequest(reqBody map[string]interface{}) (string, error) {
	sha256Hex, err := haozpaySHA256HexForPayload(reqBody)
	if err != nil {
		return "", err
	}
	encrypted, err := rsa.SignPKCS1v15(rand.Reader, h.privateKey, 0, []byte(sha256Hex))
	if err != nil {
		return "", fmt.Errorf("rsa sign: %w", err)
	}
	return base64.StdEncoding.EncodeToString(encrypted), nil
}

// verifyNotificationSignature verifies HaozPay notification signature using platform public key.
func (h *HaozPay) verifyNotificationSignature(payload map[string]interface{}, sign string) error {
	sha256Hex, err := haozpaySHA256HexForPayload(payload)
	if err != nil {
		return err
	}
	signature, err := base64.StdEncoding.DecodeString(sign)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	decrypted, err := haozpayPublicDecryptPKCS1v15(h.platformPubKey, signature)
	if err != nil {
		return fmt.Errorf("verify failed: %w", err)
	}
	if string(decrypted) != sha256Hex {
		return fmt.Errorf("signature digest mismatch")
	}
	return nil
}

// mapPaymentType maps our internal payment type to HaozPay's payType enum.
func (h *HaozPay) mapPaymentType(paymentType string) (int, error) {
	switch paymentType {
	case payment.TypeAlipay:
		return haozpayTypeAlipay, nil
	case payment.TypeWxpay:
		return haozpayTypeWxpay, nil
	default:
		return 0, fmt.Errorf("unsupported payment type: %s", paymentType)
	}
}

func (h *HaozPay) notifyURL(override string) string {
	if notifyURL := strings.TrimSpace(override); notifyURL != "" {
		return notifyURL
	}
	return strings.TrimSpace(h.config["notifyUrl"])
}

func marshalHaozPayBizBody(bizBody map[string]interface{}) (string, error) {
	data, err := json.Marshal(bizBody)
	if err != nil {
		return "", fmt.Errorf("marshal bizBody: %w", err)
	}
	return string(data), nil
}

func haozpaySHA256HexForPayload(payload map[string]interface{}) (string, error) {
	signString, err := haozpaySignString(payload)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256([]byte(signString))
	return fmt.Sprintf("%x", hash), nil
}

func haozpaySignString(payload map[string]interface{}) (string, error) {
	signParams, err := haozpaySignParams(payload)
	if err != nil {
		return "", err
	}
	keys := make([]string, 0, len(signParams))
	for k, v := range signParams {
		if strings.TrimSpace(v) != "" {
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
	return buf.String(), nil
}

func haozpaySignParams(payload map[string]interface{}) (map[string]string, error) {
	params := make(map[string]string)
	for k, v := range payload {
		if k == "sign" || v == nil {
			continue
		}
		if k == "bizBody" {
			bizParams, err := haozpayBusinessParams(payload)
			if err != nil {
				return nil, err
			}
			for bk, bv := range bizParams {
				if bv != nil {
					params[bk] = haozpayStringValue(bv)
				}
			}
			continue
		}
		params[k] = haozpayStringValue(v)
	}
	return params, nil
}

func haozpayBusinessParams(payload map[string]interface{}) (map[string]interface{}, error) {
	bizBody, ok := payload["bizBody"]
	if !ok || bizBody == nil {
		params := make(map[string]interface{}, len(payload))
		for k, v := range payload {
			if k == "sign" || v == nil {
				continue
			}
			params[k] = v
		}
		return params, nil
	}
	switch body := bizBody.(type) {
	case map[string]interface{}:
		return body, nil
	case string:
		return decodeHaozPayJSONObject(body)
	default:
		return nil, fmt.Errorf("unsupported bizBody type %T", bizBody)
	}
}

func decodeHaozPayJSONObject(raw string) (map[string]interface{}, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var payload map[string]interface{}
	if err := decoder.Decode(&payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func haozpayStringValue(v interface{}) string {
	switch value := v.(type) {
	case nil:
		return ""
	case string:
		return value
	case json.Number:
		return value.String()
	case int:
		return strconv.Itoa(value)
	case int64:
		return strconv.FormatInt(value, 10)
	case int32:
		return strconv.FormatInt(int64(value), 10)
	case uint:
		return strconv.FormatUint(uint64(value), 10)
	case uint64:
		return strconv.FormatUint(value, 10)
	case float64:
		if math.Trunc(value) == value {
			return strconv.FormatInt(int64(value), 10)
		}
		return strconv.FormatFloat(value, 'f', -1, 64)
	case float32:
		f := float64(value)
		if math.Trunc(f) == f {
			return strconv.FormatInt(int64(f), 10)
		}
		return strconv.FormatFloat(f, 'f', -1, 32)
	case bool:
		return strconv.FormatBool(value)
	default:
		return fmt.Sprintf("%v", value)
	}
}

func firstHaozPayString(params map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(haozpayStringValue(params[key])); value != "" {
			return value
		}
	}
	return ""
}

func firstHaozPayFloat(params map[string]interface{}, keys ...string) float64 {
	for _, key := range keys {
		if value := strings.TrimSpace(haozpayStringValue(params[key])); value != "" {
			parsed, err := strconv.ParseFloat(value, 64)
			if err == nil {
				return parsed
			}
		}
	}
	return 0
}

func haozpayPaymentSucceeded(params map[string]interface{}) bool {
	statusText := strings.ToUpper(firstHaozPayString(params, "status", "tradeStatus"))
	if statusText == haozpayStatusSuccess || statusText == haozpayStatusPaid {
		return true
	}
	statusCode := firstHaozPayString(params, "payStatus")
	return statusCode == "2"
}

func haozpayRefundProviderStatus(status int) string {
	switch status {
	case 2:
		return payment.ProviderStatusSuccess
	case 3:
		return payment.ProviderStatusFailed
	default:
		return payment.ProviderStatusPending
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func haozpayPublicDecryptPKCS1v15(pub *rsa.PublicKey, ciphertext []byte) ([]byte, error) {
	if pub == nil {
		return nil, fmt.Errorf("missing public key")
	}
	if len(ciphertext) != pub.Size() {
		return nil, fmt.Errorf("invalid ciphertext size")
	}
	c := new(big.Int).SetBytes(ciphertext)
	m := new(big.Int).Exp(c, big.NewInt(int64(pub.E)), pub.N)
	em := leftPadBytes(m.Bytes(), pub.Size())
	if len(em) < 11 || em[0] != 0 || em[1] != 1 {
		return nil, fmt.Errorf("invalid rsa block")
	}
	i := 2
	for i < len(em) && em[i] == 0xff {
		i++
	}
	if i < 10 || i >= len(em) || em[i] != 0 {
		return nil, fmt.Errorf("invalid rsa padding")
	}
	return em[i+1:], nil
}

func leftPadBytes(in []byte, size int) []byte {
	if len(in) >= size {
		return in
	}
	out := make([]byte, size)
	copy(out[size-len(in):], in)
	return out
}

// postJSON sends a JSON request and returns response body.
func (h *HaozPay) postJSON(ctx context.Context, url string, data interface{}) ([]byte, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxHaozpayResponseSize))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// decodePEMOrBase64DER accepts either a full PEM block or a raw base64 DER key
// copied from a payment console.
func decodePEMOrBase64DER(keyText string, defaultPEMType string) (*pem.Block, error) {
	trimmed := strings.TrimSpace(keyText)
	block, _ := pem.Decode([]byte(trimmed))
	if block == nil {
		raw := strings.Join(strings.Fields(trimmed), "")
		der, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid PEM block")
		}
		block = &pem.Block{Type: defaultPEMType, Bytes: der}
	}
	return block, nil
}

// parseRSAPrivateKey parses PKCS8/PKCS1 PEM-encoded private keys and raw
// base64 DER private keys.
func parseRSAPrivateKey(keyPEM string) (*rsa.PrivateKey, error) {
	block, err := decodePEMOrBase64DER(keyPEM, "PRIVATE KEY")
	if err != nil {
		return nil, err
	}

	// Try PKCS8 first
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err == nil {
		if rsaKey, ok := key.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
		return nil, fmt.Errorf("not an RSA private key")
	}

	// Fallback to PKCS1
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

// parseRSAPublicKey parses PEM-encoded public keys and raw base64 DER public keys.
func parseRSAPublicKey(keyPEM string) (*rsa.PublicKey, error) {
	block, err := decodePEMOrBase64DER(keyPEM, "PUBLIC KEY")
	if err != nil {
		return nil, err
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not an RSA public key")
	}

	return rsaPub, nil
}
