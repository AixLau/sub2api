//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestListNinePlusProductsReadsLocalProviderProducts(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	configSvc := &PaymentConfigService{entClient: client}

	_, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeNinePlus).
		SetName("9plus Alipay").
		SetConfig(`{
			"shopToken":"shop-token",
			"channelId":"10",
			"defaultContact":"ops@example.com",
			"products":"[{\"product_id\":\"np-20\",\"display_name\":\"支付宝充值 20\",\"price\":20,\"fee\":0.3,\"payment_amount\":19.8,\"quota\":20,\"quota_unit\":\"USD\",\"enabled\":true,\"sort_order\":2},{\"product_id\":\"np-10\",\"display_name\":\"支付宝充值 10\",\"price\":10,\"quota\":10,\"quota_unit\":\"USD\",\"sort_order\":1}]"
		}`).
		SetSupportedTypes("nineplus").
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{
		entClient:     client,
		configService: configSvc,
	}

	products, err := svc.ListNinePlusProducts(ctx)
	require.NoError(t, err)
	require.Len(t, products, 2)
	require.Equal(t, "np-10", products[0].ProductID)
	require.Equal(t, "9plus", products[0].Provider)
	require.True(t, products[0].Enabled)
	require.Equal(t, "CNY", products[0].Currency)
	require.Equal(t, "np-20", products[1].ProductID)
	require.Equal(t, 19.8, products[1].PaymentAmount)
	require.Equal(t, 0.3, products[1].Fee)
	require.Equal(t, 20, products[1].Quota)
	require.Equal(t, "USD", products[1].QuotaUnit)
}

func TestPrepareNinePlusCreateOrderUsesLocalProductConfig(t *testing.T) {
	svc := &PaymentService{}
	stockCount := 12
	sel := &payment.InstanceSelection{
		ProviderKey: payment.TypeNinePlus,
		Config: map[string]string{
			"shopToken":      "shop-token",
			"channelId":      "12",
			"defaultContact": "ops@example.com",
			"products":       `[{"product_id":"np-20","display_name":"支付宝充值 20","category":"API额度","price":20,"fee":0.4,"payment_amount":20.4,"quota":150,"quota_unit":"USD","enabled":true,"stock_count":12,"sort_order":1,"external_product_ref":"https://9.plus/item/np-20"}]`,
		},
	}

	intent, err := svc.prepareNinePlusCreateOrder(context.Background(), CreateOrderRequest{
		UserID:            42,
		PaymentType:       payment.TypeNinePlus,
		OrderType:         payment.OrderTypeBalance,
		ExternalProductID: "np-20",
		ExternalQuantity:  3,
	}, sel)
	require.NoError(t, err)
	require.Equal(t, int64(42), intent.UserID)
	require.Equal(t, "np-20", intent.ProductID)
	require.Equal(t, "支付宝充值 20", intent.ProductName)
	require.Equal(t, "https://9.plus/item/np-20", intent.ExternalProductRef)
	require.Equal(t, "shop-token", intent.ShopToken)
	require.Equal(t, 3, intent.Quantity)
	require.Equal(t, 20.4, intent.Amount)
	require.Equal(t, "CNY", intent.Currency)
	require.Equal(t, "ops@example.com", intent.Contact)
	require.Equal(t, "np-20", intent.RequestPayload["goods_key"])
	require.Equal(t, 3, intent.RequestPayload["quantity"])
	require.Equal(t, 12, intent.RequestPayload["channel_id"])
	require.Equal(t, "9plus", intent.ProviderSnapshot["provider"])
	require.Equal(t, "API额度", intent.ProviderSnapshot["category"])
	require.Equal(t, 20.0, intent.ProviderSnapshot["product_price"])
	require.Equal(t, 0.4, intent.ProviderSnapshot["fee"])
	require.Equal(t, 20.4, intent.ProviderSnapshot["payment_amount"])
	require.Equal(t, float64(150), intent.ProviderSnapshot["credited_amount"])
	require.Equal(t, "USD", intent.ProviderSnapshot["credited_unit"])
	products, err := parseNinePlusConfiguredProducts(sel.Config)
	require.NoError(t, err)
	require.Equal(t, &stockCount, products[0].StockCount)
}

func TestValidateNinePlusSubscriptionOrderUsesExternalProductWithoutPlan(t *testing.T) {
	svc := &PaymentService{}

	plan, err := svc.validateOrderInput(context.Background(), CreateOrderRequest{
		UserID:            42,
		PaymentType:       payment.TypeNinePlus,
		OrderType:         payment.OrderTypeSubscription,
		Amount:            49.9,
		ExternalProductID: "np-sub-monthly",
	}, &PaymentConfig{})

	require.NoError(t, err)
	require.Nil(t, plan)
}

func TestListNinePlusProductsFetchesAPICreditAndSubscriptionCategories(t *testing.T) {
	ctx := context.Background()
	requestedCategories := make([]int, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/shopApi/Shop/goodsList":
			var req struct {
				CategoryID int `json:"category_id"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			requestedCategories = append(requestedCategories, req.CategoryID)
			switch req.CategoryID {
			case 165:
				_, _ = w.Write([]byte(`{"code":1,"msg":"ok","data":{"total":1,"list":[{"goods_key":"q8kb9h","name":"35刀额度","price":5,"description":"<p>35 USD quota</p>","link":"https://9.plus/item/q8kb9h","category":{"name":"API额度"},"extend":{"stock_count":153,"limit_count":0}}]}}`))
			case 167:
				_, _ = w.Write([]byte(`{"code":1,"msg":"ok","data":{"total":1,"list":[{"goods_key":"sub-standard","name":"Pro 标准月包：49.9 元/月，包含 400 额度","price":49.9,"description":"Pro subscription","link":"https://9.plus/item/sub-standard","category":{"name":"套餐"},"extend":{"stock_count":9,"limit_count":0}}]}}`))
			default:
				_, _ = w.Write([]byte(`{"code":1,"msg":"ok","data":{"total":0,"list":[]}}`))
			}
		case "/shopApi/Shop/goodsInfo":
			var req struct {
				GoodsKey string `json:"goods_key"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			switch req.GoodsKey {
			case "q8kb9h":
				_, _ = w.Write([]byte(`{"code":1,"msg":"ok","data":{"goods_key":"q8kb9h","name":"35刀额度","price":5,"real_price":5,"description":"<p>35 USD quota</p>","contact_format":"","extend":{"send_order":1,"limit_count":0}}}`))
			case "sub-standard":
				_, _ = w.Write([]byte(`{"code":1,"msg":"ok","data":{"goods_key":"sub-standard","name":"Pro 标准月包：49.9 元/月，包含 400 额度","price":49.9,"real_price":49.9,"description":"Pro subscription","contact_format":"","extend":{"send_order":1,"limit_count":0}}}`))
			default:
				http.NotFound(w, r)
			}
		case "/shopApi/Shop/getGoodsPrice":
			var req struct {
				GoodsKey string `json:"goods_key"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			switch req.GoodsKey {
			case "q8kb9h":
				_, _ = w.Write([]byte(`{"code":1,"msg":"ok","data":{"original_amount":5,"total_amount":5.1,"fee":0.1}}`))
			case "sub-standard":
				_, _ = w.Write([]byte(`{"code":1,"msg":"ok","data":{"original_amount":49.9,"total_amount":50.9,"fee":1}}`))
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	helper := NewExternalShopService(nil, nil).withProviderConfig(map[string]string{
		"apiBase":   server.URL,
		"shopToken": "shop-token",
		"channelId": "10",
	})

	products, err := helper.List9PlusProducts(ctx)

	require.NoError(t, err)
	require.Equal(t, []int{165, 167}, requestedCategories)
	require.Len(t, products, 2)
	require.Equal(t, "q8kb9h", products[0].ProductID)
	require.Equal(t, "API额度", products[0].Category)
	require.Equal(t, "sub-standard", products[1].ProductID)
	require.Equal(t, "套餐", products[1].Category)
	require.Equal(t, 400, products[1].Quota)
}

func TestRefreshNinePlusProductSnapshotsWritesLatestCatalogToProviderConfig(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	configSvc := &PaymentConfigService{entClient: client}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/shopApi/Shop/goodsList":
			_, _ = w.Write([]byte(`{"code":1,"msg":"ok","data":{"total":1,"list":[{"goods_key":"q8kb9h","name":"35刀额度","price":5,"description":"<p>35 USD quota</p>","link":"https://9.plus/item/q8kb9h","category":{"name":"充值卡"},"extend":{"stock_count":79,"limit_count":0}}]}}`))
		case "/shopApi/Shop/goodsInfo":
			_, _ = w.Write([]byte(`{"code":1,"msg":"ok","data":{"goods_key":"q8kb9h","name":"35刀额度","price":5,"real_price":5,"description":"<p>35 USD quota</p>","contact_format":"","extend":{"send_order":1,"limit_count":0}}}`))
		case "/shopApi/Shop/getGoodsPrice":
			_, _ = w.Write([]byte(`{"code":1,"msg":"ok","data":{"original_amount":5,"total_amount":5.1,"fee":0.1}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	instance, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeNinePlus).
		SetName("9plus Alipay").
		SetConfig(`{
			"apiBase":"` + server.URL + `",
			"shopToken":"shop-token",
			"channelId":"10",
			"defaultContact":"ops@example.com",
			"products":"[{\"product_id\":\"stale\",\"display_name\":\"old\",\"price\":1,\"quota\":1,\"enabled\":true}]"
		}`).
		SetSupportedTypes("nineplus").
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{
		entClient:           client,
		configService:       configSvc,
		externalShopService: NewExternalShopService(client, nil),
	}

	refreshed, err := svc.RefreshNinePlusProductSnapshots(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, refreshed)

	updated, err := client.PaymentProviderInstance.Get(ctx, instance.ID)
	require.NoError(t, err)
	config, err := configSvc.decryptConfig(updated.Config)
	require.NoError(t, err)
	var rawProducts []map[string]any
	require.NoError(t, json.Unmarshal([]byte(config[ninePlusProductsConfigKey]), &rawProducts))
	require.Len(t, rawProducts, 1)
	require.Equal(t, "q8kb9h", rawProducts[0]["product_id"])
	require.Equal(t, 5.0, rawProducts[0]["price"])
	require.Equal(t, 0.1, rawProducts[0]["fee"])
	require.Equal(t, 5.1, rawProducts[0]["payment_amount"])
	_, hasOriginalPrice := rawProducts[0]["original_price"]
	require.False(t, hasOriginalPrice)
	require.Equal(t, float64(35), rawProducts[0]["quota"])
	require.Equal(t, "USD", rawProducts[0]["quota_unit"])
	require.Equal(t, "充值卡", rawProducts[0]["category"])
	require.Equal(t, float64(79), rawProducts[0]["stock_count"])
}

func TestPaymentOrderExpiryRunOnceRefreshesNinePlusProductSnapshots(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	configSvc := &PaymentConfigService{entClient: client}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/shopApi/Shop/goodsList":
			_, _ = w.Write([]byte(`{"code":1,"msg":"ok","data":{"total":1,"list":[{"goods_key":"np-1700","name":"1700刀额度","price":1700,"description":"1700 USD quota","link":"https://9.plus/item/np-1700","category":{"name":"套餐"},"extend":{"stock_count":8,"limit_count":0}}]}}`))
		case "/shopApi/Shop/goodsInfo":
			_, _ = w.Write([]byte(`{"code":1,"msg":"ok","data":{"goods_key":"np-1700","name":"1700刀额度","price":1700,"real_price":1700,"description":"1700 USD quota","contact_format":"","extend":{"send_order":1,"limit_count":0}}}`))
		case "/shopApi/Shop/getGoodsPrice":
			_, _ = w.Write([]byte(`{"code":1,"msg":"ok","data":{"original_amount":1700,"total_amount":1700,"fee":0}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	instance, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeNinePlus).
		SetName("9plus Alipay").
		SetConfig(`{
			"apiBase":"` + server.URL + `",
			"shopToken":"shop-token",
			"channelId":"10",
			"defaultContact":"ops@example.com",
			"products":"[{\"product_id\":\"np-1700\",\"display_name\":\"1700刀额度\",\"price\":1700,\"quota\":1600,\"enabled\":true}]"
		}`).
		SetSupportedTypes("nineplus").
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	paymentSvc := &PaymentService{
		entClient:           client,
		configService:       configSvc,
		externalShopService: NewExternalShopService(client, nil),
	}
	expirySvc := NewPaymentOrderExpiryService(paymentSvc, time.Minute)

	expirySvc.runOnce()

	updated, err := client.PaymentProviderInstance.Get(ctx, instance.ID)
	require.NoError(t, err)
	config, err := configSvc.decryptConfig(updated.Config)
	require.NoError(t, err)
	var rawProducts []map[string]any
	require.NoError(t, json.Unmarshal([]byte(config[ninePlusProductsConfigKey]), &rawProducts))
	require.Len(t, rawProducts, 1)
	require.Equal(t, "np-1700", rawProducts[0]["product_id"])
	require.Equal(t, float64(1700), rawProducts[0]["quota"])
}

func TestMergeNinePlusRefreshedProductsPreservesExistingSubscriptionProducts(t *testing.T) {
	config := map[string]string{
		"products": `[
			{"product_id":"q8kb9h","display_name":"old 35","category":"API额度","price":5,"quota":30,"enabled":false,"sort_order":2},
			{"product_id":"sub-standard","display_name":"Pro 标准月包：49.9 元/月，包含 400 额度","category":"套餐","price":49.9,"payment_amount":50.9,"quota":400,"quota_unit":"USD","enabled":true,"stock_count":9,"sort_order":1}
		]`,
	}
	stockCount := 153
	refreshed := []ExternalShopProduct{{
		Provider:      ninePlusProviderName,
		ProductID:     "q8kb9h",
		DisplayName:   "35刀额度",
		Category:      "API额度",
		Currency:      "CNY",
		Price:         5,
		PaymentAmount: 5.1,
		Quota:         35,
		QuotaUnit:     "USD",
		Enabled:       true,
		StockCount:    &stockCount,
		SortOrder:     1,
	}}

	products := mergeNinePlusRefreshedProducts(config, refreshed)

	require.Len(t, products, 2)
	require.Equal(t, "sub-standard", products[0].ProductID)
	require.Equal(t, "Pro 标准月包：49.9 元/月，包含 400 额度", products[0].DisplayName)
	require.Equal(t, "套餐", products[0].Category)
	require.True(t, products[0].Enabled)
	require.Equal(t, 400, products[0].Quota)
	require.Equal(t, "q8kb9h", products[1].ProductID)
	require.False(t, products[1].Enabled)
	require.Equal(t, 35, products[1].Quota)
}

func TestPrepareNinePlusCreateOrderRejectsUnavailableLocalProduct(t *testing.T) {
	svc := &PaymentService{}
	sel := &payment.InstanceSelection{
		ProviderKey: payment.TypeNinePlus,
		Config: map[string]string{
			"products": `[{"product_id":"np-disabled","display_name":"Disabled","price":10,"quota":10,"enabled":false}]`,
		},
	}

	_, err := svc.prepareNinePlusCreateOrder(context.Background(), CreateOrderRequest{
		UserID:            42,
		PaymentType:       payment.TypeNinePlus,
		OrderType:         payment.OrderTypeBalance,
		ExternalProductID: "np-disabled",
	}, sel)
	require.Error(t, err)
	require.Equal(t, "NINEPLUS_PRODUCT_NOT_AVAILABLE", infraerrors.Reason(err))
}

func TestPrepareNinePlusCreateOrderRejectsOutOfStockLocalProduct(t *testing.T) {
	svc := &PaymentService{}
	sel := &payment.InstanceSelection{
		ProviderKey: payment.TypeNinePlus,
		Config: map[string]string{
			"products": `[{"product_id":"np-sold-out","display_name":"Pro 畅用月包","price":99.9,"quota":830,"enabled":true,"stock_count":0}]`,
		},
	}

	_, err := svc.prepareNinePlusCreateOrder(context.Background(), CreateOrderRequest{
		UserID:            42,
		PaymentType:       payment.TypeNinePlus,
		OrderType:         payment.OrderTypeSubscription,
		Amount:            99.9,
		ExternalProductID: "np-sold-out",
	}, sel)

	require.Error(t, err)
	require.Equal(t, "NINEPLUS_PRODUCT_NOT_AVAILABLE", infraerrors.Reason(err))
}

func TestInferNinePlusQuotaFromSubscriptionPackageName(t *testing.T) {
	quota, unit := inferNinePlusQuota("Pro 标准月包：49.9 元/月，包含 400 额度")

	require.Equal(t, 400, quota)
	require.Equal(t, "USD", unit)
}
