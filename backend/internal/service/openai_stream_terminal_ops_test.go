package service

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIStreamTerminalOverloadRecordsUpstreamAfterOutput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, passthrough := range []bool{false, true} {
		for _, bareError := range []bool{false, true} {
			name := "native"
			if passthrough {
				name = "passthrough"
			}
			if bareError {
				name += "/error_then_failed"
			}
			t.Run(name, func(t *testing.T) {
				const message = "Our servers are currently overloaded. Please try again later."
				stream := "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n"
				if bareError {
					stream += "event: error\ndata: {\"type\":\"error\",\"error\":{\"code\":\"server_error\",\"message\":\"" + message + "\"}}\n\n"
				}
				stream += "event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_terminal\",\"status\":\"failed\",\"error\":{\"code\":\"server_error\",\"message\":\"" + message + "\"}}}\n\n"
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				resp := &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"X-Request-Id": []string{"upstream-terminal"}},
					Body:       io.NopCloser(strings.NewReader(stream)),
				}
				svc := &OpenAIGatewayService{cfg: &config.Config{}}
				account := &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
				var err error
				if passthrough {
					_, err = svc.handleStreamingResponsePassthrough(c.Request.Context(), resp, c, account, time.Now(), "gpt-5.5", "gpt-5.5")
				} else {
					_, err = svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "gpt-5.5", "gpt-5.5")
				}
				require.ErrorContains(t, err, message)
				var failover *UpstreamFailoverError
				require.False(t, errors.As(err, &failover), "partial output must never be replayed")
				require.Equal(t, http.StatusOK, recorder.Code)
				require.Contains(t, recorder.Body.String(), "partial")
				require.Equal(t, 1, strings.Count(recorder.Body.String(), "event: response.failed"))
				value, exists := c.Get(OpsUpstreamErrorsKey)
				require.True(t, exists, "the provider failure must be available to ops attribution")
				events, ok := value.([]*OpsUpstreamErrorEvent)
				require.True(t, ok)
				require.Len(t, events, 1, "error + response.failed is one terminal failure")
				require.Equal(t, account.ID, events[0].AccountID)
				require.Equal(t, http.StatusServiceUnavailable, events[0].UpstreamStatusCode)
				require.Equal(t, "upstream-terminal", events[0].UpstreamRequestID)
				require.Equal(t, message, events[0].Message)
				require.Equal(t, passthrough, events[0].Passthrough)
			})
		}
	}
}
