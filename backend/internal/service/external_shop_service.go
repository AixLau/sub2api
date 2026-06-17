package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/google/uuid"
)

const (
	ninePlusProviderName   = "9plus"
	default9PlusShopToken  = "X7AR3C01"
	default9PlusCategoryID = 165
	default9PlusGoodsType  = "card"
	default9PlusPage       = 1
	default9PlusPageSize   = 20
	default9PlusChannelID  = 10
	default9PlusBaseURL    = "https://9.plus"
)

const (
	ninePlusProductCatalogCacheTTL     = 10 * time.Minute
	ninePlusProductEnrichmentMaxWorker = 5
)

var (
	ninePlusUSDQuotaPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(\d+)\s*(?:刀|usd|美元|美金|\$)`),
		regexp.MustCompile(`(?i)(\d+)\s*(?:额度|quota)`),
		regexp.MustCompile(`(?i)(?:额度|quota)[^\d]*(\d+)`),
	}
	ninePlusFirstNumberPattern = regexp.MustCompile(`(\d+)`)
)

const (
	ninePlusFulfillmentPollInterval = 5 * time.Second
	ninePlusFulfillmentMaxDuration  = 30 * time.Minute
)

type ExternalShopProduct struct {
	Provider           string   `json:"provider"`
	ProductID          string   `json:"product_id"`
	DisplayName        string   `json:"display_name"`
	Description        string   `json:"description"`
	Category           string   `json:"category,omitempty"`
	Currency           string   `json:"currency"`
	Price              float64  `json:"price"`
	Fee                float64  `json:"fee,omitempty"`
	PaymentAmount      float64  `json:"payment_amount,omitempty"`
	OriginalPrice      *float64 `json:"original_price,omitempty"`
	Quota              int      `json:"quota"`
	QuotaUnit          string   `json:"quota_unit"`
	Badge              string   `json:"badge,omitempty"`
	Enabled            bool     `json:"enabled"`
	StockCount         *int     `json:"stock_count,omitempty"`
	SortOrder          int      `json:"sort_order"`
	DeliveryNote       string   `json:"delivery_note,omitempty"`
	ExternalProductRef string   `json:"external_product_ref,omitempty"`
}

type ExternalShopCreateOrderRequest struct {
	UserID      int64
	ProductID   string
	Quantity    int
	Contact     string
	RedirectURL string
}

type ExternalShopPaymentIntent struct {
	UserID             int64
	ProductID          string
	ProductName        string
	ExternalProductRef string
	ShopToken          string
	Quantity           int
	Amount             float64
	Currency           string
	Contact            string
	RequestPayload     map[string]any
	ProviderSnapshot   map[string]any
}

type ninePlusEnvelope[T any] struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data T      `json:"data"`
}

type ninePlusGoodsListResponse struct {
	Total int                  `json:"total"`
	List  []ninePlusGoodsBrief `json:"list"`
}

type ninePlusGoodsBrief struct {
	GoodsKey    string  `json:"goods_key"`
	Name        string  `json:"name"`
	Price       float64 `json:"price"`
	Description string  `json:"description"`
	Link        string  `json:"link"`
	Category    struct {
		Name string `json:"name"`
	} `json:"category"`
	Extend struct {
		StockCount int `json:"stock_count"`
		LimitCount int `json:"limit_count"`
	} `json:"extend"`
}

type ninePlusGoodsInfoResponse struct {
	GoodsKey      string  `json:"goods_key"`
	Name          string  `json:"name"`
	Price         float64 `json:"price"`
	RealPrice     float64 `json:"real_price"`
	Description   string  `json:"description"`
	ContactFormat string  `json:"contact_format"`
	Extend        struct {
		SendOrder  int `json:"send_order"`
		LimitCount int `json:"limit_count"`
	} `json:"extend"`
}

type ninePlusGoodsPriceResponse struct {
	OriginalAmount float64 `json:"original_amount"`
	TotalAmount    float64 `json:"total_amount"`
	Fee            float64 `json:"fee"`
}

type ninePlusOrderInfoResponse struct {
	TradeNo     string  `json:"trade_no"`
	GoodsName   string  `json:"goods_name"`
	Quantity    int     `json:"quantity"`
	TotalAmount float64 `json:"total_amount"`
	Status      int     `json:"status"`
	CreateTime  int64   `json:"create_time"`
	SuccessTime *int64  `json:"success_time"`
	Sendout     int     `json:"sendout"`
	Contact     string  `json:"contact"`
	Response    struct {
		Cards          []string `json:"cards"`
		ExportCardsURL string   `json:"export_cards_url"`
	} `json:"response"`
}

type ExternalShopService struct {
	redeemService              *RedeemService
	httpClient                 *http.Client
	baseURL                    string
	shopToken                  string
	channelID                  int
	defaultContact             string
	fulfillmentPollInterval    time.Duration
	fulfillmentPollMaxDuration time.Duration
	productCatalogMu           sync.Mutex
	productCatalogCache        []ExternalShopProduct
	productCatalogCacheUntil   time.Time
}

func NewExternalShopService(_ *dbent.Client, redeemService *RedeemService) *ExternalShopService {
	return &ExternalShopService{
		redeemService:  redeemService,
		httpClient:     &http.Client{Timeout: 15 * time.Second},
		baseURL:        default9PlusBaseURL,
		shopToken:      default9PlusShopToken,
		channelID:      default9PlusChannelID,
		defaultContact: "",
	}
}

func (s *ExternalShopService) withProviderConfig(config map[string]string) *ExternalShopService {
	if s == nil {
		return nil
	}
	helper := &ExternalShopService{
		redeemService:              s.redeemService,
		httpClient:                 s.httpClient,
		baseURL:                    s.baseURL,
		shopToken:                  s.shopToken,
		channelID:                  s.channelID,
		defaultContact:             s.defaultContact,
		fulfillmentPollInterval:    s.fulfillmentPollInterval,
		fulfillmentPollMaxDuration: s.fulfillmentPollMaxDuration,
	}
	if value := strings.TrimSpace(config["apiBase"]); value != "" {
		helper.baseURL = strings.TrimRight(value, "/")
	}
	if value := strings.TrimSpace(config["shopToken"]); value != "" {
		helper.shopToken = value
	}
	if value := strings.TrimSpace(config["channelId"]); value != "" {
		if channelID, err := strconv.Atoi(value); err == nil && channelID > 0 {
			helper.channelID = channelID
		}
	}
	if value := strings.TrimSpace(config["defaultContact"]); value != "" {
		helper.defaultContact = value
	}
	return helper
}

func (s *ExternalShopService) Is9PlusEnabled() bool {
	return s != nil && strings.TrimSpace(s.baseURL) != "" && strings.TrimSpace(s.shopToken) != "" && s.channelID > 0
}

func (s *ExternalShopService) List9PlusProducts(ctx context.Context) ([]ExternalShopProduct, error) {
	s.productCatalogMu.Lock()
	defer s.productCatalogMu.Unlock()

	now := time.Now()
	if now.Before(s.productCatalogCacheUntil) && len(s.productCatalogCache) > 0 {
		return cloneExternalShopProducts(s.productCatalogCache), nil
	}

	products, err := s.list9PlusProductsUncached(ctx)
	if err != nil {
		return nil, err
	}
	s.productCatalogCache = cloneExternalShopProducts(products)
	s.productCatalogCacheUntil = now.Add(ninePlusProductCatalogCacheTTL)
	return cloneExternalShopProducts(products), nil
}

func (s *ExternalShopService) list9PlusProductsUncached(ctx context.Context) ([]ExternalShopProduct, error) {
	var resp ninePlusGoodsListResponse
	if err := s.post9PlusJSON(ctx, "/shopApi/Shop/goodsList", map[string]any{
		"token":       s.shopToken,
		"keywords":    "",
		"category_id": default9PlusCategoryID,
		"goods_type":  default9PlusGoodsType,
		"current":     default9PlusPage,
		"pageSize":    default9PlusPageSize,
	}, &resp); err != nil {
		return nil, err
	}

	products := make([]ExternalShopProduct, len(resp.List))
	errCh := make(chan error, len(resp.List))
	var wg sync.WaitGroup
	workers := ninePlusProductEnrichmentMaxWorker
	if len(resp.List) < workers {
		workers = len(resp.List)
	}
	if workers <= 0 {
		return []ExternalShopProduct{}, nil
	}
	sem := make(chan struct{}, workers)
	for idx, item := range resp.List {
		wg.Add(1)
		go func(idx int, item ninePlusGoodsBrief) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			}
			product, err := s.build9PlusProduct(ctx, idx, item)
			if err != nil {
				errCh <- err
				return
			}
			products[idx] = product
		}(idx, item)
	}
	wg.Wait()
	close(errCh)
	if err, ok := <-errCh; ok {
		return nil, err
	}

	sort.Slice(products, func(i, j int) bool {
		return products[i].SortOrder < products[j].SortOrder
	})
	return products, nil
}

func (s *ExternalShopService) build9PlusProduct(ctx context.Context, idx int, item ninePlusGoodsBrief) (ExternalShopProduct, error) {
	goodsInfo, err := s.fetch9PlusGoodsInfo(ctx, item.GoodsKey)
	if err != nil {
		return ExternalShopProduct{}, err
	}
	priceInfo, err := s.fetch9PlusGoodsPrice(ctx, item.GoodsKey, 1)
	if err != nil {
		return ExternalShopProduct{}, err
	}
	description := trimHTML(item.Description)
	if description == "" {
		description = trimHTML(goodsInfo.Description)
	}
	paymentAmount := priceInfo.TotalAmount
	if paymentAmount <= 0 {
		paymentAmount = item.Price
		if priceInfo.Fee > 0 {
			paymentAmount += priceInfo.Fee
		}
	}
	fee := priceInfo.Fee
	if fee <= 0 && paymentAmount > item.Price {
		fee = paymentAmount - item.Price
	}
	quota, quotaUnit := inferNinePlusQuota(item.Name, description)
	stockCount := item.Extend.StockCount
	product := ExternalShopProduct{
		Provider:           ninePlusProviderName,
		ProductID:          item.GoodsKey,
		DisplayName:        item.Name,
		Description:        description,
		Category:           strings.TrimSpace(item.Category.Name),
		Currency:           "CNY",
		Price:              item.Price,
		Fee:                fee,
		PaymentAmount:      paymentAmount,
		Quota:              quota,
		QuotaUnit:          quotaUnit,
		Enabled:            true,
		StockCount:         &stockCount,
		SortOrder:          idx + 1,
		DeliveryNote:       "支付成功后自动兑换到当前账户",
		ExternalProductRef: item.Link,
	}
	if item.Category.Name != "" {
		product.Badge = item.Category.Name
	}
	return product, nil
}

func inferNinePlusQuota(values ...string) (int, string) {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		for _, pattern := range ninePlusUSDQuotaPatterns {
			if match := pattern.FindStringSubmatch(value); len(match) > 1 {
				if quota, err := strconv.Atoi(match[1]); err == nil && quota > 0 {
					return quota, "USD"
				}
			}
		}
	}
	for _, value := range values {
		if match := ninePlusFirstNumberPattern.FindStringSubmatch(value); len(match) > 1 {
			if quota, err := strconv.Atoi(match[1]); err == nil && quota > 0 {
				return quota, "USD"
			}
		}
	}
	return 1, "code"
}

func cloneExternalShopProducts(products []ExternalShopProduct) []ExternalShopProduct {
	cloned := make([]ExternalShopProduct, len(products))
	copy(cloned, products)
	for i := range cloned {
		if cloned[i].OriginalPrice != nil {
			original := *cloned[i].OriginalPrice
			cloned[i].OriginalPrice = &original
		}
	}
	return cloned
}

func (s *ExternalShopService) cached9PlusProduct(productID string) (ExternalShopProduct, bool) {
	s.productCatalogMu.Lock()
	defer s.productCatalogMu.Unlock()

	if time.Now().After(s.productCatalogCacheUntil) {
		return ExternalShopProduct{}, false
	}
	for _, product := range s.productCatalogCache {
		if product.ProductID == productID {
			return cloneExternalShopProducts([]ExternalShopProduct{product})[0], true
		}
	}
	return ExternalShopProduct{}, false
}

func (s *ExternalShopService) Prepare9PlusPaymentIntent(ctx context.Context, req ExternalShopCreateOrderRequest) (*ExternalShopPaymentIntent, error) {
	if req.UserID <= 0 {
		return nil, infraerrors.BadRequest("NINEPLUS_USER_REQUIRED", "user_id is required")
	}
	req.ProductID = strings.TrimSpace(req.ProductID)
	if req.ProductID == "" {
		return nil, infraerrors.BadRequest("NINEPLUS_PRODUCT_REQUIRED", "product_id is required")
	}
	if req.Quantity <= 0 {
		req.Quantity = 1
	}

	if req.Quantity == 1 {
		if product, ok := s.cached9PlusProduct(req.ProductID); ok {
			return s.build9PlusPaymentIntentFromCatalogProduct(req, product), nil
		}
	}

	product, err := s.fetch9PlusGoodsInfo(ctx, req.ProductID)
	if err != nil {
		return nil, err
	}
	price, err := s.fetch9PlusGoodsPrice(ctx, req.ProductID, req.Quantity)
	if err != nil {
		return nil, err
	}

	contact := strings.TrimSpace(req.Contact)
	if contact == "" {
		contact = s.defaultContact
	}
	orderAmount := price.TotalAmount
	if orderAmount <= 0 {
		unitPrice := product.RealPrice
		if unitPrice <= 0 {
			unitPrice = product.Price
		}
		orderAmount = unitPrice * float64(req.Quantity)
	}

	return &ExternalShopPaymentIntent{
		UserID:             req.UserID,
		ProductID:          req.ProductID,
		ProductName:        product.Name,
		ExternalProductRef: fmt.Sprintf("%s/item/%s", s.baseURL, req.ProductID),
		ShopToken:          s.shopToken,
		Quantity:           req.Quantity,
		Amount:             orderAmount,
		Currency:           "CNY",
		Contact:            contact,
		RequestPayload:     s.build9PlusCreateOrderPayload(req.UserID, req.ProductID, req.Quantity, contact),
		ProviderSnapshot: map[string]any{
			"provider":   ninePlusProviderName,
			"shop_token": s.shopToken,
			"channel_id": s.channelID,
		},
	}, nil
}

func (s *ExternalShopService) build9PlusPaymentIntentFromCatalogProduct(req ExternalShopCreateOrderRequest, product ExternalShopProduct) *ExternalShopPaymentIntent {
	contact := strings.TrimSpace(req.Contact)
	if contact == "" {
		contact = s.defaultContact
	}
	orderAmount := product.PaymentAmount
	if orderAmount <= 0 {
		orderAmount = product.Price
	}
	return &ExternalShopPaymentIntent{
		UserID:             req.UserID,
		ProductID:          req.ProductID,
		ProductName:        product.DisplayName,
		ExternalProductRef: product.ExternalProductRef,
		ShopToken:          s.shopToken,
		Quantity:           req.Quantity,
		Amount:             orderAmount,
		Currency:           product.Currency,
		Contact:            contact,
		RequestPayload:     s.build9PlusCreateOrderPayload(req.UserID, req.ProductID, req.Quantity, contact),
		ProviderSnapshot: map[string]any{
			"provider":   ninePlusProviderName,
			"shop_token": s.shopToken,
			"channel_id": s.channelID,
		},
	}
}

func (s *ExternalShopService) fetch9PlusGoodsInfo(ctx context.Context, goodsKey string) (*ninePlusGoodsInfoResponse, error) {
	var resp ninePlusGoodsInfoResponse
	if err := s.post9PlusJSON(ctx, "/shopApi/Shop/goodsInfo", map[string]any{"goods_key": goodsKey}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (s *ExternalShopService) build9PlusCreateOrderPayload(_ int64, goodsKey string, quantity int, contact string) map[string]any {
	return map[string]any{
		"goods_key":        goodsKey,
		"quantity":         quantity,
		"coupon_code":      "",
		"channel_id":       s.channelID,
		"contact":          contact,
		"query_password":   contact,
		"select_cards_ids": []string{},
		"extend": map[string]any{
			"juuid": newNinePlusJUUID(),
		},
	}
}

func newNinePlusJUUID() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")
}

func (s *ExternalShopService) fetch9PlusGoodsPrice(ctx context.Context, goodsKey string, quantity int) (*ninePlusGoodsPriceResponse, error) {
	var resp ninePlusGoodsPriceResponse
	if err := s.post9PlusJSON(ctx, "/shopApi/Shop/getGoodsPrice", map[string]any{
		"goods_key":   goodsKey,
		"quantity":    quantity,
		"coupon_code": "",
		"channel_id":  s.channelID,
	}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (s *ExternalShopService) fetch9PlusOrderInfo(ctx context.Context, tradeNo string, queryPassword string) (*ninePlusOrderInfoResponse, error) {
	var resp ninePlusOrderInfoResponse
	payload := map[string]any{
		"trade_no": tradeNo,
		"dump":     1,
	}
	if queryPassword = strings.TrimSpace(queryPassword); queryPassword != "" {
		payload["query_password"] = queryPassword
	}
	if err := s.post9PlusJSON(ctx, "/shopApi/Order/info", payload, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (s *ExternalShopService) post9PlusJSON(ctx context.Context, path string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal 9plus payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build 9plus request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Referer", fmt.Sprintf("%s/shop/%s", s.baseURL, s.shopToken))
	req.Header.Set("User-Agent", "sub2api-nineplus/1.0")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return infraerrors.ServiceUnavailable("NINEPLUS_UPSTREAM_ERROR", "failed to call nineplus api").WithCause(err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return infraerrors.ServiceUnavailable("NINEPLUS_UPSTREAM_ERROR", "failed to read nineplus response").WithCause(err)
	}
	if resp.StatusCode >= 400 {
		return infraerrors.ServiceUnavailable("NINEPLUS_UPSTREAM_ERROR", "nineplus api is unavailable").WithMetadata(map[string]string{
			"status": strconv.Itoa(resp.StatusCode),
		})
	}

	var envelope ninePlusEnvelope[json.RawMessage]
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return infraerrors.ServiceUnavailable("NINEPLUS_UPSTREAM_ERROR", "failed to decode nineplus response").WithCause(err)
	}
	if envelope.Code != 1 {
		return infraerrors.ServiceUnavailable("NINEPLUS_UPSTREAM_ERROR", envelope.Msg)
	}
	if out == nil || len(envelope.Data) == 0 || bytes.Equal(envelope.Data, []byte("null")) {
		return nil
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return infraerrors.ServiceUnavailable("NINEPLUS_UPSTREAM_ERROR", "failed to decode nineplus data").WithCause(err)
	}
	return nil
}

func trimHTML(input string) string {
	replacer := strings.NewReplacer("<p>", "", "</p>", "\n", "<br>", "\n", "<br/>", "\n", "<br />", "\n", "&nbsp;", " ")
	out := replacer.Replace(input)
	return strings.TrimSpace(stripTags(out))
}

func stripTags(input string) string {
	var b strings.Builder
	inTag := false
	for _, r := range input {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
		default:
			if !inTag {
				_, _ = b.WriteRune(r)
			}
		}
	}
	return b.String()
}

func normalizeDeliveredCards(cards []string) []string {
	if len(cards) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(cards))
	seen := make(map[string]struct{}, len(cards))
	for _, card := range cards {
		card = strings.TrimSpace(card)
		if card == "" {
			continue
		}
		if _, ok := seen[card]; ok {
			continue
		}
		seen[card] = struct{}{}
		normalized = append(normalized, card)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}
