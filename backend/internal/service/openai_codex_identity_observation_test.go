package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCodexIdentityObservationCapturedOncePerRequest(t *testing.T) {
	first, _ := gin.CreateTestContext(httptest.NewRecorder())
	first.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	before := time.Date(2026, 9, 22, 1, 2, 3, 0, time.UTC)
	after := before.Add(7 * 24 * time.Hour)
	calls := 0
	clock := func() time.Time {
		calls++
		if calls == 1 {
			return before
		}
		return after
	}
	require.Equal(t, before, captureCodexIdentityObservedAt(first, clock))
	// Simulate queueing and failover on the same gin context. The clock must
	// not even be sampled again, regardless of how long either attempt takes.
	for range 3 {
		require.Equal(t, before, captureCodexIdentityObservedAt(first, clock))
	}
	require.Equal(t, 1, calls)
	require.Equal(t, before, CaptureCodexIdentityObservedAt(first))

	second, _ := gin.CreateTestContext(httptest.NewRecorder())
	second.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	require.Equal(t, after, captureCodexIdentityObservedAt(second, clock))
	require.Equal(t, 2, calls)
}
