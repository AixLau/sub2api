//go:build unit

package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

func TestGetCheckoutInfoOmitsDeprecatedNinePlusFlag(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	client := newPaymentCheckoutInfoTestClient(t)

	_, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeAlipay).
		SetName("Official Alipay").
		SetConfig("{}").
		SetSupportedTypes("alipay").
		SetLimits(`{"alipay":{"singleMin":10,"singleMax":100}}`).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	configSvc := service.NewPaymentConfigService(client, &checkoutInfoSettingRepoStub{}, []byte("0123456789abcdef0123456789abcdef"))
	h := NewPaymentHandler(nil, configSvc)

	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/payment/checkout-info", nil)

	h.GetCheckoutInfo(ginCtx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var resp struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	_, exists := resp.Data["nine_plus_enabled"]
	require.False(t, exists)
}

func TestGetCheckoutInfoIncludesNinePlusProductsField(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/payment/checkout-info", nil)

	payload := checkoutInfoResponse{
		Methods:          map[string]service.MethodLimits{},
		Plans:            []checkoutPlan{},
		NinePlusProducts: []checkoutNinePlusProduct{},
	}

	response.Success(ginCtx, payload)

	require.Equal(t, http.StatusOK, recorder.Code)
	var resp struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	products, exists := resp.Data["nineplus_products"]
	require.True(t, exists)
	require.IsType(t, []any{}, products)
}

func TestBuildCheckoutNinePlusProductsIncludesCatalogFields(t *testing.T) {
	stockCount := 8
	products := buildCheckoutNinePlusProducts([]service.ExternalShopProduct{
		{
			ProductID:     "np-sub-49",
			DisplayName:   "Pro 标准月包",
			Description:   "包含 400 额度",
			Category:      "套餐",
			Currency:      "CNY",
			Price:         49.9,
			Fee:           1.0,
			PaymentAmount: 50.9,
			Quota:         400,
			QuotaUnit:     "USD",
			Badge:         "套餐",
			Enabled:       true,
			StockCount:    &stockCount,
			SortOrder:     1,
		},
	})

	require.Len(t, products, 1)
	require.Equal(t, "套餐", products[0].Category)
	require.Equal(t, 1.0, products[0].Fee)
	require.Equal(t, &stockCount, products[0].StockCount)
}

func newPaymentCheckoutInfoTestClient(t *testing.T) *dbent.Client {
	t.Helper()

	dbName := fmt.Sprintf(
		"file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()),
	)
	db, err := sql.Open("sqlite", dbName)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })
	return client
}

type checkoutInfoSettingRepoStub struct{}

func (s *checkoutInfoSettingRepoStub) Get(context.Context, string) (*service.Setting, error) {
	return nil, nil
}

func (s *checkoutInfoSettingRepoStub) GetValue(context.Context, string) (string, error) {
	return "", nil
}

func (s *checkoutInfoSettingRepoStub) Set(context.Context, string, string) error { return nil }

func (s *checkoutInfoSettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		values[key] = ""
	}
	return values, nil
}

func (s *checkoutInfoSettingRepoStub) SetMultiple(context.Context, map[string]string) error {
	return nil
}

func (s *checkoutInfoSettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}

func (s *checkoutInfoSettingRepoStub) Delete(context.Context, string) error { return nil }
