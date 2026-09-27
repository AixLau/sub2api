//go:build unit

package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const processingFailureEvent = "event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_failed\",\"status\":\"failed\",\"error\":{\"code\":\"server_error\",\"message\":\"An error occurred while processing your request. You can retry your request, or contact us through our help center at help.openai.com if the error persists. Please include the request ID rid-processing in your message.\"}}}\n\n"

func processingRetrySSE(events string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "X-Request-Id": []string{"rid-processing"}},
		Body:       io.NopCloser(strings.NewReader(events)),
	}
}

func processingRetrySuccess() *http.Response {
	return processingRetrySSE("event: response.completed\ndata: " + `{"type":"response.completed","response":{"id":"resp_success","model":"gpt-6-astra","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"recovered"}]}],"usage":{"input_tokens":1,"output_tokens":1}}}` + "\n\n")
}

func TestOpenAIProcessingFailureRetryRecoversOnSameOAuthAccount(t *testing.T) {
	for _, stream := range []bool{true, false} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			upstream := newAstraProCapturedUpstream(processingRetrySSE(processingFailureEvent), processingRetrySuccess())
			h := newOpenAIResponsesFailoverTestHandler(t, upstream)
			// Recovery must reuse the first OAuth account, without switching accounts.
			h.maxAccountSwitches = 0
			c, rec := newAstraProFailoverContext(t, fmt.Sprintf(`{"model":"gpt-6-astra","stream":%t,"input":"hello"}`, stream))
			markForwardableModerationReceipt(c, "openai_responses")
			h.Responses(c)
			_, accounts, bodies := upstream.snapshot()
			require.Equal(t, []int64{1, 1}, accounts)
			require.Equal(t, bodies[0], bodies[1], "retry must preserve the request body")
			require.Equal(t, http.StatusOK, rec.Code)
			require.Contains(t, rec.Body.String(), "recovered")
			require.NotContains(t, rec.Body.String(), "resp_failed")
		})
	}
}

func TestOpenAIProcessingFailureRetryStopsAfterFiveRetries(t *testing.T) {
	for _, allowSwitch := range []bool{false, true} {
		t.Run(fmt.Sprintf("allowSwitch=%t", allowSwitch), func(t *testing.T) {
			var responses []*http.Response
			for i := 0; i < 6; i++ {
				responses = append(responses, processingRetrySSE(processingFailureEvent))
			}
			responses = append(responses, processingRetrySuccess())
			upstream := newAstraProCapturedUpstream(responses...)
			h := newOpenAIResponsesFailoverTestHandler(t, upstream)
			h.maxAccountSwitches = 0
			if allowSwitch {
				h.maxAccountSwitches = 1
			}
			c, rec := newAstraProFailoverContext(t, `{"model":"gpt-6-astra","stream":true,"input":"hello"}`)
			markForwardableModerationReceipt(c, "openai_responses")
			h.Responses(c)
			_, accounts, _ := upstream.snapshot()
			if allowSwitch {
				require.Equal(t, []int64{1, 1, 1, 1, 1, 1, 2}, accounts, "switch only after five same-account retries")
				require.Equal(t, http.StatusOK, rec.Code)
				require.Contains(t, rec.Body.String(), "recovered")
			} else {
				require.Equal(t, []int64{1, 1, 1, 1, 1, 1}, accounts, "one initial attempt plus exactly five retries")
				require.Equal(t, http.StatusBadGateway, rec.Code)
				require.Contains(t, rec.Body.String(), "upstream_error")
				require.NotContains(t, rec.Body.String(), "recovered")
			}
		})
	}
}

func TestOpenAIProcessingFailureRetryDoesNotReplayDeliveredOutput(t *testing.T) {
	for name, output := range map[string]string{
		"text": `{"type":"response.output_text.delta","delta":"partial"}`,
		"tool": `{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"write_file","arguments":"{}"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			upstream := newAstraProCapturedUpstream(processingRetrySSE("data: "+output+"\n\n"+processingFailureEvent), processingRetrySuccess())
			h := newOpenAIResponsesFailoverTestHandler(t, upstream)
			c, rec := newAstraProFailoverContext(t, `{"model":"gpt-6-astra","stream":true,"input":"hello"}`)
			markForwardableModerationReceipt(c, "openai_responses")
			h.Responses(c)
			_, accounts, _ := upstream.snapshot()
			require.Equal(t, []int64{1}, accounts)
			require.Contains(t, rec.Body.String(), "response.failed")
			require.NotContains(t, rec.Body.String(), "recovered")
		})
	}
}

type canceledProcessingUpstream struct {
	*astraProCapturedUpstream
	cancel context.CancelFunc
}

func (u *canceledProcessingUpstream) Do(req *http.Request, proxy string, accountID int64, concurrency int) (*http.Response, error) {
	resp, err := u.astraProCapturedUpstream.Do(req, proxy, accountID, concurrency)
	u.cancel()
	return resp, err
}

func TestOpenAIProcessingFailureRetryStopsWhenClientCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	u := &canceledProcessingUpstream{newAstraProCapturedUpstream(processingRetrySSE(processingFailureEvent), processingRetrySuccess()), cancel}
	h := newOpenAIResponsesFailoverTestHandler(t, u)
	c, rec := newAstraProFailoverContext(t, `{"model":"gpt-6-astra","stream":true,"input":"hello"}`)
	c.Request = c.Request.WithContext(ctx)
	markForwardableModerationReceipt(c, "openai_responses")
	h.Responses(c)
	_, accounts, _ := u.snapshot()
	require.Equal(t, []int64{1}, accounts)
	require.NotContains(t, rec.Body.String(), "recovered")
}
