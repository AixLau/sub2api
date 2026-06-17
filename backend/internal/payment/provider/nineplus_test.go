//go:build unit

package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestParseNinePlusPaymentPageRemainingSeconds(t *testing.T) {
	t.Parallel()

	remaining, ok := parseNinePlusPaymentPageRemainingSeconds([]byte(`
		<script>
			var maxtime = 600 - (parseInt('63'));
		</script>
	`))

	require.True(t, ok)
	require.Equal(t, 537, remaining)
}

func TestNinePlusCreatePaymentUsesPaymentPageExpiry(t *testing.T) {
	var createPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/shopApi/Pay/order":
			require.NoError(t, json.NewDecoder(r.Body).Decode(&createPayload))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":1,"msg":"ok","data":{"trade_no":"9PTEST","total_amount":10.2,"payurl":"/payApi/Zhifutong/pay.html?trade_no=9PTEST"}}`))
		case "/payApi/Zhifutong/pay.html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<script>var maxtime = 600 - (parseInt('75'));</script>`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	n, err := NewNinePlus("np-1", map[string]string{
		"apiBase":        server.URL,
		"shopToken":      "shop-token",
		"defaultContact": "fallback@example.com",
	})
	require.NoError(t, err)

	start := time.Now()
	resp, err := n.CreatePayment(context.Background(), payment.CreatePaymentRequest{
		OrderID: "sub2_np",
		Subject: "np-product",
		Metadata: map[string]string{
			"contact": "buyer@example.com",
		},
	})
	require.NoError(t, err)

	require.Equal(t, "buyer@example.com", createPayload["contact"])
	require.Equal(t, "buyer@example.com", createPayload["query_password"])
	require.NotNil(t, resp.ExpiresAt)
	require.WithinDuration(t, start.Add(525*time.Second), *resp.ExpiresAt, 2*time.Second)
}
