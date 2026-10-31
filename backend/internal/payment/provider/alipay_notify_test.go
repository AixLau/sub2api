package provider

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/smartwalle/alipay/v3"
)

// alipayNotifyFixture signs notifications with a locally generated key that
// stands in for Alipay's platform key, so VerifyNotification runs real RSA2 checks.
type alipayNotifyFixture struct {
	key    *rsa.PrivateKey
	config map[string]string
}

func newAlipayNotifyFixture(t *testing.T) *alipayNotifyFixture {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pub, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	return &alipayNotifyFixture{
		key: key,
		config: map[string]string{
			"appId":      "2021000000000001",
			"privateKey": string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})),
			"publicKey":  string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub})),
		},
	}
}

func (f *alipayNotifyFixture) sign(t *testing.T, fields map[string]string) string {
	t.Helper()
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+fields[k])
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "&")))
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	values := url.Values{}
	for k, v := range fields {
		values.Set(k, v)
	}
	values.Set("sign_type", "RSA2")
	values.Set("sign", base64.StdEncoding.EncodeToString(sig))
	return values.Encode()
}

func basePaidNotification() map[string]string {
	return map[string]string{
		"app_id":       "2021000000000001",
		"notify_id":    "notify-1",
		"notify_type":  "trade_status_sync",
		"out_trade_no": "sub2_order_1",
		"trade_no":     "2026103122001400000000000001",
		"trade_status": "TRADE_SUCCESS",
		"total_amount": "12.34",
		"seller_id":    "2088000000000001",
	}
}

func TestAlipayVerifyNotificationPaid(t *testing.T) {
	f := newAlipayNotifyFixture(t)
	a, err := NewAlipay("inst", f.config)
	if err != nil {
		t.Fatal(err)
	}
	n, err := a.VerifyNotification(context.Background(), f.sign(t, basePaidNotification()), nil)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if n.Status != payment.ProviderStatusSuccess || n.OrderID != "sub2_order_1" || n.Amount != 12.34 {
		t.Fatalf("unexpected notification: %+v", n)
	}
	if n.Metadata["app_id"] != "2021000000000001" {
		t.Fatalf("app_id metadata missing: %+v", n.Metadata)
	}
}

func TestAlipayVerifyNotificationRejectsTamperedBody(t *testing.T) {
	f := newAlipayNotifyFixture(t)
	a, _ := NewAlipay("inst", f.config)
	body := strings.Replace(f.sign(t, basePaidNotification()), "total_amount=12.34", "total_amount=0.01", 1)
	if _, err := a.VerifyNotification(context.Background(), body, nil); err == nil {
		t.Fatal("expected signature failure for tampered amount")
	}
}

func TestAlipayVerifyNotificationRefundEventIsNotPayment(t *testing.T) {
	for _, field := range []string{"gmt_refund", "refund_fee", "out_biz_no"} {
		t.Run(field, func(t *testing.T) {
			f := newAlipayNotifyFixture(t)
			a, _ := NewAlipay("inst", f.config)
			fields := basePaidNotification()
			fields[field] = "1.00"
			n, err := a.VerifyNotification(context.Background(), f.sign(t, fields), nil)
			if err != nil {
				t.Fatalf("verify: %v", err)
			}
			if n.Status == payment.ProviderStatusSuccess {
				t.Fatalf("%s notification must not be treated as payment success", field)
			}
		})
	}
}

func TestAlipayVerifyNotificationSellerID(t *testing.T) {
	f := newAlipayNotifyFixture(t)
	f.config["sellerId"] = "2088000000000001"
	a, _ := NewAlipay("inst", f.config)
	if _, err := a.VerifyNotification(context.Background(), f.sign(t, basePaidNotification()), nil); err != nil {
		t.Fatalf("matching seller_id should pass: %v", err)
	}

	fields := basePaidNotification()
	fields["seller_id"] = "2088999999999999"
	if _, err := a.VerifyNotification(context.Background(), f.sign(t, fields), nil); err == nil {
		t.Fatal("expected seller_id mismatch error")
	}
}

func TestAlipayEnvironmentSelectsGateway(t *testing.T) {
	f := newAlipayNotifyFixture(t)
	cases := map[string]bool{"": false, "production": false, "sandbox": true, " Sandbox ": true}
	for env, wantSandbox := range cases {
		cfg := map[string]string{}
		for k, v := range f.config {
			cfg[k] = v
		}
		cfg["environment"] = env
		a, _ := NewAlipay("inst", cfg)
		if got := a.isSandbox(); got != wantSandbox {
			t.Errorf("environment=%q: isSandbox=%v, want %v", env, got, wantSandbox)
		}
		client, err := a.getClient()
		if err != nil {
			t.Fatalf("environment=%q: client: %v", env, err)
		}
		payURL, err := client.TradePagePay(alipayPagePayParam())
		if err != nil {
			t.Fatalf("page pay url: %v", err)
		}
		wantHost := "openapi.alipay.com"
		if wantSandbox {
			wantHost = "openapi-sandbox.dl.alipaydev.com"
		}
		if payURL.Host != wantHost {
			t.Errorf("environment=%q: host=%s, want %s", env, payURL.Host, wantHost)
		}
	}
}

func alipayPagePayParam() alipay.TradePagePay {
	p := alipay.TradePagePay{}
	p.OutTradeNo = "sub2_order_1"
	p.TotalAmount = "0.01"
	p.Subject = "test"
	p.ProductCode = alipayProductCodePagePay
	return p
}
