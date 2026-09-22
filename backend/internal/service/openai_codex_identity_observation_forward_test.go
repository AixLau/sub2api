package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
)

func TestCodexIdentityHTTPObservationSurvivesAccountAttemptsAcrossEpoch(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		t.Run(fmt.Sprintf("passthrough=%v", passthrough), func(t *testing.T) {
			server := miniredis.RunT(t)
			svc := newCodexPeriodRedisService(t, server)
			account := newTestOAuthAccount(7640, map[string]any{codexFingerprintModeExtraKey: "session", "openai_passthrough": passthrough})
			account.Concurrency = 1
			account.Credentials = map[string]any{"access_token": "test", "chatgpt_account_id": "request-time-account"}
			failoverAccount := newTestOAuthAccount(7641, map[string]any{codexFingerprintModeExtraKey: "session", "openai_passthrough": passthrough})
			failoverAccount.Concurrency = 1
			failoverAccount.Credentials = map[string]any{"access_token": "test", "chatgpt_account_id": "request-time-failover"}
			seed, _ := codexFingerprintSeed(account.Extra)
			current := resolveCodexSessionPeriod(seed, "user:1", codexSessionIdentityUpstreamScope(account), time.Now())
			boundary := current.expiresAt.Add(-codexSessionPeriodGrace)
			observedAt := boundary.Add(-time.Second)
			c, body := codexPeriodInput(t, 1, 11, "request-root", "request-thread", "", "request-root")
			captureCodexIdentityObservedAt(c, func() time.Time { return observedAt })
			var first codexPeriodOutbound
			for i, selected := range []*Account{account, failoverAccount, account} {
				upstream := &httpUpstreamRecorder{resp: &http.Response{
					StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}},
					Body: io.NopCloser(strings.NewReader("data: [DONE]\n\n")),
				}}
				svc.httpUpstream = upstream
				_, err := svc.Forward(context.Background(), c, selected, body)
				require.NoError(t, err)
				outbound := checkCodexPeriodOutbound(t, upstream.lastReq.Header, upstream.lastBody)
				selectedSeed, _ := codexFingerprintSeed(selected.Extra)
				period := resolveCodexSessionPeriod(selectedSeed, "user:1", codexSessionIdentityUpstreamScope(selected), observedAt)
				mapped, err := svc.cache.(codexSessionIdentityStore).GetCodexSessionIdentity(context.Background(), period.key)
				require.NoError(t, err, "all account attempts select the ingress epoch")
				require.Equal(t, mapped, outbound.session)
				historyKey := codexHTTPThreadKey("thread-history", "user:1", codexSessionIdentityUpstreamScope(selected), "", "request-thread")
				raw, err := svc.cache.(codexSessionIdentityStore).GetCodexSessionIdentity(context.Background(), historyKey)
				require.NoError(t, err)
				history, err := decodeCodexHTTPThreadHistory(raw)
				require.NoError(t, err)
				require.Equal(t, observedAt.UnixMilli(), history.ObservedAtMs)
				if i == 0 {
					first = outbound
				} else if i == 2 {
					require.Equal(t, first.session, outbound.session)
					require.Equal(t, first.thread, outbound.thread)
				}
			}
			newRequest, newBody := codexPeriodInput(t, 1, 12, "request-root", "request-thread", "", "request-root")
			captureCodexIdentityObservedAt(newRequest, func() time.Time { return boundary.Add(time.Second) })
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: [DONE]\n\n"))}}
			svc.httpUpstream = upstream
			_, err := svc.Forward(context.Background(), newRequest, account, newBody)
			require.NoError(t, err)
			next := checkCodexPeriodOutbound(t, upstream.lastReq.Header, upstream.lastBody)
			require.NotEqual(t, first.session, next.session)
			require.NotEqual(t, first.thread, next.thread)
			require.NotEqual(t, first.cache, next.cache)
		})
	}
}
