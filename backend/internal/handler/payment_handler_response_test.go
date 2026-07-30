//go:build unit

package handler

import (
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
)

func TestSanitizePaymentOrderForResponseIncludesProviderKey(t *testing.T) {
	t.Parallel()

	providerKey := payment.TypeHaozPay
	result := sanitizePaymentOrderForResponse(&dbent.PaymentOrder{
		PaymentType: payment.TypeAlipay,
		ProviderKey: &providerKey,
	})

	if result == nil {
		t.Fatal("sanitizePaymentOrderForResponse() returned nil")
	}
	if result.PaymentType != payment.TypeAlipay {
		t.Fatalf("payment_type = %q, want %q", result.PaymentType, payment.TypeAlipay)
	}
	if result.ProviderKey == nil || *result.ProviderKey != payment.TypeHaozPay {
		t.Fatalf("provider_key = %v, want %q", result.ProviderKey, payment.TypeHaozPay)
	}
}
