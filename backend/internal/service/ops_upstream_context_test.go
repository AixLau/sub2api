package service

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newOpsTimingTestContext() *gin.Context {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/test", nil)
	return c
}

func TestDoOpsUpstreamRecordsWriteAndResponseHeaderTimings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := newOpsTimingTestContext()
	req, err := http.NewRequest(http.MethodPost, server.URL, nil)
	require.NoError(t, err)
	resp, err := DoOpsUpstream(c, req, func(req *http.Request) (*http.Response, error) {
		return http.DefaultClient.Do(req)
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NoError(t, resp.Body.Close())

	require.NotNil(t, OpsLatencyMs(c, OpsUpstreamRequestWriteMsKey))
	require.NotNil(t, OpsLatencyMs(c, OpsUpstreamResponseHeadersMsKey))
	require.NotNil(t, OpsLatencyMs(c, OpsUpstreamLatencyMsKey))
	_, ok := c.Get(OpsUpstreamStartAtKey)
	require.True(t, ok)
}

func TestDoOpsUpstreamLeavesResponseHeaderTimingUnsetOnTransportError(t *testing.T) {
	c := newOpsTimingTestContext()
	req := httptest.NewRequest(http.MethodPost, "http://upstream.invalid", nil)
	wantErr := errors.New("transport failed")

	resp, err := DoOpsUpstream(c, req, func(*http.Request) (*http.Response, error) {
		return nil, wantErr
	})
	require.ErrorIs(t, err, wantErr)
	require.Nil(t, resp)
	require.Nil(t, OpsLatencyMs(c, OpsUpstreamResponseHeadersMsKey))
	require.NotNil(t, OpsLatencyMs(c, OpsUpstreamLatencyMsKey))
}
