//go:build unit

package service

import (
	"context"
	"errors"
	"net"
	"os"
	"syscall"
	"testing"
)

// TestClassifyUpstreamTransportError pins which transport-level upstream failures
// are "persistent" (retrying the same proxy/account is pointless — evict + alert)
// versus "transient" (a blip — fail over to a healthy account but do not evict).
//
// The motivating incident: a SOCKS5 proxy whose credentials expired returned
// `username/password authentication failed`, yet the account kept being scheduled.
func TestClassifyUpstreamTransportError(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		persistent bool
	}{
		// Durable — config/credential/routing problems. Retrying same proxy won't help.
		{"socks5 proxy credential rejected", errors.New(`Post "https://chatgpt.com/backend-api/codex/responses": socks connect tcp 85.255.176.68:12324->chatgpt.com:443: username/password authentication failed`), true},
		{"proxy connection refused", errors.New(`proxyconnect tcp: dial tcp 1.2.3.4:1080: connect: connection refused`), true},
		{"no route to host", errors.New(`dial tcp 1.2.3.4:443: connect: no route to host`), true},
		{"dns resolution failure", errors.New(`dial tcp: lookup proxy.example.com: no such host`), true},
		{"network unreachable", errors.New(`dial tcp 1.2.3.4:443: connect: network is unreachable`), true},

		// 407 is not persistent by itself; it needs consecutive failures (see below).
		{"proxy 407 alone", errors.New(`Post "https://chatgpt.com/backend-api/codex/responses": Proxy Authentication Required`), false},

		// Transient — a temporary blip. Fail over, but do NOT evict the account.
		{"client timeout", errors.New(`Post "https://chatgpt.com/...": context deadline exceeded (Client.Timeout exceeded while awaiting headers)`), false},
		{"i/o timeout", errors.New(`dial tcp 1.2.3.4:443: i/o timeout`), false},
		{"connection reset by peer", errors.New(`read tcp 10.0.0.1:5->2.2.2.2:443: read: connection reset by peer`), false},
		{"unexpected eof", errors.New(`unexpected EOF`), false},
		{"broken pipe", errors.New(`write tcp 10.0.0.1:5->2.2.2.2:443: write: broken pipe`), false},

		{"nil error", nil, false},

		// ── Typed-error cases ──────────────────────────────────────────────
		// ECONNREFUSED wrapped in the canonical net.OpError shape Go produces.
		{
			"ECONNREFUSED via net.OpError",
			&net.OpError{
				Op:  "dial",
				Net: "tcp",
				Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED},
			},
			true,
		},
		// Bare syscall error (errors.Is traverses the chain).
		{"ECONNREFUSED bare", syscall.ECONNREFUSED, true},
		{"EHOSTUNREACH bare", syscall.EHOSTUNREACH, true},
		{"ENETUNREACH bare", syscall.ENETUNREACH, true},

		// *net.DNSError with IsNotFound — permanent DNS lookup failure.
		{
			"DNS not found (IsNotFound=true)",
			&net.DNSError{Err: "no such host", Name: "proxy.example.com", IsNotFound: true},
			true,
		},
		// *net.DNSError with IsNotFound=false — transient DNS timeout (not persistent).
		{
			"DNS timeout (IsNotFound=false)",
			&net.DNSError{Err: "i/o timeout", Name: "proxy.example.com", IsTimeout: true},
			false,
		},

		// context.Canceled — client gone; NOT classified as persistent.
		{"context.Canceled", context.Canceled, false},
		// context.DeadlineExceeded — slow upstream; NOT persistent.
		{"context.DeadlineExceeded", context.DeadlineExceeded, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyUpstreamTransportError(tc.err).Persistent
			if got != tc.persistent {
				t.Fatalf("classifyUpstreamTransportError(%q).Persistent = %v, want %v", errString(tc.err), got, tc.persistent)
			}
		})
	}
}

func errString(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

func TestClassifyUpstreamTransportError_ProxyAuthRequired(t *testing.T) {
	got := classifyUpstreamTransportError(errors.New(`Post "https://chatgpt.com/x": Proxy Authentication Required`))
	if got.Persistent || !got.ProxyAuthRequired {
		t.Fatalf("407 class = %+v, want ProxyAuthRequired only", got)
	}
}

// 407 只有连续 3 次（中间无成功）才临时下线账号；成功会清零。
func TestHandleOpenAIUpstreamTransportError_ProxyAuthNeedsConsecutiveFailures(t *testing.T) {
	err407 := errors.New(`Post "https://chatgpt.com/x": Proxy Authentication Required`)
	account := &Account{ID: 494, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	svc := &OpenAIGatewayService{}
	until := func() bool { _, ok := svc.openaiProxyAuthFailures.Load(account.ID); return !ok }

	for i := 1; i < openAIProxyAuthFailureThreshold; i++ {
		var failover *UpstreamFailoverError
		if e := svc.handleOpenAIUpstreamTransportError(context.Background(), newTransportErrorTestGin(t), account, err407, false); !errors.As(e, &failover) {
			t.Fatalf("attempt %d: want failover error, got %v", i, e)
		}
		if until() {
			t.Fatalf("attempt %d: counter should be kept", i)
		}
	}
	if svc.isOpenAIAccountRuntimeBlocked(account) {
		t.Fatal("account must stay schedulable before threshold")
	}

	// 成功请求清零。
	svc.ReportOpenAIAccountScheduleResult(account, "gpt-5", true, nil)
	if !until() {
		t.Fatal("success must reset the 407 counter")
	}

	// 重新累计到阈值：第 3 次触发下线并清零计数。
	for i := 1; i <= openAIProxyAuthFailureThreshold; i++ {
		_ = svc.handleOpenAIUpstreamTransportError(context.Background(), newTransportErrorTestGin(t), account, err407, false)
	}
	if !until() {
		t.Fatal("counter must be cleared after unscheduling")
	}
	if !svc.isOpenAIAccountRuntimeBlocked(account) {
		t.Fatal("account must be blocked after consecutive 407 threshold")
	}
}
