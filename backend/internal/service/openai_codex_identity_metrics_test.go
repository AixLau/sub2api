package service

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexIdentityMetricsBoundedLabelsAndPrivateData(t *testing.T) {
	m := newCodexIdentityMetrics()
	m.recordEvent("thread_history", "stale_rejected")
	m.recordEvent("raw-session-secret", "raw-thread-secret")
	response := httptest.NewRecorder()
	m.handler().ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
	require.Equal(t, 200, response.Code)
	body := response.Body.String()
	require.Contains(t, body, `sub2api_codex_identity_events_total{operation="thread_history",result="stale_rejected"} 1`)
	require.Contains(t, body, `sub2api_codex_identity_events_total{operation="other",result="other"} 1`)
	for _, forbidden := range []string{"raw-session", "raw-thread", "session_id=", "thread_id=", "user_id=", "account_id=", "prompt="} {
		require.NotContains(t, body, forbidden)
	}
	other := httptest.NewRecorder()
	newCodexIdentityMetrics().handler().ServeHTTP(other, httptest.NewRequest("GET", "/metrics", nil))
	require.NotContains(t, other.Body.String(), "stale_rejected")
}

func TestCodexIdentityMetricsPollOwnershipOnlyOnScrape(t *testing.T) {
	m := newCodexIdentityMetrics()
	calls := 0
	counts := map[string]int64{"thread_history": 1000, "side_session": 200, "private-raw-key": 3}
	var storeErr error
	m.keyCountProvider = func(ctx context.Context) (map[string]int64, error) {
		calls++
		_, hasDeadline := ctx.Deadline()
		require.True(t, hasDeadline)
		return counts, storeErr
	}
	m.recordEvent("side_session", "reused")
	require.Zero(t, calls, "normal request events cannot query Redis counts")
	scrape := func() string {
		response := httptest.NewRecorder()
		m.handler().ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
		require.Equal(t, 200, response.Code)
		return response.Body.String()
	}
	body := scrape()
	require.Equal(t, 1, calls)
	require.Contains(t, body, `sub2api_codex_identity_indexed_keys{kind="thread_history"} 1000`)
	require.Contains(t, body, `sub2api_codex_identity_indexed_keys{kind="side_session"} 200`)
	require.Contains(t, body, `sub2api_codex_identity_indexed_keys{kind="other"} 3`)
	require.Contains(t, body, "sub2api_codex_identity_key_counts_available 1")
	require.NotContains(t, body, "private-raw-key")

	counts = map[string]int64{"thread_history": 999, "side_session": 0}
	body = scrape()
	require.Contains(t, body, `sub2api_codex_identity_indexed_keys{kind="thread_history"} 999`)
	require.Contains(t, body, `sub2api_codex_identity_indexed_keys{kind="side_session"} 0`)
	require.NotContains(t, body, `kind="other"`)

	storeErr = errors.New("private Redis error")
	body = scrape()
	require.Contains(t, body, "sub2api_codex_identity_key_counts_available 0")
	require.Contains(t, body, `sub2api_codex_identity_events_total{operation="identity_store",result="error"} 1`)
	require.False(t, strings.Contains(body, storeErr.Error()))
	require.Contains(t, body, `sub2api_codex_identity_indexed_keys{kind="thread_history"} 999`, "failed refresh retains last values with availability=0")
}
