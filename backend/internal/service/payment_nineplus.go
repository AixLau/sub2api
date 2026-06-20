package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/ent/paymentproviderinstance"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const ninePlusProductsConfigKey = "products"

type ninePlusFulfillmentResult struct {
	CreditedAmount float64
	RedeemedCodes  []string
	DeliveredCodes []string
}

type ninePlusConfiguredProduct struct {
	Provider           string   `json:"provider"`
	ProductID          string   `json:"product_id"`
	DisplayName        string   `json:"display_name"`
	Description        string   `json:"description"`
	Category           string   `json:"category"`
	Currency           string   `json:"currency"`
	Price              float64  `json:"price"`
	Fee                float64  `json:"fee"`
	PaymentAmount      float64  `json:"payment_amount"`
	OriginalPrice      *float64 `json:"original_price"`
	Quota              int      `json:"quota"`
	QuotaUnit          string   `json:"quota_unit"`
	Badge              string   `json:"badge"`
	Enabled            *bool    `json:"enabled"`
	StockCount         *int     `json:"stock_count"`
	SortOrder          int      `json:"sort_order"`
	DeliveryNote       string   `json:"delivery_note"`
	ExternalProductRef string   `json:"external_product_ref"`
}

func (s *PaymentService) ExecuteNinePlusFulfillment(ctx context.Context, oid int64) error {
	o, err := s.entClient.PaymentOrder.Get(ctx, oid)
	if err != nil {
		return infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	if o.Status == OrderStatusCompleted {
		return nil
	}
	if psIsRefundStatus(o.Status) {
		return infraerrors.BadRequest("INVALID_STATUS", "refund-related order cannot fulfill")
	}
	if o.Status != OrderStatusPaid && o.Status != OrderStatusFailed && o.Status != OrderStatusRecharging {
		return infraerrors.BadRequest("INVALID_STATUS", "order cannot fulfill in status "+o.Status)
	}
	if o.Status != OrderStatusRecharging {
		c, err := s.entClient.PaymentOrder.Update().
			Where(paymentorder.IDEQ(oid), paymentorder.StatusIn(OrderStatusPaid, OrderStatusFailed)).
			SetStatus(OrderStatusRecharging).
			Save(ctx)
		if err != nil {
			return fmt.Errorf("lock nineplus order: %w", err)
		}
		if c == 0 {
			return nil
		}
	}
	if s.externalShopService == nil || s.redeemService == nil {
		err := infraerrors.ServiceUnavailable("NINEPLUS_NOT_CONFIGURED", "nineplus fulfillment is unavailable")
		s.markFailed(ctx, oid, err)
		return err
	}
	helper, err := s.ninePlusHelperForOrder(ctx, o)
	if err != nil {
		s.markFailed(ctx, oid, err)
		return err
	}
	result, err := helper.FulfillNinePlusPaymentOrder(ctx, o)
	if err != nil {
		if infraerrors.Reason(err) == "NINEPLUS_DELIVERY_PENDING" {
			return nil
		}
		s.markFailed(ctx, oid, err)
		return err
	}
	if o.OrderType != payment.OrderTypeSubscription && result != nil && result.CreditedAmount > 0 && result.CreditedAmount != o.Amount {
		updated, updateErr := s.entClient.PaymentOrder.UpdateOneID(o.ID).
			SetAmount(result.CreditedAmount).
			Save(ctx)
		if updateErr != nil {
			s.markFailed(ctx, oid, updateErr)
			return fmt.Errorf("update nineplus credited amount: %w", updateErr)
		}
		o = updated
	}
	if err := s.applyAffiliateRebateForOrder(ctx, o); err != nil {
		s.markFailed(ctx, oid, err)
		return err
	}
	auditAction := "RECHARGE_SUCCESS"
	if o.OrderType == payment.OrderTypeSubscription {
		auditAction = "SUBSCRIPTION_SUCCESS"
	}
	return s.markCompleted(ctx, o, auditAction)
}

func (s *ExternalShopService) FulfillNinePlusPaymentOrder(ctx context.Context, order *dbent.PaymentOrder) (*ninePlusFulfillmentResult, error) {
	if order == nil {
		return nil, infraerrors.NotFound("NOT_FOUND", "payment order not found")
	}
	if s == nil {
		return nil, infraerrors.ServiceUnavailable("NINEPLUS_NOT_CONFIGURED", "nineplus service is unavailable")
	}
	snapshot := psOrderProviderSnapshot(order)
	if snapshot == nil {
		return nil, infraerrors.BadRequest("NINEPLUS_ORDER_METADATA_MISSING", "nineplus order metadata is missing")
	}

	productID := strings.TrimSpace(snapshot.ExternalProductID)
	if productID == "" {
		return nil, infraerrors.BadRequest("NINEPLUS_PRODUCT_REQUIRED", "nineplus product id is required")
	}
	contact := strings.TrimSpace(snapshot.Contact)
	if contact == "" {
		contact = s.defaultContact
	}
	tradeNo := strings.TrimSpace(order.PaymentTradeNo)
	if tradeNo == "" {
		return nil, infraerrors.BadRequest("NINEPLUS_TRADE_NO_MISSING", "nineplus trade number is missing")
	}

	info, err := s.fetch9PlusOrderInfo(ctx, tradeNo, contact)
	if err != nil {
		return nil, err
	}
	if info != nil && info.Status < 0 {
		return nil, infraerrors.ServiceUnavailable("NINEPLUS_ORDER_FAILED", "nineplus order failed")
	}
	deliveredCodes := normalizeDeliveredCards(info.Response.Cards)
	if len(deliveredCodes) == 0 {
		return nil, infraerrors.ServiceUnavailable("NINEPLUS_DELIVERY_PENDING", "nineplus order has no delivered redeem codes yet")
	}

	redeemedCodes := make([]string, 0, len(deliveredCodes))
	creditedAmount := 0.0
	for _, code := range deliveredCodes {
		redeemCode, lookupErr := s.redeemService.GetByCode(ctx, code)
		if lookupErr == nil && redeemCode != nil && redeemCode.IsUsed() {
			if redeemCode.UsedBy == nil || *redeemCode.UsedBy != order.UserID {
				return nil, infraerrors.Conflict("NINEPLUS_CODE_ALREADY_USED", "nineplus redeem code was used by another account")
			}
			redeemedCodes = append(redeemedCodes, code)
			creditedAmount += redeemCode.Value
			continue
		}
		redeemedCode, err := s.redeemService.Redeem(ContextSkipRedeemAffiliate(ctx), order.UserID, code)
		if err != nil {
			return nil, fmt.Errorf("redeem nineplus code %s: %w", code, err)
		}
		redeemedCodes = append(redeemedCodes, code)
		if redeemedCode != nil {
			creditedAmount += redeemedCode.Value
		}
	}
	if creditedAmount <= 0 {
		creditedAmount = order.Amount
	}

	return &ninePlusFulfillmentResult{
		CreditedAmount: creditedAmount,
		RedeemedCodes:  redeemedCodes,
		DeliveredCodes: deliveredCodes,
	}, nil
}

func (s *PaymentService) prepareNinePlusCreateOrder(_ context.Context, req CreateOrderRequest, sel *payment.InstanceSelection) (*ExternalShopPaymentIntent, error) {
	if sel == nil {
		return nil, infraerrors.ServiceUnavailable("NINEPLUS_NOT_CONFIGURED", "nineplus provider is unavailable")
	}
	productID := strings.TrimSpace(req.ExternalProductID)
	if productID == "" {
		return nil, infraerrors.BadRequest("NINEPLUS_PRODUCT_REQUIRED", "nineplus product id is required")
	}
	products, err := parseNinePlusConfiguredProducts(sel.Config)
	if err != nil {
		return nil, err
	}
	product, ok := findNinePlusConfiguredProduct(products, productID)
	if !ok || !product.Enabled || (product.StockCount != nil && *product.StockCount <= 0) {
		return nil, infraerrors.BadRequest("NINEPLUS_PRODUCT_NOT_AVAILABLE", "nineplus product is not available")
	}
	quantity := req.ExternalQuantity
	if quantity <= 0 {
		quantity = 1
	}
	amount := product.PaymentAmount
	if amount <= 0 && product.Price > 0 {
		amount = product.Price + product.Fee
	}
	if amount <= 0 {
		amount = product.Price
	}
	if amount <= 0 {
		return nil, infraerrors.BadRequest("NINEPLUS_PRODUCT_NOT_AVAILABLE", "nineplus product amount is not configured")
	}
	contact := strings.TrimSpace(req.Contact)
	if contact == "" {
		contact = strings.TrimSpace(sel.Config["defaultContact"])
	}
	channelID := ninePlusProviderConfigChannelID(sel.Config)
	shopToken := strings.TrimSpace(sel.Config["shopToken"])
	productName := strings.TrimSpace(product.DisplayName)
	if productName == "" {
		productName = product.ProductID
	}
	externalProductRef := strings.TrimSpace(product.ExternalProductRef)
	if externalProductRef == "" {
		externalProductRef = strings.TrimRight(ninePlusProviderConfigBaseURL(sel.Config), "/") + "/item/" + product.ProductID
	}

	return &ExternalShopPaymentIntent{
		UserID:             req.UserID,
		ProductID:          product.ProductID,
		ProductName:        productName,
		ExternalProductRef: externalProductRef,
		ShopToken:          shopToken,
		Quantity:           quantity,
		Amount:             amount,
		Currency:           product.Currency,
		Contact:            contact,
		RequestPayload:     buildNinePlusCreateOrderPayloadFromConfig(req.UserID, product.ProductID, quantity, contact, channelID),
		ProviderSnapshot: map[string]any{
			"provider":        ninePlusProviderName,
			"shop_token":      shopToken,
			"channel_id":      channelID,
			"category":        product.Category,
			"product_price":   product.Price,
			"fee":             product.Fee,
			"payment_amount":  amount,
			"credited_amount": float64(product.Quota),
			"credited_unit":   product.QuotaUnit,
		},
	}, nil
}

func (s *PaymentService) RefreshNinePlusProductSnapshots(ctx context.Context) (int, error) {
	if s == nil || s.entClient == nil || s.configService == nil || s.externalShopService == nil {
		return 0, nil
	}
	instances, err := s.entClient.PaymentProviderInstance.Query().
		Where(
			paymentproviderinstance.EnabledEQ(true),
			paymentproviderinstance.ProviderKeyEQ(payment.TypeNinePlus),
		).
		Order(dbent.Asc(paymentproviderinstance.FieldSortOrder), dbent.Asc(paymentproviderinstance.FieldID)).
		All(ctx)
	if err != nil {
		return 0, fmt.Errorf("find nineplus providers: %w", err)
	}

	refreshed := 0
	for _, instance := range instances {
		config, err := s.configService.decryptConfig(instance.Config)
		if err != nil {
			return refreshed, fmt.Errorf("read nineplus provider config %d: %w", instance.ID, err)
		}
		if config == nil {
			config = map[string]string{}
		}
		helper := s.externalShopService.withProviderConfig(config)
		if helper == nil || !helper.Is9PlusEnabled() {
			continue
		}
		products, err := helper.List9PlusProducts(ctx)
		if err != nil {
			return refreshed, fmt.Errorf("refresh nineplus products for provider %d: %w", instance.ID, err)
		}
		products = mergeNinePlusRefreshedProducts(config, products)
		rawProducts, err := json.Marshal(products)
		if err != nil {
			return refreshed, fmt.Errorf("marshal nineplus products for provider %d: %w", instance.ID, err)
		}
		nextConfig := cloneStringConfig(config)
		nextConfig[ninePlusProductsConfigKey] = string(rawProducts)
		if strings.TrimSpace(config[ninePlusProductsConfigKey]) == nextConfig[ninePlusProductsConfigKey] {
			continue
		}
		encoded, err := s.configService.encryptConfig(nextConfig)
		if err != nil {
			return refreshed, fmt.Errorf("store nineplus products for provider %d: %w", instance.ID, err)
		}
		if _, err := s.entClient.PaymentProviderInstance.UpdateOneID(instance.ID).SetConfig(encoded).Save(ctx); err != nil {
			return refreshed, fmt.Errorf("update nineplus products for provider %d: %w", instance.ID, err)
		}
		refreshed++
	}
	return refreshed, nil
}

func mergeNinePlusRefreshedProducts(config map[string]string, refreshed []ExternalShopProduct) []ExternalShopProduct {
	existing, err := parseNinePlusConfiguredProducts(config)
	if err != nil || len(existing) == 0 {
		return refreshed
	}
	byID := make(map[string]ExternalShopProduct, len(existing))
	for _, product := range existing {
		byID[strings.TrimSpace(product.ProductID)] = product
	}
	refreshedIDs := make(map[string]struct{}, len(refreshed))
	for idx := range refreshed {
		current := &refreshed[idx]
		productID := strings.TrimSpace(current.ProductID)
		refreshedIDs[productID] = struct{}{}
		previous, ok := byID[productID]
		if !ok {
			continue
		}
		current.Enabled = previous.Enabled
		if previous.SortOrder > 0 {
			current.SortOrder = previous.SortOrder
		}
		if previous.DeliveryNote != "" {
			current.DeliveryNote = previous.DeliveryNote
		}
		if current.Quota <= 1 && previous.Quota > 0 {
			current.Quota = previous.Quota
			current.QuotaUnit = previous.QuotaUnit
		}
	}
	for _, product := range existing {
		productID := strings.TrimSpace(product.ProductID)
		if _, ok := refreshedIDs[productID]; ok {
			continue
		}
		if !isNinePlusSubscriptionConfiguredProduct(product) {
			continue
		}
		refreshed = append(refreshed, product)
	}
	sort.SliceStable(refreshed, func(i, j int) bool {
		return refreshed[i].SortOrder < refreshed[j].SortOrder
	})
	return refreshed
}

func isNinePlusSubscriptionConfiguredProduct(product ExternalShopProduct) bool {
	text := strings.ToLower(strings.Join([]string{
		product.Category,
		product.Badge,
		product.DisplayName,
		product.Description,
	}, " "))
	return strings.Contains(text, "套餐") ||
		strings.Contains(text, "月包") ||
		strings.Contains(text, "月卡") ||
		strings.Contains(text, "年包") ||
		strings.Contains(text, "年卡") ||
		strings.Contains(text, "会员") ||
		strings.Contains(text, "畅用") ||
		strings.Contains(text, "订阅") ||
		strings.Contains(text, "subscription") ||
		strings.Contains(text, "membership")
}

func cloneStringConfig(config map[string]string) map[string]string {
	cloned := make(map[string]string, len(config))
	for key, value := range config {
		cloned[key] = value
	}
	return cloned
}

func (s *PaymentService) ListNinePlusProducts(ctx context.Context) ([]ExternalShopProduct, error) {
	if s == nil || s.entClient == nil || s.configService == nil {
		return []ExternalShopProduct{}, nil
	}
	instance, err := s.entClient.PaymentProviderInstance.Query().
		Where(
			paymentproviderinstance.EnabledEQ(true),
			paymentproviderinstance.ProviderKeyEQ(payment.TypeNinePlus),
		).
		Order(dbent.Asc(paymentproviderinstance.FieldSortOrder), dbent.Asc(paymentproviderinstance.FieldID)).
		First(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return []ExternalShopProduct{}, nil
		}
		return nil, fmt.Errorf("find nineplus provider: %w", err)
	}
	config, err := s.configService.decryptConfig(instance.Config)
	if err != nil {
		return nil, fmt.Errorf("read nineplus provider config: %w", err)
	}
	return parseNinePlusConfiguredProducts(config)
}

func (s *PaymentService) ninePlusHelperForOrder(ctx context.Context, order *dbent.PaymentOrder) (*ExternalShopService, error) {
	snapshot := psOrderProviderSnapshot(order)
	instance, err := s.resolveSnapshotOrderProviderInstance(ctx, order, snapshot)
	if err != nil {
		return nil, err
	}
	if instance == nil || s.configService == nil {
		return nil, infraerrors.ServiceUnavailable("NINEPLUS_NOT_CONFIGURED", "nineplus provider configuration is unavailable")
	}
	config, err := s.configService.decryptConfig(instance.Config)
	if err != nil {
		return nil, fmt.Errorf("read nineplus provider config: %w", err)
	}
	return s.externalShopService.withProviderConfig(config), nil
}

func buildNinePlusProviderMetadata(productID string, quantity int, contact string) map[string]string {
	if quantity <= 0 {
		quantity = 1
	}
	return map[string]string{
		"external_product_id": strings.TrimSpace(productID),
		"external_quantity":   strconv.Itoa(quantity),
		"contact":             strings.TrimSpace(contact),
	}
}

func parseNinePlusConfiguredProducts(config map[string]string) ([]ExternalShopProduct, error) {
	raw := strings.TrimSpace(config[ninePlusProductsConfigKey])
	if raw == "" {
		return []ExternalShopProduct{}, nil
	}
	var configured []ninePlusConfiguredProduct
	if err := json.Unmarshal([]byte(raw), &configured); err != nil {
		return nil, infraerrors.BadRequest("NINEPLUS_PRODUCTS_INVALID", "nineplus products config is invalid").WithCause(err)
	}
	products := make([]ExternalShopProduct, 0, len(configured))
	for idx, item := range configured {
		productID := strings.TrimSpace(item.ProductID)
		if productID == "" {
			return nil, infraerrors.BadRequest("NINEPLUS_PRODUCTS_INVALID", "nineplus product_id is required")
		}
		enabled := true
		if item.Enabled != nil {
			enabled = *item.Enabled
		}
		providerName := strings.TrimSpace(item.Provider)
		if providerName == "" {
			providerName = ninePlusProviderName
		}
		displayName := strings.TrimSpace(item.DisplayName)
		if displayName == "" {
			displayName = productID
		}
		currency := strings.ToUpper(strings.TrimSpace(item.Currency))
		if currency == "" {
			currency = payment.DefaultPaymentCurrency
		}
		sortOrder := item.SortOrder
		if sortOrder == 0 {
			sortOrder = idx + 1
		}
		products = append(products, ExternalShopProduct{
			Provider:           providerName,
			ProductID:          productID,
			DisplayName:        displayName,
			Description:        strings.TrimSpace(item.Description),
			Category:           strings.TrimSpace(item.Category),
			Currency:           currency,
			Price:              item.Price,
			Fee:                item.Fee,
			PaymentAmount:      item.PaymentAmount,
			OriginalPrice:      item.OriginalPrice,
			Quota:              item.Quota,
			QuotaUnit:          strings.TrimSpace(item.QuotaUnit),
			Badge:              strings.TrimSpace(item.Badge),
			Enabled:            enabled,
			StockCount:         item.StockCount,
			SortOrder:          sortOrder,
			DeliveryNote:       strings.TrimSpace(item.DeliveryNote),
			ExternalProductRef: strings.TrimSpace(item.ExternalProductRef),
		})
	}
	sort.SliceStable(products, func(i, j int) bool {
		return products[i].SortOrder < products[j].SortOrder
	})
	return products, nil
}

func findNinePlusConfiguredProduct(products []ExternalShopProduct, productID string) (ExternalShopProduct, bool) {
	for _, product := range products {
		if strings.TrimSpace(product.ProductID) == productID {
			return product, true
		}
	}
	return ExternalShopProduct{}, false
}

func ninePlusProviderConfigChannelID(config map[string]string) int {
	channelID := default9PlusChannelID
	if value := strings.TrimSpace(config["channelId"]); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			channelID = parsed
		}
	}
	return channelID
}

func ninePlusProviderConfigBaseURL(config map[string]string) string {
	baseURL := default9PlusBaseURL
	if value := strings.TrimSpace(config["apiBase"]); value != "" {
		baseURL = strings.TrimRight(value, "/")
	}
	return baseURL
}

func buildNinePlusCreateOrderPayloadFromConfig(_ int64, productID string, quantity int, contact string, channelID int) map[string]any {
	return map[string]any{
		"goods_key":        productID,
		"quantity":         quantity,
		"coupon_code":      "",
		"channel_id":       channelID,
		"contact":          contact,
		"query_password":   contact,
		"select_cards_ids": []string{},
		"extend": map[string]any{
			"juuid": newNinePlusJUUID(),
		},
	}
}
