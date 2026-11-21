package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const chatHTTP2RetryCompletedStream = `data: {"type":"response.created","response":{"id":"resp_2","model":"gpt-5.4","status":"in_progress","output":[]}}

data: {"type":"response.output_text.delta","delta":"ok"}

data: {"type":"response.completed","response":{"id":"resp_2","object":"response","model":"gpt-5.4","status":"completed","output":[{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":11,"output_tokens":5,"total_tokens":16}}}

data: [DONE]

`

func chatHTTP2RetryTestAccount() *Account {
	return &Account{
		ID:          1,
		Name:        "openai-oauth",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "oauth-token",
			"chatgpt_account_id": "chatgpt-acc",
		},
	}
}

func chatHTTP2RetrySSE(body io.ReadCloser) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_h2_retry"}},
		Body:       body,
	}
}

func runChatHTTP2RetryForward(t *testing.T, responses ...*http.Response) (*httpUpstreamRecorder, *httptest.ResponseRecorder, *OpenAIForwardResult, error) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}],"stream":true}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	upstream := &httpUpstreamRecorder{responses: responses}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	result, err := svc.ForwardAsChatCompletions(context.Background(), c, chatHTTP2RetryTestAccount(), body, "", "gpt-5.1")
	return upstream, rec, result, err
}

func TestForwardAsChatCompletions_RetriesPreOutputHTTP2ResetOverHTTP1(t *testing.T) {
	for name, firstPayload := range map[string]string{
		"reset before any event": "",
		"reset after response.created": `data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-5.4","status":"in_progress","output":[]}}` + "\n\n" +
			`data: {"type":"response.in_progress","response":{"id":"resp_1","model":"gpt-5.4","status":"in_progress","output":[]}}` + "\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			upstream, rec, result, err := runChatHTTP2RetryForward(t,
				chatHTTP2RetrySSE(&openAIChatStreamReadErrorCloser{
					payload: []byte(firstPayload),
					err:     errors.New("stream error: stream ID 2335; INTERNAL_ERROR; received from peer"),
				}),
				chatHTTP2RetrySSE(io.NopCloser(strings.NewReader(chatHTTP2RetryCompletedStream))),
			)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, 5, result.Usage.OutputTokens)
			require.Len(t, upstream.requests, 2)
			require.Equal(t, HTTPUpstreamProtocolOverrideNone, HTTPUpstreamProtocolOverrideFromContext(upstream.requests[0].Context()))
			require.Equal(t, HTTPUpstreamProtocolOverrideHTTP1, HTTPUpstreamProtocolOverrideFromContext(upstream.requests[1].Context()))
			require.Equal(t, upstream.bodies[0], upstream.bodies[1], "replay must send the same upstream request body")
			require.Contains(t, rec.Body.String(), `"content":"ok"`)
			require.NotContains(t, rec.Body.String(), OpenAIUpstreamHTTP2StreamErrorCode)
			require.Equal(t, 1, strings.Count(rec.Body.String(), `"role":"assistant"`), "replay must not resend the role preamble")
			ids := map[string]struct{}{}
			for _, line := range strings.Split(rec.Body.String(), "\n") {
				payload, ok := strings.CutPrefix(line, "data: ")
				if !ok || payload == "[DONE]" {
					continue
				}
				ids[gjson.Get(payload, "id").String()] = struct{}{}
			}
			require.Len(t, ids, 1, "every chunk the client sees must share one completion id")
		})
	}
}

func TestForwardAsChatCompletions_HTTP2ResetRetriesOnlyOnce(t *testing.T) {
	reset := func() *http.Response {
		return chatHTTP2RetrySSE(&openAIChatStreamReadErrorCloser{
			err: errors.New("stream error: stream ID 7; INTERNAL_ERROR; received from peer"),
		})
	}
	upstream, _, _, err := runChatHTTP2RetryForward(t, reset(), reset(), reset())

	require.Error(t, err)
	require.Len(t, upstream.requests, 2)
	code, _, ok := OpenAIUpstreamStreamReadErrorDetails(err)
	require.True(t, ok)
	require.Equal(t, OpenAIUpstreamHTTP2StreamErrorCode, code)
}

func TestForwardAsChatCompletions_DoesNotRetryHTTP2ResetAfterOutput(t *testing.T) {
	upstream, rec, _, err := runChatHTTP2RetryForward(t,
		chatHTTP2RetrySSE(&openAIChatStreamReadErrorCloser{
			payload: []byte(`data: {"type":"response.output_text.delta","delta":"partial"}` + "\n\n"),
			err:     errors.New("stream error: stream ID 5; INTERNAL_ERROR; received from peer"),
		}),
		chatHTTP2RetrySSE(io.NopCloser(strings.NewReader(chatHTTP2RetryCompletedStream))),
	)

	require.Error(t, err)
	require.Len(t, upstream.requests, 1)
	require.Contains(t, rec.Body.String(), "partial")
}

func TestForwardAsChatCompletions_DoesNotRetryNonHTTP2ReadError(t *testing.T) {
	upstream, _, _, err := runChatHTTP2RetryForward(t,
		chatHTTP2RetrySSE(&openAIChatStreamReadErrorCloser{err: io.ErrUnexpectedEOF}),
		chatHTTP2RetrySSE(io.NopCloser(strings.NewReader(chatHTTP2RetryCompletedStream))),
	)

	require.Error(t, err)
	require.Len(t, upstream.requests, 1)
	code, _, ok := OpenAIUpstreamStreamReadErrorDetails(err)
	require.True(t, ok)
	require.Equal(t, OpenAIUpstreamStreamReadErrorCode, code)
}
