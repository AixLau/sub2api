//go:build unit

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMarkHTTPServerDrainingReturnsMaintenanceResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	var requestsHandled int
	r.POST("/responses", func(c *gin.Context) {
		requestsHandled++
		c.Status(http.StatusOK)
	})

	srv := ProvideHTTPServer(ingressTestConfig(), r)

	before := httptest.NewRecorder()
	srv.Handler.ServeHTTP(before, httptest.NewRequest(http.MethodPost, "/responses", nil))
	require.Equal(t, http.StatusOK, before.Code)
	require.Equal(t, 1, requestsHandled)

	require.True(t, MarkHTTPServerDraining(srv))
	require.True(t, MarkHTTPServerDraining(srv), "draining should be idempotent")

	after := httptest.NewRecorder()
	srv.Handler.ServeHTTP(after, httptest.NewRequest(http.MethodPost, "/responses", nil))
	require.Equal(t, http.StatusServiceUnavailable, after.Code)
	require.Equal(t, "30", after.Header().Get("Retry-After"))
	require.Equal(t, "maintenance", after.Header().Get("X-Sub2API-Status"))
	require.Contains(t, after.Header().Get("Content-Type"), "application/json")
	require.Equal(t, 1, requestsHandled, "new requests must not enter the application after draining")

	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(after.Body.Bytes(), &payload))
	require.Equal(t, serverMaintenanceCode, payload.Error.Code)
	require.Contains(t, payload.Error.Message, "正在更新或重启")
}

func TestMarkHTTPServerDrainingUsesHealthPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	srv := ProvideHTTPServer(ingressTestConfig(), r)
	require.True(t, MarkHTTPServerDraining(srv))

	recorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	var payload struct {
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Equal(t, "draining", payload.Status)
}

func TestMarkHTTPServerDrainingRejectsUnmanagedServer(t *testing.T) {
	srv := &http.Server{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})}
	require.False(t, MarkHTTPServerDraining(srv))
	require.False(t, MarkHTTPServerDraining(nil))
}
