//go:build alipay_sandbox

// Alipay sandbox end-to-end test for 网站支付 (alipay.trade.page.pay).
//
// It talks to the real sandbox gateway (openapi-sandbox.dl.alipaydev.com), so it
// only builds with the alipay_sandbox tag and needs a sandbox config JSON with
// the shape produced by the Alipay quick-sandbox tool (appIds[0].appId,
// appIds[0].appPrivatePkcsKey, appIds[0].alipayPublicKey, appIds[0].pid).
//
//	ALIPAY_SANDBOX_CONFIG=/path/.alipay-sandbox.json \
//	ALIPAY_SANDBOX_PAYURL_FILE=/tmp/payurl.txt \
//	go test -tags alipay_sandbox -run TestAlipaySandbox -v -timeout 15m ./internal/payment/provider/
//
// The test writes the checkout URL to ALIPAY_SANDBOX_PAYURL_FILE and waits up to
// ALIPAY_SANDBOX_WAIT (default 10m) for the order to be paid with the sandbox
// buyer account in a browser, then runs query, full refund and refund query.
// To resume an order paid after the wait expired, rerun with
// ALIPAY_SANDBOX_ORDER_ID=<out_trade_no logged by the first run>.
package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
)

type alipaySandboxFile struct {
	AppIDs []struct {
		AppID             string `json:"appId"`
		AppPrivatePkcsKey string `json:"appPrivatePkcsKey"`
		AlipayPublicKey   string `json:"alipayPublicKey"`
		PID               string `json:"pid"`
	} `json:"appIds"`
}

func loadAlipaySandbox(t *testing.T) *Alipay {
	t.Helper()
	path := os.Getenv("ALIPAY_SANDBOX_CONFIG")
	if path == "" {
		t.Skip("ALIPAY_SANDBOX_CONFIG not set")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sandbox config: %v", err)
	}
	var cfg alipaySandboxFile
	if err := json.Unmarshal(raw, &cfg); err != nil || len(cfg.AppIDs) == 0 {
		t.Fatalf("parse sandbox config: %v", err)
	}
	app := cfg.AppIDs[0]
	a, err := NewAlipay("sandbox", map[string]string{
		"appId":       app.AppID,
		"privateKey":  app.AppPrivatePkcsKey,
		"publicKey":   app.AlipayPublicKey,
		"sellerId":    app.PID,
		"environment": "sandbox",
		"paymentMode": "redirect",
	})
	if err != nil {
		t.Fatalf("new alipay: %v", err)
	}
	return a
}

func TestAlipaySandboxPagePayE2E(t *testing.T) {
	a := loadAlipaySandbox(t)
	ctx := context.Background()
	orderID := os.Getenv("ALIPAY_SANDBOX_ORDER_ID")
	if orderID == "" {
		orderID = createAlipaySandboxOrder(t, a)
	}
	wait := 10 * time.Minute
	if v := os.Getenv("ALIPAY_SANDBOX_WAIT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			t.Fatalf("ALIPAY_SANDBOX_WAIT: %v", err)
		}
		wait = d
	}

	// The sandbox gateway occasionally answers with an HTML error page, so
	// transient query errors are tolerated while waiting for the buyer.
	var q *payment.QueryOrderResponse
	var err error
	deadline := time.Now().Add(wait)
	for {
		q, err = a.QueryOrder(ctx, orderID)
		if err == nil && q.Status == payment.ProviderStatusPaid {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("order %s not paid before deadline: last err=%v", orderID, err)
		}
		time.Sleep(3 * time.Second)
	}
	if q.Amount != 0.01 || q.TradeNo == "" {
		t.Fatalf("unexpected paid query result: %+v", q)
	}
	t.Logf("paid: trade_no=%s amount=%.2f", q.TradeNo, q.Amount)

	refund, err := a.Refund(ctx, payment.RefundRequest{OrderID: orderID, TradeNo: q.TradeNo, Amount: "0.01", Reason: "sandbox test"})
	if err != nil {
		t.Fatalf("refund: %v", err)
	}
	t.Logf("refund requested: status=%s", refund.Status)

	var rq *payment.RefundResponse
	for i := 0; i < 10; i++ {
		rq, err = a.QueryRefund(ctx, payment.RefundQueryRequest{OrderID: orderID, TradeNo: q.TradeNo, RefundID: refund.RefundID, Amount: "0.01", RequestedAt: time.Now()})
		if err == nil && rq.Status == payment.ProviderStatusSuccess {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if err != nil || rq.Status != payment.ProviderStatusSuccess {
		t.Fatalf("refund not successful: resp=%+v err=%v", rq, err)
	}
	t.Log("refund confirmed by alipay.trade.fastpay.refund.query")
}

func createAlipaySandboxOrder(t *testing.T, a *Alipay) string {
	t.Helper()
	ctx := context.Background()
	orderID := fmt.Sprintf("sub2sbx%d", time.Now().UnixNano())
	resp, err := a.CreatePayment(ctx, payment.CreatePaymentRequest{
		OrderID:     orderID,
		Amount:      "0.01",
		PaymentType: payment.TypeAlipay,
		Subject:     "sub2api sandbox test",
		ReturnURL:   "https://example.com/payment/result",
	})
	if err != nil {
		t.Fatalf("create payment: %v", err)
	}
	if resp.PayURL == "" || resp.QRCode != "" {
		t.Fatalf("expected page.pay checkout URL only, got %+v", resp)
	}
	t.Logf("order %s created", orderID)
	if f := os.Getenv("ALIPAY_SANDBOX_PAYURL_FILE"); f != "" {
		if err := os.WriteFile(f, []byte(resp.PayURL), 0o600); err != nil {
			t.Fatalf("write pay url: %v", err)
		}
	}
	q, err := a.QueryOrder(ctx, orderID)
	if err != nil {
		t.Fatalf("initial query: %v", err)
	}
	if q.Status == payment.ProviderStatusPaid {
		t.Fatalf("order paid before checkout")
	}
	return orderID
}

// TestAlipaySandboxCloseTrade creates an unpaid page.pay order and closes it.
// Alipay only materialises a page.pay trade after the cashier page is opened,
// so ACQ.TRADE_NOT_EXIST on close is treated as success by CancelPayment.
func TestAlipaySandboxCloseTrade(t *testing.T) {
	a := loadAlipaySandbox(t)
	ctx := context.Background()
	orderID := fmt.Sprintf("sub2sbxclose%d", time.Now().UnixNano())
	if _, err := a.CreatePayment(ctx, payment.CreatePaymentRequest{OrderID: orderID, Amount: "0.01", PaymentType: payment.TypeAlipay, Subject: "sub2api sandbox close"}); err != nil {
		t.Fatalf("create payment: %v", err)
	}
	if err := a.CancelPayment(ctx, orderID); err != nil {
		t.Fatalf("close trade: %v", err)
	}
	q, err := a.QueryOrder(ctx, orderID)
	if err != nil {
		t.Fatalf("query after close: %v", err)
	}
	if q.Status == payment.ProviderStatusPaid {
		t.Fatalf("closed order reported paid")
	}
}
