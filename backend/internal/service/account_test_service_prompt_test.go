//go:build unit

package service

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestAccountTestService_TextPromptReachesUpstream(t *testing.T) {
	for _, tc := range []struct {
		name, platform, accountType, model, protocol, path string
	}{
		{"claude", PlatformAnthropic, AccountTypeAPIKey, "claude-sonnet-4-6", "", "messages.0.content.0.text"},
		{"bedrock", PlatformAnthropic, AccountTypeBedrock, "claude-sonnet-4-6", "", "messages.0.content.0.text"},
		{"openai", PlatformOpenAI, AccountTypeAPIKey, "gpt-5.4", "", "input.0.content.0.text"},
		{"openai-oauth", PlatformOpenAI, AccountTypeOAuth, "gpt-5.4", "", "input.0.content.0.text"},
		{"grok", PlatformGrok, AccountTypeAPIKey, "grok-4.3", "", "input"},
		{"gemini", PlatformGemini, AccountTypeAPIKey, "gemini-2.5-flash", "", "contents.0.parts.0.text"},
		{"antigravity-apikey", PlatformAntigravity, AccountTypeAPIKey, "claude-sonnet-4-6", "", "messages.0.content.0.text"},
		{"cn-anthropic", PlatformZhipu, AccountTypeAPIKey, "glm-4.7", APIProtocolAnthropic, "messages.0.content.0.text"},
		{"cn-chat", PlatformZhipu, AccountTypeAPIKey, "glm-4.7", APIProtocolChatCompletions, "messages.0.content"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, prompt := range []string{"  请回答：\"你好\"\n第二行内容  ", "", " \n\t "} {
				t.Run(prompt, func(t *testing.T) {
					account := &Account{
						ID: 900, Platform: tc.platform, Type: tc.accountType, Concurrency: 1,
						Credentials: map[string]any{"api_key": "test-key", "access_token": "test-token", "api_protocol": tc.protocol, "auth_mode": "apikey"},
					}
					// Stop after capturing the request; response parsing is covered by protocol tests.
					svc, upstream := adaptiveCNAccountTestService(account, newJSONResponse(http.StatusBadRequest, `{"error":"test response"}`))
					c, _ := newTestContext()
					err := svc.TestAccountConnection(c, account.ID, tc.model, prompt, AccountTestModeDefault)
					require.Error(t, err)
					require.Len(t, upstream.requests, 1)
					want := strings.TrimSpace(prompt)
					if want == "" {
						want = "hi"
					}
					require.Equal(t, want, gjson.GetBytes(upstream.lastBody, tc.path).String())
				})
			}
		})
	}
}

func TestAccountTestService_AdaptiveForwardsPromptToEveryEndpoint(t *testing.T) {
	account := adaptiveCNAccountTestAccount(901, PlatformDeepseek)
	svc, upstream := adaptiveCNAccountTestService(account,
		adaptiveCNChatTestResponse(), adaptiveCNAnthropicTestResponse(), adaptiveCNResponsesTestResponse())
	c, _ := newTestContext()
	prompt := "请计算 1 + 1\n只返回结果"
	require.NoError(t, svc.TestAccountConnection(c, account.ID, "deepseek-chat", prompt, AccountTestModeDefault))
	require.Len(t, upstream.bodies, 3)
	for i, path := range []string{"messages.0.content", "messages.0.content.0.text", "input.0.content.0.text"} {
		require.Equal(t, prompt, gjson.GetBytes(upstream.bodies[i], path).String())
	}
}

func TestAntigravityTestRequest_CustomPrompt(t *testing.T) {
	svc := &AntigravityGatewayService{}
	for _, model := range []string{"gemini-2.5-flash", "claude-sonnet-4-6"} {
		t.Run(model, func(t *testing.T) {
			prompt := "请回答：\"你好\"\n第二行"
			var body []byte
			var err error
			if strings.HasPrefix(model, "gemini-") {
				body, err = svc.buildGeminiTestRequest("test-project", model, prompt)
			} else {
				body, err = svc.buildClaudeTestRequest("test-project", model, prompt)
			}
			require.NoError(t, err)
			require.Equal(t, prompt, gjson.GetBytes(body, "request.contents.0.parts.0.text").String())
			require.Greater(t, gjson.GetBytes(body, "request.generationConfig.maxOutputTokens").Int(), int64(1))
		})
	}
}
