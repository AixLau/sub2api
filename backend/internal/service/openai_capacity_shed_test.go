package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// --- mock: 只记录临时不可调度写入，其余方法不应被调用 ---

type capacityShedAccountRepoStub struct {
	AccountRepository // 嵌入接口，未实现的方法会 panic（不应被调用）

	tempUnschedCalls int
}

func (r *capacityShedAccountRepoStub) SetTempUnschedulable(_ context.Context, _ int64, _ time.Time, _ string) error {
	r.tempUnschedCalls++
	return nil
}

// 上游容量降载是请求级信号：故障因素（客户端身份、模型容量）与账号无关，
// 同账号重试用尽后不得把账号临时摘掉——否则一个被降载的请求会顺着 failover
// 把整池账号逐个封禁，而每个账号都会以同一个错误失败。
func TestTempUnscheduleRetryableErrorSkipsRequestScopedTransient(t *testing.T) {
	t.Run("请求级瞬时故障不写账号状态", func(t *testing.T) {
		repo := &capacityShedAccountRepoStub{}
		svc := &GatewayService{accountRepo: repo}

		svc.TempUnscheduleRetryableError(context.Background(), 1, &UpstreamFailoverError{
			StatusCode:             http.StatusBadGateway,
			RetryableOnSameAccount: true,
			RequestScopedTransient: true,
		})

		require.Zero(t, repo.tempUnschedCalls)
	})

	// 对照组：同样的 502 在未标记请求级瞬时故障时仍按原有语义临时摘号，
	// 确认上面的断言来自新增守卫而非其他前置条件。
	t.Run("未标记时保持原有临时摘号语义", func(t *testing.T) {
		repo := &capacityShedAccountRepoStub{}
		svc := &GatewayService{accountRepo: repo}

		svc.TempUnscheduleRetryableError(context.Background(), 1, &UpstreamFailoverError{
			StatusCode:             http.StatusBadGateway,
			RetryableOnSameAccount: true,
		})

		require.Equal(t, 1, repo.tempUnschedCalls)
	})
}

func TestRequestScopedTransientDoesNotReportAccountScheduleFailure(t *testing.T) {
	failure := &UpstreamFailoverError{
		StatusCode:             http.StatusTooManyRequests,
		RequestScopedTransient: true,
		RetryableOnSameAccount: true,
	}

	require.False(t, failure.ShouldReportAccountScheduleFailure())
}

// 非池模式账号同样要先在同账号重试：换号不改变降载因素。
func TestStreamFailedEventCapacityShedRetriesOnSameAccount(t *testing.T) {
	nonPool := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}

	for _, code := range []string{"server_is_overloaded", "slow_down"} {
		payload := []byte(`{"type":"response.failed","response":{"error":{"code":"` + code + `"}}}`)
		require.True(t, isOpenAIUpstreamCapacityShedEvent(payload), code)
		require.True(t, openAIStreamFailedEventRetryableOnSameAccount(nonPool, payload, "overloaded"), code)
	}

	// 非降载的 failed 事件在非池模式下仍不做同账号重试，避免放大改动面。
	other := []byte(`{"type":"response.failed","response":{"error":{"code":"server_error"}}}`)
	require.False(t, isOpenAIUpstreamCapacityShedEvent(other))
	require.False(t, openAIStreamFailedEventRetryableOnSameAccount(nonPool, other, "boom"))
}

func TestOpenAIHTTPCapacityShedIsRequestScopedForOAuthAccounts(t *testing.T) {
	payload := []byte(`{"error":{"type":"server_error","message":"Our servers are currently overloaded. Please try again later."}}`)
	failoverErr := newOpenAIUpstreamFailoverError(
		http.StatusBadRequest,
		http.Header{"X-Request-Id": []string{"rid-http-capacity"}},
		payload,
		"Our servers are currently overloaded. Please try again later.",
		false,
	)

	require.True(t, failoverErr.RetryableOnSameAccount)
	require.True(t, failoverErr.RequestScopedTransient)
	require.WithinDuration(t, time.Now().Add(OpenAIRequestScopedRetryWindow), failoverErr.SameAccountRetryDeadline, time.Second)

	repo := &capacityShedAccountRepoStub{}
	(&GatewayService{accountRepo: repo}).TempUnscheduleRetryableError(context.Background(), 1, failoverErr)
	require.Zero(t, repo.tempUnschedCalls)

	rateLimitService := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	gateway := &OpenAIGatewayService{rateLimitService: rateLimitService}
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	require.False(t, gateway.handleOpenAIAccountUpstreamError(
		context.Background(),
		account,
		http.StatusBadRequest,
		nil,
		payload,
		"gpt-5",
	))
	require.Zero(t, repo.tempUnschedCalls)
}

func TestOpenAIResponseProtectionUnavailableUsesBoundedSameAccountRetry(t *testing.T) {
	payload := []byte(`{"response":{"error":{"code":"upstream_error","message":"response protection is unavailable","type":"internal_error"}}}`)
	failoverErr := newOpenAIUpstreamFailoverError(
		http.StatusBadGateway,
		http.Header{"X-Request-Id": []string{"rid-protection"}},
		payload,
		"response protection is unavailable",
		false,
	)

	require.True(t, failoverErr.RetryableOnSameAccount)
	require.True(t, failoverErr.RequestScopedTransient)
	require.Equal(t, 5, failoverErr.SameAccountRetryMax)
	require.Equal(t, OpenAIResponseProtectionUnavailableReason, failoverErr.Reason)
	require.Equal(t, http.StatusBadGateway, failoverErr.ClientStatusCode)

	other := newOpenAIUpstreamFailoverError(
		http.StatusBadGateway,
		http.Header{},
		[]byte(`{"error":{"message":"a different provider failure"}}`),
		"a different provider failure",
		false,
	)
	require.NotEqual(t, OpenAIResponseProtectionUnavailableReason, other.Reason)
	require.Zero(t, other.SameAccountRetryMax)
	require.True(t, openAIStreamFailedEventShouldFailover(payload, "response protection is unavailable"))
	require.True(t, openAIStreamFailedEventRetryableOnSameAccount(&Account{ID: 1, Platform: PlatformOpenAI}, payload, "response protection is unavailable"))
	ssePayload := []byte("event: response.failed\ndata: {\"response\":{\"error\":{\"message\":\"response protection is unavailable\"}}}\n\n")
	require.True(t, isOpenAIResponseProtectionUnavailable(http.StatusBadGateway, "", ssePayload))
}

func TestOpenAICapacityShedHonorsRetryAfter(t *testing.T) {
	for _, test := range []struct {
		name    string
		headers http.Header
		body    string
		want    time.Duration
	}{
		{name: "http seconds", headers: http.Header{"Retry-After": []string{"6"}}, want: 6 * time.Second},
		{name: "http milliseconds", headers: http.Header{"Retry-After-Ms": []string{"6000"}}, want: 6 * time.Second},
		{name: "body seconds", body: `{"error":{"headers":{"retry-after":"7"},"message":"Our servers are currently overloaded. Please try again later."}}`, want: 7 * time.Second},
		{name: "body milliseconds", body: `{"response":{"error":{"headers":{"retry-after-ms":"7000"},"message":"Our servers are currently overloaded. Please try again later."}}}`, want: 7 * time.Second},
		{name: "bounded", headers: http.Header{"Retry-After": []string{"60"}}, want: openAIRequestScopedRetryMaxDelay},
	} {
		t.Run(test.name, func(t *testing.T) {
			failoverErr := newOpenAIUpstreamFailoverError(
				http.StatusServiceUnavailable,
				test.headers,
				[]byte(test.body),
				"Our servers are currently overloaded. Please try again later.",
				false,
			)
			require.True(t, failoverErr.RequestScopedTransient)
			require.Equal(t, test.want, failoverErr.SameAccountRetryDelay)
		})
	}
}

// 上游降载的真实序列是「event: error → event: response.failed」。error 帧不算
// 客户端输出：若把它当首输出 flush，clientOutputStarted 被固化，随后的 failed
// 事件就进不了 pre-output failover 分支，只能把致命错误原样转发给客户端。
func TestOpenAIStreamErrorFrameDoesNotStartClientOutput(t *testing.T) {
	cases := []struct {
		data      string
		eventType string
		want      bool
	}{
		{`{"type":"error","error":{"code":"server_is_overloaded","message":"overloaded"}}`, "error", false},
		{`{"type":"error","error":{"code":"slow_down","message":"slow down"}}`, "error", false},
		{`{"type":"error","code":"server_is_overloaded","message":"Please retry later."}`, "error", false},
		{`{"type":"error","error":{"type":"rate_limit_error","code":"rate_limit_exceeded","message":"limited"}}`, "error", false},
		// 不可重试类错误帧维持原样转发（不进 failover），保留上游错误细节。
		{`{"type":"error","error":{"type":"invalid_request_error","code":"content_policy_violation","message":"blocked"}}`, "error", true},
		{`{"type":"response.failed","response":{"error":{"code":"server_is_overloaded"}}}`, "response.failed", false},
		{`{"type":"response.created","response":{"id":"resp_1"}}`, "response.created", false},
		{`{"type":"response.queued","response":{"id":"resp_1","status":"queued"}}`, "response.queued", false},
		{`{"type":"response.metadata","response_id":"resp_1","metadata":{"openai_chatgpt_moderation_metadata":{"prompt":{}}}}`, "response.metadata", false},
		{`{"type":"response.in_progress","response":{"id":"resp_1"}}`, "response.in_progress", false},
		{`{"type":"keepalive"}`, "keepalive", false},
		{`{"type":"unknown_event"}`, "unknown_event", true},
		{`{"type":"response.output_item.added","item":{"type":"reasoning","summary":[]}}`, "response.output_item.added", false},
		{`{"type":"response.output_item.added","item":{"type":"reasoning","encrypted_content":"ciphertext"}}`, "response.output_item.added", true},
		{`{"type":"response.reasoning_summary_part.added","part":{"type":"summary_text","text":""}}`, "response.reasoning_summary_part.added", false},
		{`{"type":"response.reasoning_summary_part.added","part":{"type":"summary_text","text":"thinking"}}`, "response.reasoning_summary_part.added", true},
		{`{"type":"response.content_part.added","part":{"type":"output_text","text":""}}`, "response.content_part.added", false},
		{`{"type":"response.output_text.delta","delta":"hi"}`, "response.output_text.delta", true},
		{`{"type":"response.output_text.delta","delta":""}`, "response.output_text.delta", false},
		{`{"type":"response.reasoning_summary_text.done","text":""}`, "response.reasoning_summary_text.done", false},
		{`{"type":"response.output_item.done","item":{"type":"reasoning","summary":[]}}`, "response.output_item.done", false},
		{`{"type":"response.output_item.done","item":{"type":"message","content":[]}}`, "response.output_item.done", false},
		{`{"type":"response.content_part.done","part":{"type":"output_text","text":""}}`, "response.content_part.done", false},
		{`{"type":"response.reasoning_summary_part.done","part":{"type":"summary_text","text":""}}`, "response.reasoning_summary_part.done", false},
		{`{"type":"response.output_item.done","item":{"type":"function_call","name":"write_file","arguments":""}}`, "response.output_item.done", true},
		{`{"type":"response.output_item.done","item":{"type":"reasoning","encrypted_content":"ciphertext"}}`, "response.output_item.done", true},
		{`{"type":"response.output_text.done","text":"partial"}`, "response.output_text.done", true},
		{`[DONE]`, "", true},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, openAIStreamDataStartsClientOutput(tc.data, tc.eventType), "data=%s type=%s", tc.data, tc.eventType)
	}
}

func TestOpenAIStreamMetadataAndKeepaliveBeforeOverloadFailOver(t *testing.T) {
	gin.SetMode(gin.TestMode)
	largeMetadata := strings.Repeat("x", 16*1024)
	stream := strings.Join([]string{
		"event: response.created",
		`data: {"type":"response.created","response":{"id":"resp_1","metadata":{"padding":"` + largeMetadata + `"}}}`,
		"",
		"event: response.queued",
		`data: {"type":"response.queued","response":{"id":"resp_1","status":"queued"}}`,
		"",
		"event: response.metadata",
		`data: {"type":"response.metadata","response_id":"resp_1","metadata":{"openai_chatgpt_moderation_metadata":{"prompt":{}}}}`,
		"",
		"event: response.output_item.added",
		`data: {"type":"response.output_item.added","item":{"type":"reasoning","summary":[]}}`,
		"",
		"event: response.reasoning_summary_part.added",
		`data: {"type":"response.reasoning_summary_part.added","part":{"type":"summary_text","text":""}}`,
		"",
		`data: {"type":"response.output_text.delta","delta":""}`,
		"",
		`data: {"type":"response.reasoning_summary_text.done","text":""}`,
		"",
		`data: {"type":"response.reasoning_summary_part.done","part":{"type":"summary_text","text":""}}`,
		"",
		`data: {"type":"response.output_item.done","item":{"type":"reasoning","summary":[]}}`,
		"",
		"event: keepalive",
		`data: {"type":"keepalive"}`,
		"",
		`data: {"type":"keepalive"}`,
		"",
		"event: error",
		`data: {"type":"error","error":{"type":"service_unavailable_error","message":"Our servers are currently overloaded. Please try again later."}}`,
		"",
	}, "\n")

	tests := []struct {
		name string
		run  func(*OpenAIGatewayService, *gin.Context, *http.Response, *Account) error
	}{
		{
			name: "native",
			run: func(svc *OpenAIGatewayService, c *gin.Context, resp *http.Response, account *Account) error {
				_, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "model", "model")
				return err
			},
		},
		{
			name: "passthrough",
			run: func(svc *OpenAIGatewayService, c *gin.Context, resp *http.Response, account *Account) error {
				_, err := svc.handleStreamingResponsePassthrough(c.Request.Context(), resp, c, account, time.Now(), "model", "model")
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(stream)),
				Header:     http.Header{"X-Request-Id": []string{"rid-message-only-overload"}},
			}
			account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Name: "acc"}

			err := tt.run(svc, c, resp, account)
			require.Error(t, err)
			var failoverErr *UpstreamFailoverError
			require.ErrorAs(t, err, &failoverErr)
			require.True(t, failoverErr.RetryableOnSameAccount)
			require.True(t, failoverErr.RequestScopedTransient)
			require.Equal(t, http.StatusServiceUnavailable, failoverErr.ClientStatusCode)
			require.Equal(t, "Our servers are currently overloaded. Please try again later.", failoverErr.ClientMessage)
			require.False(t, c.Writer.Written())
			require.Empty(t, rec.Body.String())
		})
	}
}

func TestOpenAIStreamEmptyContentBeforeFailureCanFailOver(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, payload := range []string{
		`{"type":"response.output_text.delta","delta":""}`,
		`{"type":"response.reasoning_summary_text.delta","delta":""}`,
		`{"type":"response.reasoning_text.delta","delta":""}`,
		`{"type":"response.function_call_arguments.delta","delta":""}`,
		`{"type":"response.custom_tool_call_input.delta","delta":""}`,
		`{"type":"response.output_text.done","text":""}`,
		`{"type":"response.reasoning_summary_text.done","text":""}`,
		`{"type":"response.reasoning_text.done","text":""}`,
		`{"type":"response.refusal.delta","delta":""}`,
		`{"type":"response.refusal.done","refusal":""}`,
		`{"type":"response.content_part.done","part":{"type":"output_text","text":""}}`,
		`{"type":"response.reasoning_summary_part.done","part":{"type":"summary_text","text":""}}`,
	} {
		for _, passthrough := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/passthrough=%t", payload, passthrough), func(t *testing.T) {
				svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}}
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				resp := &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{},
					Body: io.NopCloser(strings.NewReader("data: " + payload + "\n\n" +
						`data: {"type":"response.failed","response":{"status":"failed","error":{"code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}}` + "\n\n")),
				}
				account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
				var err error
				if passthrough {
					_, err = svc.handleStreamingResponsePassthrough(c.Request.Context(), resp, c, account, time.Now(), "model", "model")
				} else {
					_, err = svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "model", "model")
				}
				var failoverErr *UpstreamFailoverError
				require.ErrorAs(t, err, &failoverErr)
				require.True(t, failoverErr.RequestScopedTransient)
				require.Empty(t, rec.Body.String())
			})
		}
	}
}

func TestOpenAIStreamReplayBoundaryPreservesContentAndToolCompletion(t *testing.T) {
	for _, payload := range []string{
		`{"type":"response.output_text.delta","delta":" "}`,
		`{"type":"response.function_call_arguments.delta","delta":"{}"}`,
		`{"type":"response.custom_tool_call_input.delta","delta":"run"}`,
		`{"type":"response.function_call_arguments.done","arguments":""}`,
		`{"type":"response.custom_tool_call_input.done","input":""}`,
		`{"type":"response.output_item.done","item":{"type":"function_call","arguments":""}}`,
		`{"type":"response.output_item.done","item":{"type":"reasoning","encrypted_content":"ciphertext"}}`,
		`{"type":"response.output_text.delta","delta":null}`,
		`{"type":"response.output_text.delta"}`,
		`{"type":"response.output_text.delta","delta":{}}`,
		`{"type":"response.new_kind.delta","delta":""}`,
	} {
		eventType := gjson.Get(payload, "type").String()
		require.True(t, openAIStreamDataStartsClientOutput(payload, eventType), payload)
	}
}

func TestOpenAIStreamKeepaliveOnlyFailureDoesNotRecordFirstToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, passthrough := range []bool{false, true} {
		name := "native"
		if passthrough {
			name = "passthrough"
		}
		t.Run(name, func(t *testing.T) {
			svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
				MaxLineSize: defaultMaxLineSize,
			}}}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{},
				Body: io.NopCloser(strings.NewReader(strings.Join([]string{
					`data: {"type":"response.created","response":{"id":"resp_heartbeat"}}`,
					"",
					`data: {"type":"keepalive"}`,
					"",
					`data: {"type":"response.failed","response":{"id":"resp_heartbeat","status":"failed","error":{"code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}}`,
					"",
					"",
				}, "\n"))),
			}
			account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
			var err error
			if passthrough {
				var result *openaiStreamingResultPassthrough
				result, err = svc.handleStreamingResponsePassthrough(c.Request.Context(), resp, c, account, time.Now(), "model", "model")
				require.NotNil(t, result)
				require.Nil(t, result.firstTokenMs)
			} else {
				var result *openaiStreamingResult
				result, err = svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "model", "model")
				require.NotNil(t, result)
				require.Nil(t, result.firstTokenMs)
			}
			var failoverErr *UpstreamFailoverError
			require.ErrorAs(t, err, &failoverErr)
			require.True(t, failoverErr.RetryableOnSameAccount)
			require.True(t, failoverErr.RequestScopedTransient)
			require.Empty(t, rec.Body.String())
		})
	}
}

// 回归用例（真实上游降载序列）：created → in_progress → error 帧 → response.failed。
// 期望仍然走 pre-output failover（同账号重试 + 请求级瞬时标记），且不向客户端写出任何字节。
func TestOpenAIStreamCapacityShedErrorFramePrecedingFailedStillFailsOver(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize},
	}
	svc := &OpenAIGatewayService{cfg: cfg}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			"event: response.created",
			`data: {"type":"response.created","response":{"id":"resp_1"},"sequence_number":0}`,
			"",
			"event: response.in_progress",
			`data: {"type":"response.in_progress","response":{"id":"resp_1"},"sequence_number":1}`,
			"",
			"event: error",
			`data: {"type":"error","error":{"type":"service_unavailable_error","code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."},"sequence_number":2}`,
			"",
			"event: response.failed",
			`data: {"type":"response.failed","response":{"id":"resp_1","status":"failed","error":{"code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}},"sequence_number":3}`,
			"",
		}, "\n"))),
		Header: http.Header{"X-Request-Id": []string{"rid-shed-error-then-failed"}},
	}

	_, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Name: "acc"}, time.Now(), "model", "model")
	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.True(t, failoverErr.RetryableOnSameAccount)
	require.True(t, failoverErr.RequestScopedTransient)
	require.False(t, c.Writer.Written())
	require.Empty(t, rec.Body.String())
}

// 流中途（已有真实输出）降载时无法再 failover，此时必须把上游降载事件原样
// 转发给客户端：错误码与错误文案都不改写，gateway 只负责记录与账号状态判定。
func TestOpenAIStreamCapacityShedAfterOutputForwardsProviderError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logSink, restore := captureStructuredLog(t)
	defer restore()
	cfg := &config.Config{
		Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize},
	}
	svc := &OpenAIGatewayService{cfg: cfg}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			"event: response.created",
			`data: {"type":"response.created","response":{"id":"resp_1"}}`,
			"",
			"event: response.output_text.delta",
			`data: {"type":"response.output_text.delta","delta":"partial"}`,
			"",
			"event: error",
			`data: {"type":"error","error":{"type":"service_unavailable_error","code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."},"sequence_number":2}`,
			"",
			"event: response.failed",
			`data: {"type":"response.failed","response":{"id":"resp_1","status":"failed","error":{"code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}},"sequence_number":3}`,
			"",
		}, "\n"))),
		Header: http.Header{"X-Request-Id": []string{"rid-shed-after-output"}},
	}

	_, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Name: "acc"}, time.Now(), "model", "model")
	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr))

	body := rec.Body.String()
	require.Contains(t, body, "partial")
	require.NotContains(t, body, "event: error")
	require.Equal(t, 1, strings.Count(body, "event: response.failed"))
	// 只改写 code：客户端据此退避重试；文案保留上游原文。
	require.Contains(t, body, `"code":"server_error"`)
	require.NotContains(t, body, `"code":"server_is_overloaded"`)
	require.Contains(t, body, "Our servers are currently overloaded")
	require.True(t, logSink.ContainsMessage("gateway.failover_suppressed_after_semantic_output"))
	require.True(t, logSink.ContainsFieldValue("path", "native_sse"))
	require.True(t, logSink.ContainsFieldValue("upstream_request_id", "rid-shed-after-output"))
}

func TestOpenAIStreamProcessingFailureAfterOutputIsRecorded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stream := strings.Join([]string{
		"event: response.created",
		`data: {"type":"response.created","response":{"id":"resp_processing_failure"}}`,
		"",
		"event: response.output_text.delta",
		`data: {"type":"response.output_text.delta","delta":"partial"}`,
		"",
		"event: response.failed",
		`data: {"type":"response.failed","response":{"id":"resp_processing_failure","status":"failed","error":{"code":"server_error","message":"An error occurred while processing your request. Please include the request ID rid-processing-failure in your message."}}}`,
		"",
	}, "\n")

	for _, passthrough := range []bool{false, true} {
		t.Run(map[bool]string{false: "native", true: "passthrough"}[passthrough], func(t *testing.T) {
			svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(stream)),
				Header:     http.Header{"X-Request-Id": []string{"rid-processing-failure"}},
			}
			account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Name: "codex-account"}

			var err error
			if passthrough {
				_, err = svc.handleStreamingResponsePassthrough(c.Request.Context(), resp, c, account, time.Now(), "gpt-5.6-terra", "gpt-5.6-terra")
			} else {
				_, err = svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "gpt-5.6-terra", "gpt-5.6-terra")
			}

			require.Error(t, err)
			var failoverErr *UpstreamFailoverError
			require.False(t, errors.As(err, &failoverErr))
			require.NotEmpty(t, rec.Body.String())
			require.Contains(t, rec.Body.String(), "response.failed")
			require.NotNil(t, c)
			rawEvents, ok := c.Get(OpsUpstreamErrorsKey)
			require.True(t, ok)
			events, ok := rawEvents.([]*OpsUpstreamErrorEvent)
			require.True(t, ok)
			require.NotEmpty(t, events)
			event := events[len(events)-1]
			require.Equal(t, "stream_failed", event.Kind)
			require.Equal(t, "rid-processing-failure", event.UpstreamRequestID)
			require.Contains(t, event.Message, "An error occurred while processing your request")
		})
	}
}

// 生产实测：上游降载常常发生在「响应头已提交、但一个语义字节都没写出」时
// （gin Size()==0），旧判定把它当成已输出，于是降载既不重试也直接透传，
// 客户端因 server_is_overloaded 判致命而终止会话。这里锁住修复后的行为：
// 只有真正写出非心跳字节才算输出，否则必须走同账号重试。
func TestOpenAIStreamCapacityShedAfterHeaderOnlyCommitRetriesOnSameAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	// 模拟响应头已提交（空 flush / 心跳提交响应头）：Size()==0，但没有语义字节。
	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.WriteHeaderNow() // 与 gin Flush() 提交响应头的效果一致
	require.Equal(t, 0, c.Writer.Size(), "前提：仅提交响应头时 gin 的 Size() 为 0")
	require.False(t, openAIStreamClientOutputStarted(c, false), "仅提交响应头不得判定为已输出")

	stream := strings.Join([]string{
		"event: response.created",
		`data: {"type":"response.created","response":{"id":"resp_header_only"}}`,
		"",
		"event: error",
		`data: {"type":"error","code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}`,
		"",
		"event: response.failed",
		`data: {"type":"response.failed","response":{"id":"resp_header_only","status":"failed","error":{"code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}}`,
		"",
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Request-Id": []string{"rid-header-only-shed"}},
		Body:       io.NopCloser(strings.NewReader(stream)),
	}
	account := &Account{ID: 9, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Name: "acc"}

	_, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "gpt-6-astra", "gpt-6-astra")

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr, "输出前降载必须返回重试信号而不是终态错误")
	require.True(t, failoverErr.RetryableOnSameAccount)
	require.True(t, failoverErr.RequestScopedTransient)
	require.WithinDuration(t, time.Now().Add(OpenAIRequestScopedRetryWindow), failoverErr.SameAccountRetryDeadline, time.Second)
	require.NotContains(t, rec.Body.String(), "server_is_overloaded", "致命降载码不得透传给客户端")
}

// 生产实测(378 账号):handler 在并发槽位等待期向下游打过一个 ":\n\n"(3 字节)
// SSE 心跳。心跳会提交 HTTP 200 并提交响应头,但不构成任何语义输出;如果它没有
// 计入「非语义字节」,上游在正文之前降载时就会因 Writer.Size()==3 被误判成
// 「已输出」而放弃同账号重试(实测 61 次透传)。修复后心跳必须被扣除,降载仍走
// 输出前重试,客户端除心跳外收不到任何字节。
func TestOpenAIStreamSlotWaitHeartbeatDoesNotBlockCapacityShedRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	// handler 的等待期心跳:写入 3 字节并登记为心跳。
	if _, err := c.Writer.WriteString(":\n\n"); err != nil {
		t.Fatalf("write heartbeat: %v", err)
	}
	RecordOpenAIStreamHeartbeatBytes(c, 3)
	require.Equal(t, -1, OpenAICompactKeepaliveAdjustedWrittenSize(c),
		"仅等待期心跳不得被算成语义输出")
	require.False(t, openAIStreamClientOutputStarted(c, false))

	echo := strings.Repeat("x", 2048)
	stream := strings.Join([]string{
		"event: response.created",
		`data: {"type":"response.created","response":{"id":"resp_hb","echo":"` + echo + `"}}`,
		"",
		"event: response.metadata",
		`data: {"type":"response.metadata","metadata":{"trace":"x"}}`,
		"",
		"event: error",
		`data: {"type":"error","code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}`,
		"",
		"event: response.failed",
		`data: {"type":"response.failed","response":{"id":"resp_hb","status":"failed","error":{"code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}}`,
		"",
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Request-Id": []string{"rid-heartbeat"}},
		Body:       io.NopCloser(strings.NewReader(stream)),
	}
	account := &Account{ID: 378, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Name: "pro"}

	_, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "gpt-6-sol", "gpt-6-sol")

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr, "只有等待期心跳时,降载必须走重试而不是终态")
	require.True(t, failoverErr.RequestScopedTransient)
	require.True(t, failoverErr.RetryableOnSameAccount)
	require.Equal(t, ":\n\n", rec.Body.String(), "客户端只应收到等待期心跳,不应收到任何上游帧")
}

// 生产实测（431/378 账号）：上游在正文之前推 response.created / response.in_progress /
// response.metadata，每帧都带几十 KB 的请求回显，随后直接推降载 error + response.failed。
// response.metadata 曾被当作语义输出：它触发首个 flush、把回显写给客户端并置位
// semantic_seen，于是降载被误判成「输出后」，白白丢掉本可安全重放的同账号重试。
// 修复后这三帧都必须留在 stage 里，降载走同账号重试，客户端一个字节都收不到。
func TestOpenAIStreamMetadataOnlyPrefixThenCapacityShedRetries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	echo := strings.Repeat("x", 4096)
	stream := strings.Join([]string{
		"event: response.created",
		`data: {"type":"response.created","response":{"id":"resp_echo","echo":"` + echo + `"}}`,
		"",
		"event: response.in_progress",
		`data: {"type":"response.in_progress","response":{"id":"resp_echo","echo":"` + echo + `"}}`,
		"",
		"event: response.metadata",
		`data: {"type":"response.metadata","metadata":{"trace":"x"}}`,
		"",
		"event: error",
		`data: {"type":"error","code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}`,
		"",
		"event: response.failed",
		`data: {"type":"response.failed","response":{"id":"resp_echo","status":"failed","error":{"code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}}`,
		"",
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Request-Id": []string{"rid-meta-prefix"}},
		Body:       io.NopCloser(strings.NewReader(stream)),
	}
	account := &Account{ID: 431, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Name: "acc"}

	_, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "gpt-6.1-sol", "gpt-6.1-sol")

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr, "只有元数据回显的降载必须走重试而不是终态")
	require.True(t, failoverErr.RequestScopedTransient)
	require.True(t, failoverErr.RetryableOnSameAccount)
	require.Empty(t, rec.Body.String(), "回显帧必须留在 stage 内，客户端不应收到任何字节")
	require.False(t, c.Writer.Written(), "响应头也不应提交")
}

// 客户端写出的 error / response.failed 事件**只改写降载错误码**：
// server_is_overloaded / slow_down → server_error（Codex 等客户端据此退避重试，
// 而不是判致命终止会话）；文案与 type 原样保留。其余错误码一律不动
// （rate_limit_exceeded 的原码被客户端用来解析重试延时）。
func TestOpenAIStreamFailedEventsRewriteOnlyCapacityShedCode(t *testing.T) {
	cases := []struct {
		name        string
		eventType   string
		payload     string
		wantChanged bool
		wantContain []string
		wantAbsent  []string
	}{
		{
			name:        "failed事件改写降载码保留文案",
			eventType:   "response.failed",
			payload:     `{"type":"response.failed","response":{"error":{"code":"server_is_overloaded","message":"overloaded"}}}`,
			wantChanged: true,
			wantContain: []string{`"code":"server_error"`, "overloaded"},
			wantAbsent:  []string{"server_is_overloaded"},
		},
		{
			name:        "error帧改写slow_down",
			eventType:   "error",
			payload:     `{"type":"error","error":{"code":"slow_down","message":"slow down"}}`,
			wantChanged: true,
			wantContain: []string{`"code":"server_error"`, "slow down"},
			wantAbsent:  []string{"slow_down"},
		},
		{
			name:        "裸error帧改写顶层码",
			eventType:   "error",
			payload:     `{"type":"error","code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}`,
			wantChanged: true,
			wantContain: []string{`"code":"server_error"`, "Our servers are currently overloaded. Please try again later."},
			wantAbsent:  []string{"server_is_overloaded"},
		},
		{
			name:        "rate_limit保持原码",
			eventType:   "response.failed",
			payload:     `{"type":"response.failed","response":{"error":{"code":"rate_limit_exceeded","message":"try again in 3s"}}}`,
			wantChanged: false,
			wantContain: []string{`"code":"rate_limit_exceeded"`},
		},
		{
			name:        "非JSON原样",
			eventType:   "error",
			payload:     `not-json`,
			wantChanged: false,
			wantContain: []string{`not-json`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, changed := sanitizeOpenAIResponseFailedEventForClient([]byte(tc.payload), tc.eventType, false)
			require.Equal(t, tc.wantChanged, changed)
			for _, want := range tc.wantContain {
				require.Contains(t, string(out), want)
			}
			for _, absent := range tc.wantAbsent {
				require.NotContains(t, string(out), absent)
			}
		})
	}
}

// 出站身份的版本声明只能有一个来源：UA 的版本段、version 头、探针版本三处必须同源，
// 各自硬编码会漂移成互相矛盾的身份，而自相矛盾或陈旧的身份会被上游优先降载。
func TestCodexOutboundVersionHasSingleSource(t *testing.T) {
	require.True(t,
		strings.HasPrefix(codexCLIUserAgent, openai.CodexDefaultOriginator+"/"+codexCLIVersion+" "),
		"codexCLIUserAgent=%q 必须以 codexCLIVersion=%q 作为版本段", codexCLIUserAgent, codexCLIVersion,
	)
	require.GreaterOrEqual(t, CompareVersions(codexCLIVersion, codexUpstreamMinVersion), 0,
		"codexCLIVersion=%q 不得低于上游最低门槛 %q", codexCLIVersion, codexUpstreamMinVersion,
	)
}
