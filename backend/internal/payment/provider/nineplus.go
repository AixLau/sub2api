package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
)

const (
	ninePlusHTTPTimeout        = 15 * time.Second
	defaultNinePlusBaseURL     = "https://9.plus"
	defaultNinePlusChannelID   = 10
	defaultNinePlusCurrency    = "CNY"
	maxNinePlusResponseSummary = 512
	maxNinePlusPaymentPageSize = 256 << 10
)

var ninePlusPaymentPageElapsedPattern = regexp.MustCompile(`maxtime\s*=\s*600\s*-\s*\(\s*parseInt\(\s*['"](\d+)['"]\s*\)\s*\)`)

type NinePlus struct {
	instanceID string
	config     map[string]string
	httpClient *http.Client
	baseURL    string
	shopToken  string
	channelID  int
}

type ninePlusProviderEnvelope[T any] struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

type ninePlusCreateOrderResponse struct {
	TradeNo     string  `json:"trade_no"`
	TotalAmount float64 `json:"total_amount"`
	PayURL      string  `json:"payurl"`
}

type ninePlusProviderOrderInfoResponse struct {
	TradeNo     string  `json:"trade_no"`
	TotalAmount float64 `json:"total_amount"`
	Status      int     `json:"status"`
	SuccessTime *int64  `json:"success_time"`
	Sendout     int     `json:"sendout"`
}

func NewNinePlus(instanceID string, config map[string]string) (*NinePlus, error) {
	cfg := cloneStringMap(config)
	baseURL := strings.TrimSpace(cfg["apiBase"])
	if baseURL == "" {
		baseURL = defaultNinePlusBaseURL
	}
	shopToken := strings.TrimSpace(cfg["shopToken"])
	if shopToken == "" {
		return nil, fmt.Errorf("nineplus config missing required key: shopToken")
	}
	if strings.TrimSpace(cfg["defaultContact"]) == "" {
		return nil, fmt.Errorf("nineplus config missing required key: defaultContact")
	}
	channelID := defaultNinePlusChannelID
	if raw := strings.TrimSpace(cfg["channelId"]); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			return nil, fmt.Errorf("nineplus config invalid channelId: %s", raw)
		}
		channelID = parsed
	}
	return &NinePlus{
		instanceID: instanceID,
		config:     cfg,
		httpClient: &http.Client{Timeout: ninePlusHTTPTimeout},
		baseURL:    strings.TrimRight(baseURL, "/"),
		shopToken:  shopToken,
		channelID:  channelID,
	}, nil
}

func (n *NinePlus) Name() string        { return "9.plus" }
func (n *NinePlus) ProviderKey() string { return payment.TypeNinePlus }
func (n *NinePlus) SupportedTypes() []payment.PaymentType {
	return []payment.PaymentType{payment.TypeNinePlus}
}

func (n *NinePlus) MerchantIdentityMetadata() map[string]string {
	return map[string]string{
		"shop_token": n.shopToken,
		"channel_id": strconv.Itoa(n.channelID),
	}
}

func (n *NinePlus) CreatePayment(ctx context.Context, req payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	productID := strings.TrimSpace(req.Metadata["external_product_id"])
	if productID == "" {
		productID = strings.TrimSpace(req.Subject)
	}
	if productID == "" {
		return nil, fmt.Errorf("nineplus create payment: missing product id")
	}
	quantity := 1
	rawQuantity := strings.TrimSpace(req.Metadata["external_quantity"])
	if rawQuantity == "" {
		rawQuantity = strings.TrimSpace(req.Amount)
	}
	if raw := rawQuantity; raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			quantity = parsed
		}
	}
	contact := strings.TrimSpace(req.Metadata["contact"])
	if contact == "" {
		contact = strings.TrimSpace(req.ClientIP)
	}
	if contact == "" {
		contact = n.config["defaultContact"]
	}
	payload := map[string]any{
		"goods_key":        productID,
		"quantity":         quantity,
		"coupon_code":      "",
		"channel_id":       n.channelID,
		"contact":          contact,
		"query_password":   contact,
		"select_cards_ids": []string{},
		"extend": map[string]any{
			"sub2api_order_id": req.OrderID,
		},
	}
	var resp ninePlusCreateOrderResponse
	if err := n.postJSON(ctx, "/shopApi/Pay/order", payload, &resp); err != nil {
		return nil, fmt.Errorf("nineplus create payment: %w", err)
	}
	payURL := n.absoluteURL(resp.PayURL)
	var expiresAt *time.Time
	if payURL != "" {
		expiresAt = n.paymentPageExpiresAt(ctx, payURL)
	}
	return &payment.CreatePaymentResponse{
		TradeNo:   resp.TradeNo,
		PayURL:    payURL,
		Currency:  defaultNinePlusCurrency,
		ExpiresAt: expiresAt,
	}, nil
}

func (n *NinePlus) QueryOrder(ctx context.Context, tradeNo string) (*payment.QueryOrderResponse, error) {
	tradeNo = strings.TrimSpace(tradeNo)
	paid, err := n.checkPaymentPageStatus(ctx, tradeNo)
	if err != nil {
		return nil, fmt.Errorf("nineplus query order status: %w", err)
	}
	if !paid {
		return &payment.QueryOrderResponse{
			TradeNo: tradeNo,
			Status:  payment.ProviderStatusPending,
			Metadata: map[string]string{
				"provider": "nineplus",
			},
		}, nil
	}

	payload := map[string]any{
		"trade_no": tradeNo,
		"dump":     1,
	}
	var resp ninePlusProviderOrderInfoResponse
	if err := n.postJSON(ctx, "/shopApi/Order/info", payload, &resp); err != nil {
		return nil, fmt.Errorf("nineplus query order: %w", err)
	}

	status := payment.ProviderStatusPending
	if resp.Status == 1 && resp.SuccessTime != nil && *resp.SuccessTime > 0 {
		status = payment.ProviderStatusPaid
	}
	if resp.Status < 0 {
		status = payment.ProviderStatusFailed
	}
	return &payment.QueryOrderResponse{
		TradeNo: resp.TradeNo,
		Status:  status,
		Amount:  resp.TotalAmount,
		Metadata: map[string]string{
			"provider": "nineplus",
			"sendout":  strconv.Itoa(resp.Sendout),
		},
	}, nil
}

func (n *NinePlus) checkPaymentPageStatus(ctx context.Context, tradeNo string) (bool, error) {
	var envelope ninePlusProviderEnvelope[json.RawMessage]
	if err := n.postFormEnvelope(ctx, "/payApi/common/checkOrderStatus.html", url.Values{"trade_no": []string{tradeNo}}, &envelope); err != nil {
		return false, err
	}
	return envelope.Code == 1, nil
}

func (n *NinePlus) VerifyNotification(context.Context, string, map[string]string) (*payment.PaymentNotification, error) {
	return nil, nil
}

func (n *NinePlus) Refund(context.Context, payment.RefundRequest) (*payment.RefundResponse, error) {
	return nil, fmt.Errorf("nineplus refund is not supported")
}

func (n *NinePlus) CancelPayment(context.Context, string) error {
	return nil
}

func (n *NinePlus) absoluteURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err == nil && parsed.Scheme != "" && parsed.Host != "" {
		return raw
	}
	base, err := url.Parse(n.baseURL)
	if err != nil {
		return raw
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return base.ResolveReference(ref).String()
}

func (n *NinePlus) paymentPageExpiresAt(ctx context.Context, payURL string) *time.Time {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, payURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Referer", fmt.Sprintf("%s/shop/%s", n.baseURL, n.shopToken))
	req.Header.Set("User-Agent", "sub2api-nineplus/1.0")
	resp, err := n.httpClient.Do(req)
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxNinePlusPaymentPageSize))
	if err != nil {
		return nil
	}
	remaining, ok := parseNinePlusPaymentPageRemainingSeconds(raw)
	if !ok || remaining <= 0 {
		return nil
	}
	expiresAt := time.Now().Add(time.Duration(remaining) * time.Second).UTC()
	return &expiresAt
}

func parseNinePlusPaymentPageRemainingSeconds(raw []byte) (int, bool) {
	match := ninePlusPaymentPageElapsedPattern.FindSubmatch(raw)
	if len(match) != 2 {
		return 0, false
	}
	elapsed, err := strconv.Atoi(string(match[1]))
	if err != nil {
		return 0, false
	}
	remaining := 600 - elapsed
	if remaining < 0 {
		remaining = 0
	}
	return remaining, true
}

func (n *NinePlus) postJSON(ctx context.Context, path string, payload map[string]any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Referer", fmt.Sprintf("%s/shop/%s", n.baseURL, n.shopToken))
	req.Header.Set("User-Agent", "sub2api-nineplus/1.0")
	resp, err := n.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("nineplus upstream error: status=%d body=%s", resp.StatusCode, summarizeNinePlusResponse(raw))
	}
	var envelope ninePlusProviderEnvelope[json.RawMessage]
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return err
	}
	if envelope.Code != 1 && envelope.Code != 200 {
		return fmt.Errorf("nineplus api error: code=%d msg=%s", envelope.Code, strings.TrimSpace(envelope.Msg))
	}
	if out == nil {
		return nil
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return nil
	}
	return json.Unmarshal(envelope.Data, out)
}

func (n *NinePlus) postFormEnvelope(ctx context.Context, path string, values url.Values, out any) error {
	body := values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.baseURL+path, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("Referer", fmt.Sprintf("%s/shop/%s", n.baseURL, n.shopToken))
	req.Header.Set("User-Agent", "sub2api-nineplus/1.0")
	resp, err := n.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("nineplus upstream error: status=%d body=%s", resp.StatusCode, summarizeNinePlusResponse(raw))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func summarizeNinePlusResponse(raw []byte) string {
	summary := strings.TrimSpace(string(raw))
	if len(summary) > maxNinePlusResponseSummary {
		return summary[:maxNinePlusResponseSummary] + "..."
	}
	return summary
}
