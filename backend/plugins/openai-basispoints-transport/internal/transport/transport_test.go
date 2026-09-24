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
}
