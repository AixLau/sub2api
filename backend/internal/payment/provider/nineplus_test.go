//go:build unit

package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestNinePlusQueryOrderUsesPaymentPageStatusBeforeOrderInfo(t *testing.T) {
	var checkedStatus bool
	var queriedOrderInfo bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/payApi/common/checkOrderStatus.html":
			require.Equal(t, http.MethodPost, r.Method)
			require.NoError(t, r.ParseForm())
			require.Equal(t, "9PPAID", r.Form.Get("trade_no"))
			checkedStatus = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":1,"msg":"success","data":{"url":"/order/result/9PPAID"}}`))
		case "/shopApi/Order/info":
			require.Equal(t, http.MethodPost, r.Method)
			var payload map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
			require.Equal(t, "9PPAID", payload["trade_no"])
			_, hasQueryPassword := payload["query_password"]
			require.False(t, hasQueryPassword)
			queriedOrderInfo = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":1,"msg":"success","data":{"trade_no":"9PPAID","total_amount":5.1,"status":1,"success_time":1781724711,"sendout":1}}`))
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

	resp, err := n.QueryOrder(context.Background(), "9PPAID")
	require.NoError(t, err)
	require.True(t, checkedStatus)
	require.True(t, queriedOrderInfo)
	require.Equal(t, "9PPAID", resp.TradeNo)
	require.Equal(t, payment.ProviderStatusPaid, resp.Status)
	require.Equal(t, 5.1, resp.Amount)
	require.Equal(t, "1", resp.Metadata["sendout"])
}

func TestNinePlusQueryOrderSkipsOrderInfoWhenPaymentPageStatusIsUnpaid(t *testing.T) {
	var queriedOrderInfo bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/payApi/common/checkOrderStatus.html":
			require.Equal(t, http.MethodPost, r.Method)
			require.NoError(t, r.ParseForm())
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"msg":"not pay","data":null}`))
		case "/shopApi/Order/info":
			queriedOrderInfo = true
			http.NotFound(w, r)
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

	resp, err := n.QueryOrder(context.Background(), "9PUNPAID")
	require.NoError(t, err)
	require.False(t, queriedOrderInfo)
	require.Equal(t, "9PUNPAID", resp.TradeNo)
	require.Equal(t, payment.ProviderStatusPending, resp.Status)
	require.Zero(t, resp.Amount)
}

func TestNinePlusPostFormEncodesPaymentStatusLikeCheckoutPage(t *testing.T) {
	var rawBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/payApi/common/checkOrderStatus.html", r.URL.Path)
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		rawBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"not pay","data":null}`))
	}))
	t.Cleanup(server.Close)

	n, err := NewNinePlus("np-1", map[string]string{
		"apiBase":        server.URL,
		"shopToken":      "shop-token",
		"defaultContact": "fallback@example.com",
	})
	require.NoError(t, err)

	_, err = n.QueryOrder(context.Background(), "9P FORM")
	require.NoError(t, err)
	require.Equal(t, url.Values{"trade_no": []string{"9P FORM"}}.Encode(), rawBody)
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
