package transport

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	pluginconfig "github.com/Wei-Shaw/sub2api/plugins/openai-basispoints-transport/internal/config"
)

func TestResolveTargetMapsResponsesPaths(t *testing.T) {
	cfg := pluginconfig.Defaults()
	tests := []struct {
		incoming string
		want     string
	}{
		{"https://chatgpt.com/backend-api/codex/responses", "https://bps.openai.com/basispoints/api/responses"},
		{"https://api.openai.com/v1/responses/compact?x=1", "https://bps.openai.com/basispoints/api/responses/compact?x=1"},
	}
	for _, test := range tests {
		got, err := resolveTarget(test.incoming, cfg)
		if err != nil || got.String() != test.want {
			t.Fatalf("resolveTarget(%q): got %v, err %v; want %q", test.incoming, got, err, test.want)
		}
	}
}

func TestResolveTargetUsesExplicitDevelopmentServer(t *testing.T) {
	cfg := pluginconfig.Defaults()
	cfg.UpstreamBaseURL = "http://127.0.0.1:4242"
	got, err := resolveTarget("https://chatgpt.com/backend-api/codex/responses", cfg)
	if err != nil || got.String() != "http://127.0.0.1:4242/responses" {
		t.Fatalf("local target: got %v, err %v", got, err)
	}
}

func TestBuildRequestUsesHostIdentityAndBasisPointsHeaders(t *testing.T) {
	cfg := pluginconfig.Defaults()
	target, err := url.Parse(cfg.UpstreamBaseURL + "/responses")
	if err != nil {
		t.Fatal(err)
	}
	start := &pluginv1.ForwardRequestStart{
		Method:      http.MethodPost,
		Url:         "https://chatgpt.com/backend-api/codex/responses",
		AccountId:   7,
		Platform:    "openai",
		AccountType: "oauth",
		HasBody:     true,
		Headers: map[string]*pluginv1.HeaderValues{
			"Content-Type":  {Values: []string{"application/json"}},
			"Authorization": {Values: []string{"Bearer client-value"}},
			"User-Agent":    {Values: []string{"Mozilla/5.0 ExcelWebView/1.0"}},
			"Originator":    {Values: []string{"codex-tui"}},
			"Version":       {Values: []string{"0.158.0"}},
			"OpenAI-Beta":   {Values: []string{"responses=experimental"}},
		},
	}
	p := New()
	request, err := p.buildRequest(context.Background(), start, target, cfg, outboundIdentity{
		token: "host-token",
		headers: http.Header{
			"chatgpt-account-id": []string{"acct-own"},
		},
	}, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	if got := request.Header.Get("Authorization"); got != "Bearer host-token" {
		t.Fatalf("authorization was not replaced: %q", got)
	}
	if got := request.Header.Get("x-openai-account-id"); got != "acct-own" {
		t.Fatalf("account id header: %q", got)
	}
	if got := request.Header.Get("x-basispoints-auth-mode"); got != "chatgpt" {
		t.Fatalf("basispoints auth mode: %q", got)
	}
	if strings.Contains(request.Header.Get("Authorization"), "client-value") {
		t.Fatal("client authorization leaked into the upstream request")
	}
	if got := request.Header.Get("User-Agent"); got != "Mozilla/5.0 ExcelWebView/1.0" {
		t.Fatalf("BPS user-agent was rewritten: %q", got)
	}
	for _, key := range []string{"Originator", "Version", "OpenAI-Beta"} {
		if got := request.Header.Get(key); got != "" {
			t.Fatalf("BPS request retained Codex header %s=%q", key, got)
		}
	}
	if got := request.Header.Get("x-openai-internal-basispoints-client-product"); got != "basispoints-excel-plugin" {
		t.Fatalf("BPS client profile: %q", got)
	}
}

func TestBuildRequestBPSHeaderDefaults(t *testing.T) {
	tests := []struct {
		name                           string
		incoming, host, extra          map[string]string
		wantUA, wantOrigin             string
		wantNativeUA, wantNativeOrigin string
	}{
		{name: "missing", wantUA: "Mozilla/5.0", wantOrigin: "https://bps.openai.com"},
		{name: "empty", incoming: map[string]string{"User-Agent": "", "Origin": ""}, wantUA: "Mozilla/5.0", wantOrigin: "https://bps.openai.com"},
		{name: "whitespace", incoming: map[string]string{"User-Agent": " \t", "Origin": " \t"}, wantUA: "Mozilla/5.0", wantOrigin: "https://bps.openai.com", wantNativeUA: " \t", wantNativeOrigin: " \t"},
		{name: "caller values", incoming: map[string]string{"User-Agent": "excel-client/1.0", "Origin": "https://chatgpt.com"}, wantUA: "excel-client/1.0", wantOrigin: "https://chatgpt.com", wantNativeUA: "excel-client/1.0", wantNativeOrigin: "https://chatgpt.com"},
		{name: "only origin missing", incoming: map[string]string{"User-Agent": "excel-client/1.0"}, wantUA: "excel-client/1.0", wantOrigin: "https://bps.openai.com", wantNativeUA: "excel-client/1.0"},
		{name: "only UA missing", incoming: map[string]string{"Origin": "https://chatgpt.com"}, wantUA: "Mozilla/5.0", wantOrigin: "https://chatgpt.com", wantNativeOrigin: "https://chatgpt.com"},
		{name: "host values", host: map[string]string{"User-Agent": "host-client/1.0", "Origin": "https://chatgpt.com"}, wantUA: "host-client/1.0", wantOrigin: "https://chatgpt.com", wantNativeUA: "host-client/1.0", wantNativeOrigin: "https://chatgpt.com"},
		{name: "explicit config", incoming: map[string]string{"User-Agent": "caller-client/1.0", "Origin": "https://chatgpt.com"}, extra: map[string]string{"User-Agent": "configured-client/1.0", "Origin": "https://bps.openai.com"}, wantUA: "configured-client/1.0", wantOrigin: "https://bps.openai.com", wantNativeUA: "caller-client/1.0", wantNativeOrigin: "https://chatgpt.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := pluginconfig.Defaults()
			cfg.ExtraHeaders = tt.extra
			start := &pluginv1.ForwardRequestStart{Method: http.MethodPost, Headers: map[string]*pluginv1.HeaderValues{}}
			for key, value := range tt.incoming {
				start.Headers[key] = &pluginv1.HeaderValues{Values: []string{value}}
			}
			identity := outboundIdentity{token: "test-token", headers: make(http.Header)}
			identity.headers.Set("Chatgpt-Account-Id", "test-account")
			for key, value := range tt.host {
				identity.headers.Set(key, value)
			}
			p := New()
			target, err := url.Parse(cfg.UpstreamBaseURL + "/responses")
			if err != nil {
				t.Fatal(err)
			}
			bps, err := p.buildRequest(context.Background(), start, target, cfg, identity, http.NoBody)
			if err != nil {
				t.Fatal(err)
			}
			nativeTarget, err := url.Parse("https://chatgpt.com/backend-api/codex/responses")
			if err != nil {
				t.Fatal(err)
			}
			native, err := p.buildOutboundRequest(context.Background(), start, nativeTarget, cfg, identity, http.NoBody)
			if err != nil {
				t.Fatal(err)
			}
			for _, check := range []struct {
				request    *http.Request
				ua, origin string
			}{
				{bps, tt.wantUA, tt.wantOrigin},
				{native, tt.wantNativeUA, tt.wantNativeOrigin},
			} {
				if got := check.request.Header.Get("User-Agent"); got != check.ua {
					t.Errorf("%s UA = %q, want %q", check.request.URL.Host, got, check.ua)
				}
				if got := check.request.Header.Get("Origin"); got != check.origin {
					t.Errorf("%s Origin = %q, want %q", check.request.URL.Host, got, check.origin)
				}
			}
		})
	}
}

func TestApplyBPSClientIdentityDoesNotRewriteUserAgent(t *testing.T) {
	h := http.Header{
		"User-Agent":  []string{"excel-client/1.0"},
		"Originator":  []string{"codex-tui"},
		"Version":     []string{"0.158.0"},
		"OpenAI-Beta": []string{"responses=experimental"},
	}
	applyBPSClientIdentity(h)
	if got := h.Get("User-Agent"); got != "excel-client/1.0" {
		t.Fatalf("user-agent was rewritten: %q", got)
	}
	for _, key := range []string{"Originator", "Version", "OpenAI-Beta"} {
		if got := h.Get(key); got != "" {
			t.Fatalf("retained Codex header %s=%q", key, got)
		}
	}
	if got := h.Get("x-openai-internal-basispoints-client-agent-profile"); got != "excel" {
		t.Fatalf("client profile: %q", got)
	}
}
