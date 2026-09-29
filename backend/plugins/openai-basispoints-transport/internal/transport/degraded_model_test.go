package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInternalDegradedModelNotFound(t *testing.T) {
	body := []byte("{\"model\":\"gpt-6-astra\"}")
	message := "The model `gpt-6-astra-degrade2-luna-1p-codexswic-ev3` does not exist or you do not have access to it."
	require.True(t, isInternalDegradedModelNotFound(404, body, "model_not_found", message))
	require.False(t, isInternalDegradedModelNotFound(404, body, "model_not_found", "The model `gpt-6-astra` does not exist or you do not have access to it."))
	require.False(t, isInternalDegradedModelNotFound(404, body, "model_not_found", "The model `gpt-6-sol-degrade2-luna` does not exist or you do not have access to it."))
	require.False(t, isInternalDegradedModelNotFound(404, body, "model_not_found", "The model `gpt-6-astra-degrade2-luna` is missing from the request."))
	require.False(t, isInternalDegradedModelNotFound(400, body, "model_not_found", message))
}

func TestBPSDegradedModel404Retries(t *testing.T) {
	for _, recoverOnRetry := range []bool{false, true} {
		t.Run(fmt.Sprint("recover=", recoverOnRetry), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			var hits atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				n := hits.Add(1)
				raw, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				var sent struct {
					Model string `json:"model"`
				}
				require.NoError(t, json.Unmarshal(raw, &sent))
				require.Equal(t, "gpt-6-astra", sent.Model)
				w.Header().Set("Content-Type", "application/json")
				if recoverOnRetry && n == 2 {
					io.WriteString(w, "{\"id\":\"resp_ok\",\"status\":\"completed\",\"output\":[]}")
					return
				}
				w.WriteHeader(http.StatusNotFound)
				io.WriteString(w, "{\"error\":{\"code\":\"model_not_found\",\"message\":\"The model `gpt-6-astra-degrade2-luna-1p-codexswic-ev3` does not exist or you do not have access to it.\"}}")
			}))
			defer upstream.Close()
			c := recoveryClient(t, ctx, upstream.URL)
			start, output, failure := forwardForTest(t, c, []byte("{\"model\":\"gpt-6-astra\",\"input\":\"hello\",\"stream\":true}"), ctx)
			require.Nil(t, failure)
			if recoverOnRetry {
				require.Equal(t, int32(2), hits.Load())
				require.Equal(t, int32(http.StatusOK), start.StatusCode)
				require.Contains(t, string(output), "resp_ok")
			} else {
				require.Equal(t, int32(maxDegradedModelRetries+1), hits.Load())
				require.Equal(t, int32(http.StatusServiceUnavailable), start.StatusCode)
				require.Equal(t, "bps_degraded_model_unavailable", jsonErrorCode(t, output))
			}
		})
	}
}

func TestBPSRequestedModel404KeepsOriginalResponse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, "{\"error\":{\"code\":\"model_not_found\",\"message\":\"The model `gpt-6-astra` does not exist or you do not have access to it.\"}}")
	}))
	defer upstream.Close()
	c := recoveryClient(t, ctx, upstream.URL)
	start, output, failure := forwardForTest(t, c, []byte("{\"model\":\"gpt-6-astra\",\"input\":\"hello\",\"stream\":true}"), ctx)
	require.Nil(t, failure)
	require.Equal(t, int32(1), hits.Load())
	require.Equal(t, int32(http.StatusNotFound), start.StatusCode)
	require.Equal(t, "model_not_found", jsonErrorCode(t, output))
}

func jsonErrorCode(t *testing.T, body []byte) string {
	t.Helper()
	var response struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(body, &response))
	return response.Error.Code
}
