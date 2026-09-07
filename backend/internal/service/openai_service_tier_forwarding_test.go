package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIForwardingModesPreserveObservedTierForBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"native", "passthrough", "chat", "messages"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", mode, stream), func(t *testing.T) {
				const sse = "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_billing\",\"model\":\"gpt-5.5\",\"status\":\"completed\",\"service_tier\":\"default\",\"output\":[],\"usage\":{\"input_tokens\":100,\"output_tokens\":1}}}\n\n"
				upstream := &httpUpstreamRecorder{resp: &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
					Body:       io.NopCloser(strings.NewReader(sse)),
				}}
				svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
				account := &Account{
					ID: 89, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
					Credentials: map[string]any{"api_key": "test-key"},
					Extra:       map[string]any{"openai_passthrough": mode == "passthrough"},
				}
				body := []byte(fmt.Sprintf(`{"model":"gpt-5.5","input":"hello","messages":[{"role":"user","content":"hello"}],"max_tokens":16,"service_tier":"priority","stream":%t}`, stream))
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
				if mode == "messages" {
					c.Request.Header.Set("anthropic-beta", claude.BetaFastMode)
				}
				var result *OpenAIForwardResult
				var err error
				switch mode {
				case "chat":
					result, err = svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
				case "messages":
					result, err = svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
				default:
					result, err = svc.Forward(context.Background(), c, account, body)
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, "priority", optionalStringValue(result.ServiceTier))
				require.Equal(t, "default", result.UpstreamResponseServiceTier)
				require.True(t, ApplyOpenAIServiceTierBillingResolution(account, result).Downgraded)
				require.Equal(t, "default", optionalStringValue(result.ServiceTier))
			})
		}
	}
}
