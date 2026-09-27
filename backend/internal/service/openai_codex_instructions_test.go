package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type codexInstructionsUpstream struct {
	lastReq  *http.Request
	lastBody []byte
	resp     *http.Response
}

func (u *codexInstructionsUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.lastReq = req
	body, err := io.ReadAll(req.Body)
	_ = req.Body.Close()
	if err != nil {
		return nil, err
	}
	u.lastBody = body
	return u.resp, nil
}

func (u *codexInstructionsUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, concurrency)
}

func TestApplyCodexOAuthTransform_PreservesInputInstructions(t *testing.T) {
	for _, isCodexCLI := range []bool{false, true} {
		for _, content := range []string{
			`"Client-owned coding instructions."`,
			`[{"type":"input_text","text":"Client-owned coding instructions."}]`,
		} {
			for _, instructions := range []string{"", `,"instructions":null`, `,"instructions":"  "`, `,"instructions":"Explicit top-level guidance."`} {
				t.Run(fmt.Sprintf("codex=%t/content=%s/instructions=%s", isCodexCLI, content, instructions), func(t *testing.T) {
					var body map[string]any
					require.NoError(t, json.Unmarshal([]byte(`{"model":"gpt-6-astra"`+instructions+`,"input":[{"role":"developer","content":`+content+`},{"role":"user","content":"hello"}]}`), &body))
					originalInstructions, hadInstructions := body["instructions"]
					originalInput, err := json.Marshal(body["input"])
					require.NoError(t, err)

					// Repeated transforms must not inject a default on a later pass.
					for range 2 {
						result := applyCodexOAuthTransform(body, isCodexCLI, false)
						require.NoError(t, result.Error)
						currentInstructions, hasInstructions := body["instructions"]
						require.Equal(t, hadInstructions, hasInstructions)
						require.Equal(t, originalInstructions, currentInstructions)
						currentInput, err := json.Marshal(body["input"])
						require.NoError(t, err)
						require.JSONEq(t, string(originalInput), string(currentInput))
					}
				})
			}
		}
	}
}

func TestApplyCodexOAuthTransform_DefaultInstructionsRequireMissingGuidance(t *testing.T) {
	for _, input := range []string{
		`[]`,
		`[{"role":"user","content":"User input is not developer guidance."}]`,
		`[{"role":"assistant","content":"An earlier response."}]`,
		`[{"role":"developer","content":"  "}]`,
		`[{"role":"developer","content":[{"type":"input_text","text":"  "}]}]`,
		`[{"type":"additional_tools","role":"developer","tools":[]}]`,
	} {
		t.Run(input, func(t *testing.T) {
			var body map[string]any
			require.NoError(t, json.Unmarshal([]byte(`{"model":"gpt-6-astra","input":`+input+`}`), &body))
			result := applyCodexOAuthTransform(body, true, false)
			require.NoError(t, result.Error)
			require.Equal(t, defaultCodexSynthInstructions("gpt-6-astra"), body["instructions"])
		})
	}
}

func TestOpenAIGatewayService_ForwardPreservesInputInstructions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name        string
		model       string
		passthrough bool
		lite        bool
		role        string
		mappedModel string
	}{
		{name: "native responses lite", model: "gpt-6-astra", lite: true},
		{name: "native responses", model: "gpt-6-astra"},
		{name: "codex model", model: "gpt-5.3-codex"},
		{name: "passthrough responses lite", model: "gpt-6-astra", passthrough: true, lite: true},
		{name: "passthrough codex model", model: "gpt-5.3-codex", passthrough: true},
		{name: "mapped developer instructions", model: "astra-public", mappedModel: "gpt-6-astra"},
		{name: "mapped default instructions", model: "astra-public", mappedModel: "gpt-6-astra", role: "user"},
		{name: "passthrough default instructions", model: "gpt-5.3-codex", passthrough: true, role: "user"},
		{name: "system instructions", model: "gpt-6-astra", role: "system"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Match native Codex's request shape without copying private captures.
			body := []byte(`{"model":"` + tc.model + `","stream":true,"input":[{"type":"additional_tools","role":"developer","tools":[]},{"type":"message","role":"developer","content":[{"type":"input_text","text":"You are Codex. Follow the client's coding instructions."}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"Reply OK."}]}]}`)
			if tc.role != "" {
				body = bytes.Replace(body, []byte(`"type":"message","role":"developer"`), []byte(`"type":"message","role":"`+tc.role+`"`), 1)
			}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
			c.Request.Header.Set("User-Agent", "codex_cli_rs/0.157.1")
			if tc.lite {
				c.Request.Header.Set(responsesLiteHeader, "true")
			}
			upstream := &codexInstructionsUpstream{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body: io.NopCloser(strings.NewReader(strings.Join([]string{
					`data: {"type":"response.completed","response":{"id":"resp_instructions","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}`,
					"", "data: [DONE]", "",
				}, "\n"))),
			}}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			account := &Account{
				ID: 123, Name: "instructions-test", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 1,
				Credentials: map[string]any{"access_token": "oauth-test-token", "chatgpt_account_id": "test-account"},
				Extra: map[string]any{
					"openai_passthrough":                        tc.passthrough,
					"openai_oauth_responses_websockets_v2_mode": OpenAIWSIngressModeOff,
				},
				Status: StatusActive, Schedulable: true,
			}
			if tc.mappedModel != "" {
				account.Credentials["model_mapping"] = map[string]any{tc.model: tc.mappedModel}
			}

			result, err := svc.Forward(context.Background(), c, account, body)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.NotNil(t, upstream.lastReq)
			model := tc.model
			if tc.mappedModel != "" {
				model = tc.mappedModel
			}
			require.Equal(t, model, gjson.GetBytes(upstream.lastBody, "model").String())
			switch tc.role {
			case "system":
				require.Equal(t, gjson.GetBytes(body, "input.1.content.0.text").String(), gjson.GetBytes(upstream.lastBody, "instructions").String())
				require.Equal(t, int64(2), gjson.GetBytes(upstream.lastBody, "input.#").Int())
				require.JSONEq(t, gjson.GetBytes(body, "input.2").Raw, gjson.GetBytes(upstream.lastBody, "input.1").Raw)
				return
			case "user":
				require.Equal(t, defaultCodexSynthInstructions(model), gjson.GetBytes(upstream.lastBody, "instructions").String())
			default:
				require.False(t, gjson.GetBytes(upstream.lastBody, "instructions").Exists())
			}
			require.JSONEq(t, gjson.GetBytes(body, "input").Raw, gjson.GetBytes(upstream.lastBody, "input").Raw)
		})
	}
}
