package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
)

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
	return &payment.CreatePaymentResponse{
		TradeNo:  resp.TradeNo,
		PayURL:   n.absoluteURL(resp.PayURL),
		Currency: defaultNinePlusCurrency,
	}, nil
}

func (n *NinePlus) QueryOrder(ctx context.Context, tradeNo string) (*payment.QueryOrderResponse, error) {
	payload := map[string]any{
		"trade_no": strings.TrimSpace(tradeNo),
		"dump":     1,
	}
	if queryPassword := strings.TrimSpace(n.config["defaultContact"]); queryPassword != "" {
		payload["query_password"] = queryPassword
	}
	var resp ninePlusProviderOrderInfoResponse
	if err := n.postJSON(ctx, "/shopApi/Order/info", payload, &resp); err != nil {
		return nil, fmt.Errorf("nineplus query order: %w", err)
	}

	status := payment.ProviderStatusPending
	if resp.SuccessTime != nil && *resp.SuccessTime > 0 {
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

func summarizeNinePlusResponse(raw []byte) string {
	summary := strings.TrimSpace(string(raw))
	if len(summary) > maxNinePlusResponseSummary {
		return summary[:maxNinePlusResponseSummary] + "..."
	}
	return summary
}
