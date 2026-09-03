//go:build unit

package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

type paymentOrderLifecycleQueryProvider struct {
	key               string
	lastQueryTradeNo  string
	lastCancelTradeNo string
	queryCalls        int
	cancelCalls       int
	queryErr          error
	responses         []*payment.QueryOrderResponse
	resp              *payment.QueryOrderResponse
}

type paymentOrderLifecycleRedeemRepo struct {
	codesByCode map[string]*RedeemCode
	useCalls    []struct {
		id     int64
		userID int64
	}
}

type paymentOrderLifecycleLoadBalancer struct {
	config map[string]string
}

func (p *paymentOrderLifecycleQueryProvider) Name() string {
	return "payment-order-lifecycle-query-provider"
}

func (p *paymentOrderLifecycleQueryProvider) ProviderKey() string {
	if p.key != "" {
		return p.key
	}
	return payment.TypeAlipay
}

func (p *paymentOrderLifecycleQueryProvider) SupportedTypes() []payment.PaymentType {
	return []payment.PaymentType{p.ProviderKey()}
}

func (p *paymentOrderLifecycleQueryProvider) CreatePayment(context.Context, payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	panic("unexpected call")
}

func (p *paymentOrderLifecycleQueryProvider) QueryOrder(_ context.Context, tradeNo string) (*payment.QueryOrderResponse, error) {
	p.lastQueryTradeNo = tradeNo
	p.queryCalls++
	if p.queryErr != nil {
		return nil, p.queryErr
	}
	if len(p.responses) > 0 {
		resp := p.responses[0]
		if len(p.responses) > 1 {
			p.responses = p.responses[1:]
		}
		return resp, nil
	}
	return p.resp, nil
}

func (p *paymentOrderLifecycleQueryProvider) VerifyNotification(context.Context, string, map[string]string) (*payment.PaymentNotification, error) {
	panic("unexpected call")
}

func (p *paymentOrderLifecycleQueryProvider) Refund(context.Context, payment.RefundRequest) (*payment.RefundResponse, error) {
	panic("unexpected call")
}

func (p *paymentOrderLifecycleQueryProvider) CancelPayment(_ context.Context, tradeNo string) error {
	p.lastCancelTradeNo = tradeNo
	p.cancelCalls++
	return nil
}

func (lb paymentOrderLifecycleLoadBalancer) GetInstanceConfig(context.Context, int64) (map[string]string, error) {
	cfg := make(map[string]string, len(lb.config))
	for key, value := range lb.config {
		cfg[key] = value
	}
	return cfg, nil
}

func (lb paymentOrderLifecycleLoadBalancer) SelectInstance(context.Context, string, payment.PaymentType, payment.Strategy, float64) (*payment.InstanceSelection, error) {
	panic("unexpected call")
}

func (r *paymentOrderLifecycleRedeemRepo) Create(context.Context, *RedeemCode) error {
	panic("unexpected call")
}

func (r *paymentOrderLifecycleRedeemRepo) CreateBatch(context.Context, []RedeemCode) error {
	panic("unexpected call")
}

func (r *paymentOrderLifecycleRedeemRepo) GetByID(_ context.Context, id int64) (*RedeemCode, error) {
	for _, code := range r.codesByCode {
		if code.ID != id {
			continue
		}
		cloned := *code
		return &cloned, nil
	}
	return nil, ErrRedeemCodeNotFound
}

func (r *paymentOrderLifecycleRedeemRepo) GetByCode(_ context.Context, code string) (*RedeemCode, error) {
	redeemCode, ok := r.codesByCode[code]
	if !ok {
		return nil, ErrRedeemCodeNotFound
	}
	cloned := *redeemCode
	return &cloned, nil
}

func (r *paymentOrderLifecycleRedeemRepo) Update(context.Context, *RedeemCode) error {
	panic("unexpected call")
}

func (r *paymentOrderLifecycleRedeemRepo) BatchUpdate(context.Context, []int64, RedeemCodeBatchUpdateFields) (int64, error) {
	panic("unexpected call")
}

func (r *paymentOrderLifecycleRedeemRepo) Delete(context.Context, int64) error {
	panic("unexpected call")
}

func (r *paymentOrderLifecycleRedeemRepo) Use(_ context.Context, id, userID int64) error {
	for code, redeemCode := range r.codesByCode {
		if redeemCode.ID != id {
			continue
		}
		now := time.Now().UTC()
		redeemCode.Status = StatusUsed
		redeemCode.UsedBy = &userID
		redeemCode.UsedAt = &now
		r.codesByCode[code] = redeemCode
		r.useCalls = append(r.useCalls, struct {
			id     int64
			userID int64
		}{id: id, userID: userID})
		return nil
	}
	return ErrRedeemCodeNotFound
}

func (r *paymentOrderLifecycleRedeemRepo) List(context.Context, pagination.PaginationParams) ([]RedeemCode, *pagination.PaginationResult, error) {
	panic("unexpected call")
}

func (r *paymentOrderLifecycleRedeemRepo) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string) ([]RedeemCode, *pagination.PaginationResult, error) {
	panic("unexpected call")
}

func (r *paymentOrderLifecycleRedeemRepo) ListByUser(context.Context, int64, int) ([]RedeemCode, error) {
	panic("unexpected call")
}

func (r *paymentOrderLifecycleRedeemRepo) ListByUserPaginated(context.Context, int64, pagination.PaginationParams, string) ([]RedeemCode, *pagination.PaginationResult, error) {
	panic("unexpected call")
}

func (r *paymentOrderLifecycleRedeemRepo) SumPositiveBalanceByUser(context.Context, int64) (float64, error) {
	panic("unexpected call")
}

func TestVerifyOrderByOutTradeNoBackfillsTradeNoFromPaidQuery(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("checkpaid@example.com").
		SetPasswordHash("hash").
		SetUsername("checkpaid-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("CHECKPAID-UPSTREAM-TRADE-NO").
		SetOutTradeNo("sub2_checkpaid_trade_no_missing").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	userRepo := &mockUserRepo{
		getByIDUser: &User{
			ID:       user.ID,
			Email:    user.Email,
			Username: user.Username,
			Balance:  0,
		},
	}
	userRepo.updateBalanceFn = func(ctx context.Context, id int64, amount float64) error {
		require.Equal(t, user.ID, id)
		if userRepo.getByIDUser != nil {
			userRepo.getByIDUser.Balance += amount
		}
		return nil
	}
	redeemRepo := &paymentOrderLifecycleRedeemRepo{
		codesByCode: map[string]*RedeemCode{
			order.RechargeCode: {
				ID:     1,
				Code:   order.RechargeCode,
				Type:   RedeemTypeBalance,
				Value:  order.Amount,
				Status: StatusUnused,
			},
		},
	}
	redeemService := NewRedeemService(
		redeemRepo,
		userRepo,
		nil,
		nil,
		nil,
		client,
		nil,
		nil,
	)
	registry := payment.NewRegistry()
	provider := &paymentOrderLifecycleQueryProvider{
		resp: &payment.QueryOrderResponse{
			TradeNo: "upstream-trade-123",
			Status:  payment.ProviderStatusPaid,
			Amount:  88,
		},
	}
	registry.Register(provider)

	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		redeemService:   redeemService,
		userRepo:        userRepo,
		providersLoaded: true,
	}

	got, err := svc.VerifyOrderByOutTradeNo(ctx, order.OutTradeNo, user.ID)
	require.NoError(t, err)
	require.Equal(t, order.OutTradeNo, provider.lastQueryTradeNo)
	require.Equal(t, OrderStatusCompleted, got.Status)
	require.Equal(t, "upstream-trade-123", got.PaymentTradeNo)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, reloaded.Status)
	require.Equal(t, "upstream-trade-123", reloaded.PaymentTradeNo)

	require.Equal(t, 88.0, userRepo.getByIDUser.Balance)
	require.Len(t, redeemRepo.useCalls, 1)
	require.Equal(t, int64(1), redeemRepo.useCalls[0].id)
	require.Equal(t, user.ID, redeemRepo.useCalls[0].userID)
}

func TestVerifyOrderByOutTradeNoRetriesZeroAmountPaidQueryOnce(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("checkpaid-retry@example.com").
		SetPasswordHash("hash").
		SetUsername("checkpaid-retry-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("CHECKPAID-UPSTREAM-RETRY").
		SetOutTradeNo("sub2_checkpaid_retry_zero_amount").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	userRepo := &mockUserRepo{
		getByIDUser: &User{
			ID:       user.ID,
			Email:    user.Email,
			Username: user.Username,
			Balance:  0,
		},
	}
	userRepo.updateBalanceFn = func(ctx context.Context, id int64, amount float64) error {
		require.Equal(t, user.ID, id)
		if userRepo.getByIDUser != nil {
			userRepo.getByIDUser.Balance += amount
		}
		return nil
	}
	redeemRepo := &paymentOrderLifecycleRedeemRepo{
		codesByCode: map[string]*RedeemCode{
			order.RechargeCode: {
				ID:     1,
				Code:   order.RechargeCode,
				Type:   RedeemTypeBalance,
				Value:  order.Amount,
				Status: StatusUnused,
			},
		},
	}
	redeemService := NewRedeemService(
		redeemRepo,
		userRepo,
		nil,
		nil,
		nil,
		client,
		nil,
		nil,
	)
	registry := payment.NewRegistry()
	provider := &paymentOrderLifecycleQueryProvider{
		responses: []*payment.QueryOrderResponse{
			{
				TradeNo: "upstream-trade-zero",
				Status:  payment.ProviderStatusPaid,
				Amount:  0,
			},
			{
				TradeNo: "upstream-trade-retry",
				Status:  payment.ProviderStatusPaid,
				Amount:  88,
			},
		},
	}
	registry.Register(provider)

	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		redeemService:   redeemService,
		userRepo:        userRepo,
		providersLoaded: true,
	}

	got, err := svc.VerifyOrderByOutTradeNo(ctx, order.OutTradeNo, user.ID)
	require.NoError(t, err)
	require.Equal(t, 2, provider.queryCalls)
	require.Equal(t, OrderStatusCompleted, got.Status)
	require.Equal(t, "upstream-trade-retry", got.PaymentTradeNo)
}

func TestVerifyOrderByOutTradeNoRejectsPaidQueryWithZeroAmount(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("checkpaid-zero-amount@example.com").
		SetPasswordHash("hash").
		SetUsername("checkpaid-zero-amount-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("CHECKPAID-ZERO-AMOUNT").
		SetOutTradeNo("sub2_checkpaid_zero_amount").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	userRepo := &mockUserRepo{
		getByIDUser: &User{
			ID:       user.ID,
			Email:    user.Email,
			Username: user.Username,
			Balance:  0,
		},
	}
	redeemRepo := &paymentOrderLifecycleRedeemRepo{
		codesByCode: map[string]*RedeemCode{
			order.RechargeCode: {
				ID:     1,
				Code:   order.RechargeCode,
				Type:   RedeemTypeBalance,
				Value:  order.Amount,
				Status: StatusUnused,
			},
		},
	}
	redeemService := NewRedeemService(
		redeemRepo,
		userRepo,
		nil,
		nil,
		nil,
		client,
		nil,
		nil,
	)
	registry := payment.NewRegistry()
	provider := &paymentOrderLifecycleQueryProvider{
		resp: &payment.QueryOrderResponse{
			TradeNo: "upstream-trade-zero",
			Status:  payment.ProviderStatusPaid,
			Amount:  0,
		},
	}
	registry.Register(provider)

	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		redeemService:   redeemService,
		userRepo:        userRepo,
		providersLoaded: true,
	}

	got, err := svc.VerifyOrderByOutTradeNo(ctx, order.OutTradeNo, user.ID)
	require.NoError(t, err)
	require.Equal(t, order.OutTradeNo, provider.lastQueryTradeNo)
	require.Equal(t, OrderStatusPending, got.Status)
	require.Empty(t, got.PaymentTradeNo)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, reloaded.Status)
	require.Empty(t, reloaded.PaymentTradeNo)

	require.Equal(t, 0.0, userRepo.getByIDUser.Balance)
	require.Empty(t, redeemRepo.useCalls)
}

func TestVerifyOrderByOutTradeNoDoesNotCancelUnpaidUpstreamOrder(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("checkpaid-pending@example.com").
		SetPasswordHash("hash").
		SetUsername("checkpaid-pending-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("CHECKPAID-PENDING").
		SetOutTradeNo("sub2_checkpaid_pending").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	registry := payment.NewRegistry()
	provider := &paymentOrderLifecycleQueryProvider{
		resp: &payment.QueryOrderResponse{
			TradeNo: order.OutTradeNo,
			Status:  payment.ProviderStatusPending,
			Amount:  0,
		},
	}
	registry.Register(provider)

	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		providersLoaded: true,
	}

	got, err := svc.VerifyOrderByOutTradeNo(ctx, order.OutTradeNo, user.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, got.Status)
	require.Equal(t, order.OutTradeNo, provider.lastQueryTradeNo)
	require.Zero(t, provider.cancelCalls)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, reloaded.Status)
}

func TestCancelOrderStillClosesUnpaidUpstreamOrder(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("cancel-pending@example.com").
		SetPasswordHash("hash").
		SetUsername("cancel-pending-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("CANCEL-PENDING").
		SetOutTradeNo("sub2_cancel_pending").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	registry := payment.NewRegistry()
	provider := &paymentOrderLifecycleQueryProvider{
		resp: &payment.QueryOrderResponse{
			TradeNo: order.OutTradeNo,
			Status:  payment.ProviderStatusPending,
			Amount:  0,
		},
	}
	registry.Register(provider)

	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		providersLoaded: true,
	}

	outcome, err := svc.CancelOrder(ctx, order.ID, user.ID)
	require.NoError(t, err)
	require.Equal(t, checkPaidResultCancelled, outcome)
	require.Equal(t, order.OutTradeNo, provider.lastCancelTradeNo)
	require.Equal(t, 1, provider.cancelCalls)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCancelled, reloaded.Status)
}

func TestReconcilePendingPaymentOrdersBackfillsPaidOrder(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("wxpay-reconcile@example.com").
		SetPasswordHash("hash").
		SetUsername("wxpay-reconcile-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(50).
		SetPayAmount(50).
		SetFeeRate(0).
		SetRechargeCode("WXPAY-RECONCILE").
		SetOutTradeNo("sub2_wxpay_reconcile").
		SetPaymentType(payment.TypeWxpay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	userRepo := &mockUserRepo{
		getByIDUser: &User{
			ID:       user.ID,
			Email:    user.Email,
			Username: user.Username,
			Balance:  0,
		},
	}
	userRepo.updateBalanceFn = func(ctx context.Context, id int64, amount float64) error {
		require.Equal(t, user.ID, id)
		if userRepo.getByIDUser != nil {
			userRepo.getByIDUser.Balance += amount
		}
		return nil
	}
	redeemRepo := &paymentOrderLifecycleRedeemRepo{
		codesByCode: map[string]*RedeemCode{
			order.RechargeCode: {
				ID:     1,
				Code:   order.RechargeCode,
				Type:   RedeemTypeBalance,
				Value:  order.Amount,
				Status: StatusUnused,
			},
		},
	}
	redeemService := NewRedeemService(
		redeemRepo,
		userRepo,
		nil,
		nil,
		nil,
		client,
		nil,
		nil,
	)
	registry := payment.NewRegistry()
	provider := &paymentOrderLifecycleQueryProvider{
		key: payment.TypeWxpay,
		resp: &payment.QueryOrderResponse{
			TradeNo: "wxpay-upstream-trade-123",
			Status:  payment.ProviderStatusPaid,
			Amount:  50,
			Metadata: map[string]string{
				"trade_state": "SUCCESS",
			},
		},
	}
	registry.Register(provider)

	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		redeemService:   redeemService,
		userRepo:        userRepo,
		providersLoaded: true,
	}

	recovered, err := svc.ReconcilePendingPaymentOrders(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, recovered)
	require.Equal(t, order.OutTradeNo, provider.lastQueryTradeNo)
	require.Zero(t, provider.cancelCalls)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, reloaded.Status)
	require.Equal(t, "wxpay-upstream-trade-123", reloaded.PaymentTradeNo)
	require.Equal(t, 50.0, userRepo.getByIDUser.Balance)
	require.Len(t, redeemRepo.useCalls, 1)
}

func TestReconcilePendingHaozPayOrdersFallsBackToCashierOrderNo(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	privDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	privateKeyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER}))
	publicKeyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}))

	var queryOrderNos []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/pay-core/payment/queryOrderDetail", r.URL.Path)
		var queryPayload map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&queryPayload))
		bizBodyString, ok := queryPayload["bizBody"].(string)
		require.True(t, ok)
		var bizBody map[string]any
		decoder := json.NewDecoder(strings.NewReader(bizBodyString))
		decoder.UseNumber()
		require.NoError(t, decoder.Decode(&bizBody))
		orderNo, _ := bizBody["orderNo"].(string)
		queryOrderNos = append(queryOrderNos, orderNo)
		w.Header().Set("Content-Type", "application/json")
		if orderNo == "sub2_haozpay_reconcile" {
			_, _ = w.Write([]byte(`{"code":310054,"message":"订单不存在","data":null}`))
			return
		}
		require.Equal(t, "HZHT_RECONCILE", orderNo)
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"reqSeqId":"HZHT_RECONCILE","orderNo":"HZHT_RECONCILE","merchantOrderNo":"sub2_haozpay_reconcile","transStat":"S","payAmt":1.01}}`))
	}))
	t.Cleanup(server.Close)

	user, err := client.User.Create().
		SetEmail("haozpay-reconcile@example.com").
		SetPasswordHash("hash").
		SetUsername("haozpay-reconcile-user").
		Save(ctx)
	require.NoError(t, err)
	providerConfig := map[string]string{
		"merchantNo":        "HZ_MERCHANT",
		"privateKey":        privateKeyPEM,
		"platformPublicKey": publicKeyPEM,
		"notifyUrl":         "https://example.com/api/v1/payment/webhook/haozpay",
		"apiBase":           server.URL,
	}
	config, err := json.Marshal(providerConfig)
	require.NoError(t, err)
	instance, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeHaozPay).
		SetName("haozpay-test").
		SetConfig(string(config)).
		SetSupportedTypes(payment.TypeAlipay).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)
	instanceID := strconv.FormatInt(instance.ID, 10)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(7.5).
		SetPayAmount(1.01).
		SetFeeRate(0).
		SetRechargeCode("HAOZPAY-RECONCILE").
		SetOutTradeNo("sub2_haozpay_reconcile").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusExpired).
		SetExpiresAt(time.Now().Add(-time.Minute)).
		SetQrCode("https://cashier.haozpay.com?orderNo=HZHT_RECONCILE&merchantNo=HZ_MERCHANT").
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		SetProviderInstanceID(instanceID).
		SetProviderKey(payment.TypeHaozPay).
		Save(ctx)
	require.NoError(t, err)

	userRepo := &mockUserRepo{
		getByIDUser: &User{
			ID:       user.ID,
			Email:    user.Email,
			Username: user.Username,
			Balance:  0,
		},
	}
	userRepo.updateBalanceFn = func(ctx context.Context, id int64, amount float64) error {
		require.Equal(t, user.ID, id)
		if userRepo.getByIDUser != nil {
			userRepo.getByIDUser.Balance += amount
		}
		return nil
	}
	redeemRepo := &paymentOrderLifecycleRedeemRepo{
		codesByCode: map[string]*RedeemCode{
			order.RechargeCode: {
				ID:     1,
				Code:   order.RechargeCode,
				Type:   RedeemTypeBalance,
				Value:  order.Amount,
				Status: StatusUnused,
			},
		},
	}
	redeemService := NewRedeemService(
		redeemRepo,
		userRepo,
		nil,
		nil,
		nil,
		client,
		nil,
		nil,
	)
	svc := &PaymentService{
		entClient:       client,
		registry:        payment.NewRegistry(),
		loadBalancer:    paymentOrderLifecycleLoadBalancer{config: providerConfig},
		redeemService:   redeemService,
		userRepo:        userRepo,
		providersLoaded: true,
	}

	recovered, err := svc.ReconcilePendingHaozPayOrders(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, recovered)
	require.Equal(t, []string{order.OutTradeNo, "HZHT_RECONCILE"}, queryOrderNos)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, reloaded.Status)
	require.Equal(t, "HZHT_RECONCILE", reloaded.PaymentTradeNo)
	require.Equal(t, 7.5, userRepo.getByIDUser.Balance)
	require.Len(t, redeemRepo.useCalls, 1)
}

func TestReconcilePendingNinePlusOrdersQueriesOnlyPendingUnexpiredNinePlusOrders(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("nineplus-reconcile-pending@example.com").
		SetPasswordHash("hash").
		SetUsername("nineplus-reconcile-pending-user").
		Save(ctx)
	require.NoError(t, err)

	now := time.Now()
	activeOrder, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(35).
		SetPayAmount(5.1).
		SetFeeRate(0).
		SetRechargeCode("NINEPLUS-PENDING-ACTIVE").
		SetOutTradeNo("sub2_nineplus_pending_active").
		SetPaymentType(payment.TypeNinePlus).
		SetPaymentTradeNo("9P_ACTIVE").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(now.Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)
	_, err = client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(35).
		SetPayAmount(5.1).
		SetFeeRate(0).
		SetRechargeCode("NINEPLUS-PENDING-EXPIRED").
		SetOutTradeNo("sub2_nineplus_pending_expired").
		SetPaymentType(payment.TypeNinePlus).
		SetPaymentTradeNo("9P_EXPIRED").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(now.Add(-time.Minute)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)
	_, err = client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(35).
		SetPayAmount(5.1).
		SetFeeRate(0).
		SetRechargeCode("ALIPAY-PENDING-ACTIVE").
		SetOutTradeNo("sub2_alipay_pending_active").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("ALIPAY_ACTIVE").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(now.Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	registry := payment.NewRegistry()
	provider := &paymentOrderLifecycleQueryProvider{
		key: payment.TypeNinePlus,
		resp: &payment.QueryOrderResponse{
			TradeNo: "9P_ACTIVE",
			Status:  payment.ProviderStatusPending,
		},
	}
	registry.Register(provider)

	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		providersLoaded: true,
	}

	recovered, err := svc.ReconcilePendingNinePlusOrders(ctx)
	require.NoError(t, err)
	require.Zero(t, recovered)
	require.Equal(t, 1, provider.queryCalls)
	require.Equal(t, activeOrder.PaymentTradeNo, provider.lastQueryTradeNo)
	require.Zero(t, provider.cancelCalls)
}

func TestReconcilePendingNinePlusOrdersCompletesPaidDeliveredOrder(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	var orderInfoPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/payApi/common/checkOrderStatus.html":
			require.Equal(t, http.MethodPost, r.Method)
			require.NoError(t, r.ParseForm())
			require.Equal(t, "9P_PAID_DELIVERED", r.Form.Get("trade_no"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":1,"msg":"success","data":{"url":"/order/result/9P_PAID_DELIVERED"}}`))
		case "/shopApi/Order/info":
			require.Equal(t, http.MethodPost, r.Method)
			require.NoError(t, json.NewDecoder(r.Body).Decode(&orderInfoPayload))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":1,"msg":"success","data":{"trade_no":"9P_PAID_DELIVERED","goods_name":"35刀额度","quantity":1,"total_amount":5.1,"status":1,"success_time":1781724711,"sendout":1,"contact":"buyer@example.com","response":{"cards":["CARD-35-USD"],"export_cards_url":""}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	user, err := client.User.Create().
		SetEmail("nineplus-reconcile-paid@example.com").
		SetPasswordHash("hash").
		SetUsername("nineplus-reconcile-paid-user").
		Save(ctx)
	require.NoError(t, err)

	providerConfig := map[string]string{
		"apiBase":        server.URL,
		"shopToken":      "shop-token",
		"defaultContact": "fallback@example.com",
	}
	config, err := json.Marshal(providerConfig)
	require.NoError(t, err)
	instance, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeNinePlus).
		SetName("nineplus-test").
		SetConfig(string(config)).
		SetSupportedTypes(payment.TypeNinePlus).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)
	instanceID := strconv.FormatInt(instance.ID, 10)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(35).
		SetPayAmount(5.1).
		SetFeeRate(0).
		SetRechargeCode("NINEPLUS-PAID-DELIVERED").
		SetOutTradeNo("sub2_nineplus_paid_delivered").
		SetPaymentType(payment.TypeNinePlus).
		SetPaymentTradeNo("9P_PAID_DELIVERED").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		SetProviderInstanceID(instanceID).
		SetProviderKey(payment.TypeNinePlus).
		SetProviderSnapshot(map[string]any{
			"schema_version":       2,
			"provider_instance_id": instanceID,
			"provider_key":         payment.TypeNinePlus,
			"external_product_id":  "np-35",
			"external_quantity":    1,
			"contact":              "buyer@example.com",
		}).
		Save(ctx)
	require.NoError(t, err)

	userRepo := &mockUserRepo{
		getByIDUser: &User{
			ID:       user.ID,
			Email:    user.Email,
			Username: user.Username,
			Balance:  0,
		},
	}
	userRepo.updateBalanceFn = func(ctx context.Context, id int64, amount float64) error {
		require.Equal(t, user.ID, id)
		if userRepo.getByIDUser != nil {
			userRepo.getByIDUser.Balance += amount
		}
		return nil
	}
	redeemRepo := &paymentOrderLifecycleRedeemRepo{
		codesByCode: map[string]*RedeemCode{
			"CARD-35-USD": {
				ID:     1,
				Code:   "CARD-35-USD",
				Type:   RedeemTypeBalance,
				Value:  35,
				Status: StatusUnused,
			},
		},
	}
	redeemService := NewRedeemService(
		redeemRepo,
		userRepo,
		nil,
		nil,
		nil,
		client,
		nil,
		nil,
	)
	registry := payment.NewRegistry()

	svc := &PaymentService{
		entClient:           client,
		registry:            registry,
		loadBalancer:        paymentOrderLifecycleLoadBalancer{config: providerConfig},
		redeemService:       redeemService,
		userRepo:            userRepo,
		externalShopService: NewExternalShopService(client, redeemService),
		configService:       &PaymentConfigService{entClient: client},
		providersLoaded:     true,
	}

	recovered, err := svc.ReconcilePendingNinePlusOrders(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, recovered)
	require.Equal(t, "9P_PAID_DELIVERED", orderInfoPayload["trade_no"])
	require.Equal(t, "buyer@example.com", orderInfoPayload["query_password"])

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, reloaded.Status)
	require.Equal(t, "9P_PAID_DELIVERED", reloaded.PaymentTradeNo)
	require.Equal(t, 35.0, reloaded.Amount)
	require.Equal(t, 35.0, userRepo.getByIDUser.Balance)
	require.Len(t, redeemRepo.useCalls, 1)
	require.Equal(t, user.ID, redeemRepo.useCalls[0].userID)
}

func TestReconcilePendingNinePlusOrdersKeepsPaidOrderRetryableOnTransientFulfillmentError(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/shopApi/Order/info":
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`bad gateway`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	user, err := client.User.Create().
		SetEmail("nineplus-transient@example.com").
		SetPasswordHash("hash").
		SetUsername("nineplus-transient-user").
		Save(ctx)
	require.NoError(t, err)

	providerConfig := map[string]string{
		"apiBase":        server.URL,
		"shopToken":      "shop-token",
		"defaultContact": "fallback@example.com",
	}
	config, err := json.Marshal(providerConfig)
	require.NoError(t, err)
	instance, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeNinePlus).
		SetName("nineplus-transient").
		SetConfig(string(config)).
		SetSupportedTypes(payment.TypeNinePlus).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)
	instanceID := strconv.FormatInt(instance.ID, 10)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(75).
		SetPayAmount(10.2).
		SetFeeRate(0).
		SetRechargeCode("NINEPLUS-TRANSIENT").
		SetOutTradeNo("sub2_nineplus_transient").
		SetPaymentType(payment.TypeNinePlus).
		SetPaymentTradeNo("9P_TRANSIENT").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPaid).
		SetPaidAt(time.Now()).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		SetProviderInstanceID(instanceID).
		SetProviderKey(payment.TypeNinePlus).
		SetProviderSnapshot(map[string]any{
			"schema_version":       2,
			"provider_instance_id": instanceID,
			"provider_key":         payment.TypeNinePlus,
			"external_product_id":  "np-75",
			"external_quantity":    1,
			"contact":              "buyer@example.com",
		}).
		Save(ctx)
	require.NoError(t, err)

	redeemService := NewRedeemService(&paymentOrderLifecycleRedeemRepo{codesByCode: map[string]*RedeemCode{}}, &mockUserRepo{}, nil, nil, nil, client, nil, nil)
	svc := &PaymentService{
		entClient:           client,
		loadBalancer:        paymentOrderLifecycleLoadBalancer{config: providerConfig},
		redeemService:       redeemService,
		externalShopService: NewExternalShopService(client, redeemService),
		configService:       &PaymentConfigService{entClient: client},
		providersLoaded:     true,
	}

	err = svc.executeFulfillment(ctx, order.ID)
	require.Error(t, err)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPaid, reloaded.Status)
	require.Nil(t, reloaded.FailedAt)
	require.Nil(t, reloaded.FailedReason)
	failLogs, err := client.PaymentAuditLog.Query().
		Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(order.ID, 10)), paymentauditlog.ActionEQ("FULFILLMENT_FAILED")).
		Count(ctx)
	require.NoError(t, err)
	require.Zero(t, failLogs)
}

func TestExecuteNinePlusFulfillmentSkipsAlreadyRechargingOrder(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("nineplus-recharging@example.com").
		SetPasswordHash("hash").
		SetUsername("nineplus-recharging-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(75).
		SetPayAmount(10.2).
		SetFeeRate(0).
		SetRechargeCode("NINEPLUS-RECHARGING").
		SetOutTradeNo("sub2_nineplus_recharging").
		SetPaymentType(payment.TypeNinePlus).
		SetPaymentTradeNo("9P_RECHARGING").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusRecharging).
		SetPaidAt(time.Now()).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		SetProviderInstanceID("1").
		SetProviderKey(payment.TypeNinePlus).
		SetProviderSnapshot(map[string]any{
			"schema_version":       2,
			"provider_instance_id": "1",
			"provider_key":         payment.TypeNinePlus,
			"external_product_id":  "np-75",
			"external_quantity":    1,
			"contact":              "buyer@example.com",
		}).
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{entClient: client}

	err = svc.ExecuteNinePlusFulfillment(ctx, order.ID)
	require.NoError(t, err)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusRecharging, reloaded.Status)
	successLogs, err := client.PaymentAuditLog.Query().
		Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(order.ID, 10)), paymentauditlog.ActionEQ("RECHARGE_SUCCESS")).
		Count(ctx)
	require.NoError(t, err)
	require.Zero(t, successLogs)
}

func TestMarkCompletedDoesNotWriteSuccessAuditWhenStatusWasNotUpdated(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("mark-completed-noop@example.com").
		SetPasswordHash("hash").
		SetUsername("mark-completed-noop-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(75).
		SetPayAmount(10.2).
		SetFeeRate(0).
		SetRechargeCode("MARK-COMPLETED-NOOP").
		SetOutTradeNo("sub2_mark_completed_noop").
		SetPaymentType(payment.TypeNinePlus).
		SetPaymentTradeNo("9P_MARK_COMPLETED_NOOP").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusFailed).
		SetPaidAt(time.Now()).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{entClient: client}
	err = svc.markCompleted(ctx, order, &paymentFulfillmentLease{version: order.UpdatedAt}, "RECHARGE_SUCCESS")
	require.Error(t, err)
	require.Equal(t, "CONFLICT", infraerrors.Reason(err))

	successLogs, err := client.PaymentAuditLog.Query().
		Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(order.ID, 10)), paymentauditlog.ActionEQ("RECHARGE_SUCCESS")).
		Count(ctx)
	require.NoError(t, err)
	require.Zero(t, successLogs)
}

func TestVerifyOrderByOutTradeNoUsesOutTradeNoWhenPaymentTradeNoAlreadyExistsForAlipay(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("checkpaid-existing-trade@example.com").
		SetPasswordHash("hash").
		SetUsername("checkpaid-existing-trade-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("CHECKPAID-EXISTING-TRADE-NO").
		SetOutTradeNo("sub2_checkpaid_use_out_trade_no").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("upstream-trade-existing").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	userRepo := &mockUserRepo{
		getByIDUser: &User{
			ID:       user.ID,
			Email:    user.Email,
			Username: user.Username,
			Balance:  0,
		},
	}
	userRepo.updateBalanceFn = func(ctx context.Context, id int64, amount float64) error {
		require.Equal(t, user.ID, id)
		if userRepo.getByIDUser != nil {
			userRepo.getByIDUser.Balance += amount
		}
		return nil
	}
	redeemRepo := &paymentOrderLifecycleRedeemRepo{
		codesByCode: map[string]*RedeemCode{
			order.RechargeCode: {
				ID:     1,
				Code:   order.RechargeCode,
				Type:   RedeemTypeBalance,
				Value:  order.Amount,
				Status: StatusUnused,
			},
		},
	}
	redeemService := NewRedeemService(
		redeemRepo,
		userRepo,
		nil,
		nil,
		nil,
		client,
		nil,
		nil,
	)
	registry := payment.NewRegistry()
	provider := &paymentOrderLifecycleQueryProvider{
		resp: &payment.QueryOrderResponse{
			TradeNo: "upstream-trade-existing",
			Status:  payment.ProviderStatusPaid,
			Amount:  88,
		},
	}
	registry.Register(provider)

	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		redeemService:   redeemService,
		userRepo:        userRepo,
		providersLoaded: true,
	}

	got, err := svc.VerifyOrderByOutTradeNo(ctx, order.OutTradeNo, user.ID)
	require.NoError(t, err)
	require.Equal(t, order.OutTradeNo, provider.lastQueryTradeNo)
	require.Equal(t, "upstream-trade-existing", got.PaymentTradeNo)
}

func TestPaymentOrderAllowsRegistryFallbackOnlyForLegacyOrdersWithoutPinnedProviderState(t *testing.T) {
	t.Parallel()

	require.True(t, paymentOrderAllowsRegistryFallback(&dbent.PaymentOrder{
		PaymentType: payment.TypeAlipay,
	}))

	instanceID := "12"
	require.False(t, paymentOrderAllowsRegistryFallback(&dbent.PaymentOrder{
		PaymentType:        payment.TypeAlipay,
		ProviderInstanceID: &instanceID,
	}))

	require.False(t, paymentOrderAllowsRegistryFallback(&dbent.PaymentOrder{
		PaymentType: payment.TypeAlipay,
		ProviderSnapshot: map[string]any{
			"schema_version":       2,
			"provider_instance_id": "12",
		},
	}))
}

func TestPaymentOrderQueryReferenceUsesOutTradeNoForOfficialProviders(t *testing.T) {
	t.Parallel()

	order := &dbent.PaymentOrder{
		PaymentType:    payment.TypeWxpay,
		OutTradeNo:     "sub2_out_trade_no",
		PaymentTradeNo: "wx-transaction-id",
	}

	require.Equal(t, "sub2_out_trade_no", paymentOrderQueryReference(order, &paymentOrderLifecycleQueryProvider{}))
	require.Equal(t, "sub2_out_trade_no", paymentOrderQueryReference(order, paymentFulfillmentTestProvider{
		key: payment.TypeWxpay,
	}))
}

func TestPaymentOrderQueryReferenceUsesMerchantOrderNoForHaozPay(t *testing.T) {
	t.Parallel()

	order := &dbent.PaymentOrder{
		OutTradeNo:     "20260704BIdEeZRA",
		PaymentType:    payment.TypeAlipay,
		PaymentTradeNo: "",
		QrCode:         paymentOrderLifecycleStringPtr("https://cashier.haozpay.com/cashier-pc?orderNo=HZHT202607042073263983919542272&merchantNo=HZ2072997091048607744"),
		ProviderKey:    paymentOrderLifecycleStringPtr(payment.TypeHaozPay),
	}

	require.Equal(t, order.OutTradeNo, paymentOrderQueryReference(order, paymentFulfillmentTestProvider{
		key: payment.TypeHaozPay,
	}))
	require.Equal(t, "HZHT202607042073263983919542272", haozPayOrderPlatformTradeNo(order))
}

func paymentOrderLifecycleStringPtr(value string) *string {
	return &value
}

func newPaymentOrderLifecycleTestClient(t *testing.T) *dbent.Client {
	t.Helper()

	db, err := sql.Open("sqlite", "file:payment_order_lifecycle?mode=memory&cache=shared&_fk=1")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })
	return client
}
