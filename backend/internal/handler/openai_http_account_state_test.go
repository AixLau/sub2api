package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIHTTPAccountStateErrorBeforeHeadersUsesHTTP4xx(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, EndpointResponses, nil)

	(&OpenAIGatewayHandler{}).writeHTTPAccountStateError(c, service.HTTPAccountStateUnavailable())

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	require.NotContains(t, recorder.Body.String(), "response.failed")

	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Equal(t, "account_bound_state_unavailable", envelope.Error.Code)
}

func TestOpenAIHTTPAccountStateErrorAfterSSEUsesCodexTerminalFrame(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, EndpointResponses, nil)
	c.Header("Content-Type", "text/event-stream")
	_, err := c.Writer.WriteString("event: response.created\ndata: {}\n\n")
	require.NoError(t, err)

	(&OpenAIGatewayHandler{}).writeHTTPAccountStateError(c, service.HTTPAccountStateUnavailable())

	body := recorder.Body.String()
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, strings.Count(body, "event: response.failed"))
	require.Contains(t, body, `"code":"invalid_prompt"`)
	require.Contains(t, body, `"reason":"account_bound_state_unavailable"`)
	// Codex treats unknown response.failed error codes as retryable. The
	// terminal wire code must remain the known invalid_prompt code instead.
	require.NotContains(t, body, `"code":"account_bound_state_unavailable"`)
}

func TestHTTPAccountStateTransientFailoverKeepsRetryableClassification(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  *service.UpstreamFailoverError
		want bool
	}{
		{"rate_limit", &service.UpstreamFailoverError{StatusCode: http.StatusTooManyRequests}, true},
		{"network_5xx", &service.UpstreamFailoverError{StatusCode: http.StatusBadGateway}, true},
		{"same_account_retry", &service.UpstreamFailoverError{StatusCode: http.StatusBadRequest, RetryableOnSameAccount: true}, true},
		{"credential_state", &service.UpstreamFailoverError{StatusCode: http.StatusBadGateway, Stage: service.GatewayFailureStageAccountAuth}, false},
		{"deterministic_400", &service.UpstreamFailoverError{StatusCode: http.StatusBadRequest}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := httpAccountStateTransientFailover(tc.err); got != tc.want {
				t.Fatalf("transient=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestHTTPAccountStateWriterRecordsOutputOwnershipBeforeFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Header("Content-Type", "application/json")
	var owner int64
	var keys []string
	writer := &httpAccountStateWriter{
		ResponseWriter: c.Writer,
		record: func(accountID int64, stateKeys []string) error {
			owner = accountID
			keys = append(keys, stateKeys...)
			return nil
		},
	}
	writer.selectAccount(17)
	_, err := writer.Write([]byte(`{"id":"resp_1","output":[{"type":"reasoning","encrypted_content":"opaque"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	_, published := writer.state()
	if !published || owner != 17 || len(keys) != 1 {
		t.Fatalf("published=%v owner=%d keys=%d, want published=true owner=17 keys=1", published, owner, len(keys))
	}
}
