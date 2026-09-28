package securityaudit

import (
	"math/rand/v2"
	"time"
)

// Retry policy belongs to the audit error code, regardless of HTTP status or
// a scanner's Retryable hint. Cancellation and deadlines are checked by callers.
func shouldRetryPromptGuard(err error) bool {
	return err != nil && guardErrorCode(err) == ErrorCodeUnavailable
}

func promptGuardRetryDelay() time.Duration {
	return time.Duration(100+rand.IntN(201)) * time.Millisecond
}
