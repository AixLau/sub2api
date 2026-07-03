// Package provider contains concrete payment provider implementations.
package provider

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
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
	haozpayRefundPath      = "/pay-core/refund/apply"
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
		"orderTitle":        req.Subject,
		"orderAmount":       req.Amount,
		"payType":           payType,
		"useHaozPayCashier": true,
		"notifyUrl":         h.config["notifyUrl"],
	}

	// Add redirect URL if provided
	if redirectURL := strings.TrimSpace(h.config["redirectUrl"]); redirectURL != "" {
		bizBody["redirectUrl"] = redirectURL
	}

	// Build request with signature
	timestamp := time.Now().UnixMilli()
	reqBody := map[string]interface{}{
		"merchantNo": h.merchantNo,
		"timestamp":  timestamp,
		"bizBody":    bizBody,
	}

	// Sign request
	sign, err := h.signRequest(reqBody)
	if err != nil {
		return nil, fmt.Errorf("sign request: %w", err)
	}
	reqBody["sign"] = sign

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
func (h *HaozPay) VerifyNotification(ctx context.Context, rawBody string, headers map[string]string) (*payment.PaymentNotification, error) {
	var notify struct {
		MerchantNo string                 `json:"merchantNo"`
		Timestamp  int64                  `json:"timestamp"`
		BizBody    map[string]interface{} `json:"bizBody"`
		Sign       string                 `json:"sign"`
	}

	if err := json.Unmarshal([]byte(rawBody), &notify); err != nil {
		return nil, fmt.Errorf("parse notification: %w", err)
	}

	// Verify signature
	if err := h.verifyNotificationSignature(notify.MerchantNo, notify.Timestamp, notify.BizBody, notify.Sign); err != nil {
		return nil, fmt.Errorf("verify signature: %w", err)
	}

	// Extract business data
	tradeNo, _ := notify.BizBody["seqId"].(string)
	orderID, _ := notify.BizBody["merchantOrderNo"].(string)
	status, _ := notify.BizBody["status"].(string)
	amount := 0.0
	if amt, ok := notify.BizBody["orderAmount"].(float64); ok {
		amount = amt
	} else if amtStr, ok := notify.BizBody["orderAmount"].(string); ok {
		amount, _ = strconv.ParseFloat(amtStr, 64)
	}

	// Map status
	providerStatus := payment.ProviderStatusFailed
	if status == haozpayStatusSuccess || status == haozpayStatusPaid {
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

	timestamp := time.Now().UnixMilli()
	reqBody := map[string]interface{}{
		"merchantNo": h.merchantNo,
		"timestamp":  timestamp,
		"bizBody":    bizBody,
	}

	// Sign request
	sign, err := h.signRequest(reqBody)
	if err != nil {
		return nil, fmt.Errorf("sign request: %w", err)
	}
	reqBody["sign"] = sign

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
		status = payment.ProviderStatusSuccess
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
		"seqId":        req.TradeNo,
		"refundAmount": req.Amount,
		"refundReason": req.Reason,
	}

	timestamp := time.Now().UnixMilli()
	reqBody := map[string]interface{}{
		"merchantNo": h.merchantNo,
		"timestamp":  timestamp,
		"bizBody":    bizBody,
	}

	// Sign request
	sign, err := h.signRequest(reqBody)
	if err != nil {
		return nil, fmt.Errorf("sign request: %w", err)
	}
	reqBody["sign"] = sign

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
			RefundID string `json:"refundId"`
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
		Status:   payment.ProviderStatusSuccess,
	}, nil
}

// signRequest signs a HaozPay API request following their specification:
// 1. Extract and flatten bizBody parameters
// 2. Sort all parameters by key (dictionary order)
// 3. Build sign string: key1=value1&key2=value2...
// 4. SHA256 hash the sign string
// 5. RSA encrypt with merchant private key
// 6. Base64 encode
func (h *HaozPay) signRequest(reqBody map[string]interface{}) (string, error) {
	// Step 1: Flatten bizBody
	signParams := make(map[string]string)

	// Extract bizBody if present
	if bizBody, ok := reqBody["bizBody"].(map[string]interface{}); ok {
		for k, v := range bizBody {
			if v != nil {
				signParams[k] = fmt.Sprintf("%v", v)
			}
		}
	}

	// Add common parameters
	if merchantNo, ok := reqBody["merchantNo"].(string); ok {
		signParams["merchantNo"] = merchantNo
	}
	if timestamp, ok := reqBody["timestamp"].(int64); ok {
		signParams["timestamp"] = strconv.FormatInt(timestamp, 10)
	}

	// Step 2 & 3: Sort and build sign string
	keys := make([]string, 0, len(signParams))
	for k := range signParams {
		if signParams[k] != "" { // Skip empty values
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
	signString := buf.String()

	// Step 4: SHA256 hash
	hash := sha256.Sum256([]byte(signString))

	// Step 5: RSA encrypt with private key
	encrypted, err := rsa.SignPKCS1v15(rand.Reader, h.privateKey, crypto.SHA256, hash[:])
	if err != nil {
		return "", fmt.Errorf("rsa sign: %w", err)
	}

	// Step 6: Base64 encode
	return base64.StdEncoding.EncodeToString(encrypted), nil
}

// verifyNotificationSignature verifies HaozPay notification signature using platform public key.
func (h *HaozPay) verifyNotificationSignature(merchantNo string, timestamp int64, bizBody map[string]interface{}, sign string) error {
	// Flatten parameters for verification
	signParams := make(map[string]string)

	for k, v := range bizBody {
		if v != nil {
			signParams[k] = fmt.Sprintf("%v", v)
		}
	}
	signParams["merchantNo"] = merchantNo
	signParams["timestamp"] = strconv.FormatInt(timestamp, 10)

	// Build sign string
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
	signString := buf.String()

	// SHA256 hash
	hash := sha256.Sum256([]byte(signString))

	// Decode signature
	signature, err := base64.StdEncoding.DecodeString(sign)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}

	// Verify with platform public key
	err = rsa.VerifyPKCS1v15(h.platformPubKey, crypto.SHA256, hash[:], signature)
	if err != nil {
		return fmt.Errorf("verify failed: %w", err)
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
