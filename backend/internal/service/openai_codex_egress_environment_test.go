package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type codexEgressProbeFunc func(context.Context, string) (*ProxyExitInfo, int64, error)

func (f codexEgressProbeFunc) ProbeProxy(context.Context, string) (*ProxyExitInfo, int64, error) {
	return nil, 0, errors.New("Codex must use the timezone-aware probe")
}

func (f codexEgressProbeFunc) ProbeProxyTimezone(ctx context.Context, url string) (*ProxyExitInfo, int64, error) {
	return f(ctx, url)
}

const testCodexEnvironment = "<environment_context>\n<cwd>/work</cwd>\n<current_date>2000-01-01</current_date>\n<timezone>Asia/Shanghai</timezone>\n</environment_context>"

func codexEgressAccount(proxy *Proxy) *Account {
	a := &Account{ID: 10, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 1,
		Credentials: map[string]any{"access_token": "token", "chatgpt_account_id": "account"}}
	if proxy != nil {
		a.ProxyID, a.Proxy = &proxy.ID, proxy
	}
	return a
}

func codexEgressBody(t *testing.T, content any) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{"model": "gpt-5.2", "stream": true, "instructions": "test", "input": []any{map[string]any{"role": "user", "content": content}}})
	require.NoError(t, err)
	return body
}

func TestCodexEgressEnvironment_RewriteLatestBlock(t *testing.T) {
	now := time.Date(2026, 9, 17, 1, 0, 0, 0, time.UTC)
	var probes int
	svc := &OpenAIGatewayService{codexEgressEnvironment: codexEgressEnvironmentResolver{
		now: func() time.Time { return now },
		prober: codexEgressProbeFunc(func(context.Context, string) (*ProxyExitInfo, int64, error) {
			probes++
			return &ProxyExitInfo{Timezone: "America/Los_Angeles"}, 0, nil
		}),
	}}
	outside := "<current_date>outside</current_date><timezone>outside</timezone>"
	text := outside + testCodexEnvironment + " middle " + testCodexEnvironment + outside
	body := []byte(fmt.Sprintf(`{"large":9007199254740993,"input":[{"role":"user","content":%s},{"type":"message","role":"user","content":[{"type":"input_text","text":%s},{"type":"input_text","text":%s}]}]}`, mustCodexTextJSON(t, testCodexEnvironment), mustCodexTextJSON(t, testCodexEnvironment), mustCodexTextJSON(t, text)))
	original := string(body)
	got, err := svc.rewriteCodexEgressEnvironment(context.Background(), nil, codexEgressAccount(nil), body)
	require.NoError(t, err)
	want := outside + testCodexEnvironment + " middle " + strings.ReplaceAll(strings.ReplaceAll(testCodexEnvironment, "2000-01-01", "2026-09-16"), "Asia/Shanghai", "America/Los_Angeles") + outside
	require.Equal(t, want, gjson.GetBytes(got, "input.1.content.1.text").String())
	require.Equal(t, testCodexEnvironment, gjson.GetBytes(got, "input.0.content").String())
	require.Equal(t, testCodexEnvironment, gjson.GetBytes(got, "input.1.content.0.text").String())
	require.Equal(t, "9007199254740993", gjson.GetBytes(got, "large").Raw)
	require.Equal(t, original, string(body))
	require.Equal(t, 1, probes)

	// A successful cache entry does not freeze the date at the first request.
	now = now.Add(24 * time.Hour)
	got, err = svc.rewriteCodexEgressEnvironment(context.Background(), nil, codexEgressAccount(nil), body)
	require.NoError(t, err)
	require.Contains(t, gjson.GetBytes(got, "input.1.content.1.text").String(), "2026-09-17")
	require.Equal(t, 1, probes)
}

func mustCodexTextJSON(t *testing.T, text string) string {
	t.Helper()
	b, err := json.Marshal(text)
	require.NoError(t, err)
	return string(b)
}

func TestCodexEgressEnvironment_DateAndDST(t *testing.T) {
	for _, tt := range []struct{ zone, utc, date string }{
		{"America/Los_Angeles", "2026-09-17T01:00:00Z", "2026-09-16"},
		{"Asia/Tokyo", "2026-09-17T01:00:00Z", "2026-09-17"},
		{"America/Los_Angeles", "2026-01-17T07:30:00Z", "2026-01-16"},
		{"America/Los_Angeles", "2026-07-17T07:30:00Z", "2026-07-17"},
	} {
		t.Run(tt.zone+tt.utc, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, tt.utc)
			require.NoError(t, err)
			svc := &OpenAIGatewayService{codexEgressEnvironment: codexEgressEnvironmentResolver{now: func() time.Time { return now }, prober: codexEgressProbeFunc(func(context.Context, string) (*ProxyExitInfo, int64, error) {
				return &ProxyExitInfo{Timezone: tt.zone}, 0, nil
			})}}
			// Reversed tag order and string content are supported too.
			body := codexEgressBody(t, "<environment_context><timezone>old</timezone><current_date>old</current_date></environment_context>")
			got, err := svc.rewriteCodexEgressEnvironment(context.Background(), nil, codexEgressAccount(nil), body)
			require.NoError(t, err)
			require.Contains(t, gjson.GetBytes(got, "input.0.content").String(), "<current_date>"+tt.date+"</current_date>")
			require.Contains(t, gjson.GetBytes(got, "input.0.content").String(), "<timezone>"+tt.zone+"</timezone>")
		})
	}
}

func TestCodexEgressEnvironment_SkipsWithoutProbe(t *testing.T) {
	for _, text := range []string{
		"hello",
		"<environment_context>",
		"<environment_context><timezone>old</timezone></environment_context>",
		strings.Replace(testCodexEnvironment, "</current_date>", "", 1),
		strings.Replace(testCodexEnvironment, "<timezone>", "<timezone><timezone>", 1),
		testCodexEnvironment + "<environment_context",
		strings.Replace(testCodexEnvironment, "<current_date>2000-01-01</current_date>", "<current_date><timezone>bad</timezone></current_date>", 1),
	} {
		t.Run(text, func(t *testing.T) {
			svc := &OpenAIGatewayService{codexEgressEnvironment: codexEgressEnvironmentResolver{prober: codexEgressProbeFunc(func(context.Context, string) (*ProxyExitInfo, int64, error) {
				t.Error("unexpected probe")
				return nil, 0, nil
			})}}
			body := []byte(fmt.Sprintf(`{"input":[{"role":"user","content":%s},{"role":"user","content":%s}]}`, mustCodexTextJSON(t, "historical text"), mustCodexTextJSON(t, text)))
			if strings.Contains(text, "<environment_context") {
				body = []byte(fmt.Sprintf(`{"input":[{"role":"user","content":%s},{"role":"user","content":%s}]}`, mustCodexTextJSON(t, testCodexEnvironment), mustCodexTextJSON(t, text)))
			}
			got, err := svc.rewriteCodexEgressEnvironment(context.Background(), nil, codexEgressAccount(nil), body)
			require.NoError(t, err)
			require.Equal(t, body, got)
		})
	}
	for _, kind := range []string{"apikey", "setup-token", "other-platform", "websocket", "assistant", "tool", "invalid-json"} {
		t.Run(kind, func(t *testing.T) {
			svc := &OpenAIGatewayService{codexEgressEnvironment: codexEgressEnvironmentResolver{prober: codexEgressProbeFunc(func(context.Context, string) (*ProxyExitInfo, int64, error) {
				t.Error("unexpected probe")
				return nil, 0, nil
			})}}
			a := codexEgressAccount(nil)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			body := codexEgressBody(t, testCodexEnvironment)
			switch kind {
			case "apikey":
				a.Type = AccountTypeAPIKey
			case "setup-token":
				a.Type = AccountTypeSetupToken
			case "other-platform":
				a.Platform = PlatformAnthropic
			case "websocket":
				SetOpenAIClientTransport(c, OpenAIClientTransportWS)
			case "assistant":
				body = []byte(strings.ReplaceAll(string(body), `"user"`, `"assistant"`))
			case "tool":
				body = []byte(`{"input":[{"type":"function_call_output","role":"user","content":` + mustCodexTextJSON(t, testCodexEnvironment) + `}]}`)
			case "invalid-json":
				body = append(body, 'x')
			}
			got, err := svc.rewriteCodexEgressEnvironment(context.Background(), c, a, body)
			require.NoError(t, err)
			require.Equal(t, body, got)
		})
	}
}

func TestCodexEgressEnvironment_CacheConnectionSettings(t *testing.T) {
	var urls []string
	r := codexEgressEnvironmentResolver{prober: codexEgressProbeFunc(func(_ context.Context, url string) (*ProxyExitInfo, int64, error) {
		urls = append(urls, url)
		return &ProxyExitInfo{Timezone: "Asia/Tokyo"}, 0, nil
	})}
	a := codexEgressAccount(&Proxy{ID: 1, Protocol: "http", Host: "proxy.example", Port: 8080, Username: "u", Password: "p"})
	resolve := func() {
		loc, err := r.resolve(context.Background(), a)
		require.NoError(t, err)
		require.Equal(t, "Asia/Tokyo", loc.String())
	}
	resolve()
	resolve()
	require.Equal(t, []string{a.Proxy.URL()}, urls)
	a.ID++
	a.Proxy.Name = "renamed"
	a.Proxy.UpdatedAt = time.Now()
	resolve()
	require.Len(t, urls, 1)
	for _, mutate := range []func(){func() { a.Proxy.Host = "new.example" }, func() { a.Proxy.Port++ }, func() { a.Proxy.Protocol = "socks5" }, func() { a.Proxy.Username = "new" }, func() { a.Proxy.Password = "new" }} {
		n := len(urls)
		mutate()
		resolve()
		require.Len(t, urls, n+1)
		require.Equal(t, a.Proxy.URL(), urls[n])
	}
	a = codexEgressAccount(&Proxy{ID: 2, Protocol: "http", Host: "other.example", Port: 8080})
	resolve()
	require.Len(t, urls, 7)
	a = codexEgressAccount(nil)
	resolve()
	resolve()
	require.Len(t, urls, 8)
	require.Equal(t, "", urls[7])
	// Missing/mismatched proxy associations must never use direct egress.
	a.ProxyID = new(int64)
	*a.ProxyID = 3
	_, err := r.resolve(context.Background(), a)
	require.Error(t, err)
	require.Len(t, urls, 8)
	a.Proxy = &Proxy{ID: 4}
	_, err = r.resolve(context.Background(), a)
	require.Error(t, err)
	require.Len(t, urls, 8)
}

func TestCodexEgressEnvironment_FailureBackoffAndRecovery(t *testing.T) {
	for _, zone := range []string{"probe-error", "nil-info", "", "Local", "No/Such_Zone"} {
		t.Run(zone, func(t *testing.T) {
			now := time.Now()
			calls := 0
			svc := &OpenAIGatewayService{codexEgressEnvironment: codexEgressEnvironmentResolver{now: func() time.Time { return now }, prober: codexEgressProbeFunc(func(_ context.Context, url string) (*ProxyExitInfo, int64, error) {
				calls++
				require.NotEmpty(t, url)
				if calls > 1 {
					return &ProxyExitInfo{Timezone: "Asia/Tokyo"}, 0, nil
				}
				if zone == "probe-error" {
					return nil, 0, errors.New("secret proxy URL")
				}
				if zone == "nil-info" {
					return nil, 0, nil
				}
				return &ProxyExitInfo{Timezone: zone}, 0, nil
			})}}
			a := codexEgressAccount(&Proxy{ID: 1, Protocol: "http", Host: "proxy.example", Port: 8080})
			body := codexEgressBody(t, testCodexEnvironment)
			for i := 0; i < 2; i++ {
				got, err := svc.rewriteCodexEgressEnvironment(context.Background(), nil, a, body)
				require.NoError(t, err)
				require.Equal(t, body, got)
			}
			require.Equal(t, 1, calls)
			now = now.Add(codexEgressFailureBackoff)
			got, err := svc.rewriteCodexEgressEnvironment(context.Background(), nil, a, body)
			require.NoError(t, err)
			require.Contains(t, gjson.GetBytes(got, "input.0.content").String(), "Asia/Tokyo")
			require.Equal(t, 2, calls)
		})
	}
}

func TestCodexEgressEnvironment_ConcurrentProbeAndCancellation(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	r := codexEgressEnvironmentResolver{prober: codexEgressProbeFunc(func(ctx context.Context, _ string) (*ProxyExitInfo, int64, error) {
		calls.Add(1)
		close(started)
		select {
		case <-release:
			return &ProxyExitInfo{Timezone: "Asia/Tokyo"}, 0, nil
		case <-ctx.Done():
			return nil, 0, ctx.Err()
		}
	})}
	a := codexEgressAccount(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	leader := make(chan error, 1)
	go func() { _, err := r.resolve(ctx, a); leader <- err }()
	<-started
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			loc, err := r.resolve(context.Background(), a)
			if err != nil {
				t.Error(err)
				return
			}
			if loc.String() != "Asia/Tokyo" {
				t.Error(loc)
			}
		}()
	}
	cancel()
	require.ErrorIs(t, <-leader, context.Canceled)
	close(release)
	wg.Wait()
	require.Equal(t, int32(1), calls.Load())
}

func TestCodexEgressEnvironment_OldProbeCannotOverwriteNewSettings(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	r := codexEgressEnvironmentResolver{prober: codexEgressProbeFunc(func(_ context.Context, url string) (*ProxyExitInfo, int64, error) {
		calls.Add(1)
		if strings.Contains(url, "old.example") {
			close(started)
			<-release
			return &ProxyExitInfo{Timezone: "America/Los_Angeles"}, 0, nil
		}
		return &ProxyExitInfo{Timezone: "Asia/Tokyo"}, 0, nil
	})}
	old := codexEgressAccount(&Proxy{ID: 1, Protocol: "http", Host: "old.example", Port: 8080})
	next := codexEgressAccount(&Proxy{ID: 1, Protocol: "http", Host: "new.example", Port: 8080})
	done := make(chan struct{})
	go func() {
		defer close(done)
		loc, err := r.resolve(context.Background(), old)
		if err != nil {
			t.Error(err)
			return
		}
		if loc.String() != "America/Los_Angeles" {
			t.Error(loc)
		}
	}()
	<-started
	loc, err := r.resolve(context.Background(), next)
	require.NoError(t, err)
	require.Equal(t, "Asia/Tokyo", loc.String())
	close(release)
	<-done
	loc, err = r.resolve(context.Background(), next)
	require.NoError(t, err)
	require.Equal(t, "Asia/Tokyo", loc.String())
	require.Equal(t, int32(2), calls.Load())
}

func TestCodexEgressEnvironment_ProbeTimeout(t *testing.T) {
	r := codexEgressEnvironmentResolver{prober: codexEgressProbeFunc(func(ctx context.Context, _ string) (*ProxyExitInfo, int64, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > codexEgressProbeTimeout {
			t.Error("probe must have a bounded deadline")
		}
		<-ctx.Done()
		return nil, 0, ctx.Err()
	})}
	_, err := r.resolve(context.Background(), codexEgressAccount(nil))
	require.EqualError(t, err, "exit probe failed")
}

func TestCodexEgressEnvironment_ForwardFailover(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		t.Run(fmt.Sprint(passthrough), func(t *testing.T) {
			now := time.Date(2026, 9, 17, 1, 0, 0, 0, time.UTC)
			upstream := &httpUpstreamRecorder{}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream, codexEgressEnvironment: codexEgressEnvironmentResolver{now: func() time.Time { return now }, prober: codexEgressProbeFunc(func(_ context.Context, url string) (*ProxyExitInfo, int64, error) {
				zone := "America/Los_Angeles"
				if strings.Contains(url, "japan") {
					zone = "Asia/Tokyo"
				}
				return &ProxyExitInfo{Timezone: zone}, 0, nil
			})}}
			a := codexEgressAccount(&Proxy{ID: 1, Protocol: "http", Host: "us.example", Port: 8080})
			b := codexEgressAccount(&Proxy{ID: 2, Protocol: "http", Host: "japan.example", Port: 8080})
			b.ID = 11
			a.Extra = map[string]any{"openai_passthrough": passthrough}
			b.Extra = map[string]any{"openai_passthrough": passthrough}
			body := codexEgressBody(t, []any{map[string]any{"type": "input_text", "text": testCodexEnvironment}})
			original := string(body)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			c.Request.Header.Set("User-Agent", "codex_cli_rs/0.1.0")
			SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
			upstream.resp = &http.Response{StatusCode: 429, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"rate limited","type":"rate_limit_error"}}`))}
			_, err := svc.Forward(context.Background(), c, a, body)
			var failover *UpstreamFailoverError
			require.ErrorAs(t, err, &failover)
			require.Contains(t, gjson.GetBytes(upstream.lastBody, "input.0.content.0.text").String(), "2026-09-16")
			require.Contains(t, gjson.GetBytes(upstream.lastBody, "input.0.content.0.text").String(), "America/Los_Angeles")
			require.Equal(t, a.Proxy.URL(), upstream.lastProxyURL)
			upstream.resp = &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: [DONE]\n\n"))}
			_, err = svc.Forward(context.Background(), c, b, body)
			require.NoError(t, err)
			require.Contains(t, gjson.GetBytes(upstream.lastBody, "input.0.content.0.text").String(), "2026-09-17")
			require.Contains(t, gjson.GetBytes(upstream.lastBody, "input.0.content.0.text").String(), "Asia/Tokyo")
			require.Equal(t, b.Proxy.URL(), upstream.lastProxyURL)
			require.Equal(t, original, string(body))
		})
	}
}

func TestCodexEgressEnvironment_ForwardProbeFailurePreservesOriginal(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		t.Run(fmt.Sprint(passthrough), func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: [DONE]\n\n"))}}
			var urls []string
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream, codexEgressEnvironment: codexEgressEnvironmentResolver{prober: codexEgressProbeFunc(func(_ context.Context, url string) (*ProxyExitInfo, int64, error) {
				urls = append(urls, url)
				return nil, 0, errors.New("unavailable")
			})}}
			a := codexEgressAccount(&Proxy{ID: 1, Protocol: "http", Host: "proxy.example", Port: 8080})
			a.Extra = map[string]any{"openai_passthrough": passthrough}
			body := codexEgressBody(t, []any{map[string]any{"type": "input_text", "text": testCodexEnvironment}})
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
			_, err := svc.Forward(context.Background(), c, a, body)
			require.NoError(t, err)
			require.Equal(t, testCodexEnvironment, gjson.GetBytes(upstream.lastBody, "input.0.content.0.text").String())
			require.Equal(t, []string{a.Proxy.URL()}, urls)
			require.Len(t, upstream.requests, 1)
		})
	}
}
