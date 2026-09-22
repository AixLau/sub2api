package service

import (
	"time"

	"github.com/gin-gonic/gin"
)

const codexIdentityObservedAtContextKey = "codex_identity_observed_at"

// CaptureCodexIdentityObservedAt records the server's first observation of an
// HTTP request. Selection, queueing and all subsequent account attempts share
// this clock; client turn timestamps never participate in identity ordering.
func CaptureCodexIdentityObservedAt(c *gin.Context) time.Time {
	return captureCodexIdentityObservedAt(c, time.Now)
}

func captureCodexIdentityObservedAt(c *gin.Context, clock func() time.Time) time.Time {
	if c != nil {
		if value, ok := c.Get(codexIdentityObservedAtContextKey); ok {
			if observedAt, ok := value.(time.Time); ok && !observedAt.IsZero() {
				return observedAt
			}
		}
	}
	observedAt := clock()
	if c != nil {
		c.Set(codexIdentityObservedAtContextKey, observedAt)
	}
	return observedAt
}
