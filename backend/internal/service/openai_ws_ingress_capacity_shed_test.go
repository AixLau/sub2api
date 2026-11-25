package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// openAIWSIngressCapacityShedRepo 补齐 SetError，避免非容量类错误（如
// workspace_suspended）走到账号状态副作用时打空指针。
type openAIWSIngressCapacityShedRepo struct {
	stubOpenAIAccountRepo
}

func (r *openAIWSIngressCapacityShedRepo) SetError(context.Context, int64, string) error { return nil }

func (r *openAIWSIngressCapacityShedRepo) SetRateLimited(context.Context, int64, time.Time) error {
	return nil
}

func (r *openAIWSIngressCapacityShedRepo) UpdateExtra(context.Context, int64, map[string]any) error {
	return nil
}

// ctx_pool 的 ingress 直写路径把 error / response.failed 交给 WS 客户端前，把容量
// 降载码改写为客户端可重试的 server_error（Codex 对 server_is_overloaded /
// slow_down 判致命并终止会话），文案保留上游原文；非降载错误码一律原样下发。
func TestProxyResponsesWebSocketFromClient_RewritesCapacityShedCodeForClient(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		upstreamEvents [][]byte
		wantContains   []string
		wantAbsent     []string
	}{
		{
			name: "capacity_shed_error_and_failed_are_rewritten",
			upstreamEvents: [][]byte{
				[]byte(`{"type":"response.output_text.delta","delta":"partial"}`),
				[]byte(`{"type":"error","error":{"type":"service_unavailable_error","code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}`),
				[]byte(`{"type":"response.failed","response":{"id":"resp_shed","status":"failed","error":{"code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}}`),
			},
			wantContains: []string{
				`"delta":"partial"`,
				`"code":"server_error"`,
				"Our servers are currently overloaded. Please try again later.",
			},
			wantAbsent: []string{"server_is_overloaded"},
		},
		{
			name: "non_capacity_error_code_is_passed_through",
			upstreamEvents: [][]byte{
				[]byte(`{"type":"error","error":{"type":"invalid_request_error","code":"workspace_suspended","message":"workspace is suspended"}}`),
				[]byte(`{"type":"response.failed","response":{"id":"resp_suspended","status":"failed","error":{"code":"workspace_suspended","message":"workspace is suspended"}}}`),
			},
			wantContains: []string{
				`"code":"workspace_suspended"`,
				"workspace is suspended",
			},
			wantAbsent: []string{"server_error"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newOpenAIWSV2TestConfig()
			cfg.Security.URLAllowlist.Enabled = false
			cfg.Security.URLAllowlist.AllowInsecureHTTP = true
			cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
			cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
			cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
			cfg.Gateway.OpenAIWS.QueueLimitPerConn = 8
			cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 3
			cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
			cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3

			events := make([][]byte, 0, len(tt.upstreamEvents))
			for _, event := range tt.upstreamEvents {
				events = append(events, append([]byte(nil), event...))
			}
			captureConn := &openAIWSCaptureConn{events: events}
			pool := newOpenAIWSConnPool(cfg)
			pool.setClientDialerForTest(&openAIWSCaptureDialer{conn: captureConn})

			account := Account{
				ID:          5401,
				Name:        "openai-ingress-capacity-shed",
				Platform:    PlatformOpenAI,
				Type:        AccountTypeAPIKey,
				Status:      StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Credentials: map[string]any{"api_key": "sk-test"},
				Extra:       map[string]any{"responses_websockets_v2_enabled": true},
			}
			repo := &openAIWSIngressCapacityShedRepo{stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}}}
			svc := &OpenAIGatewayService{
				accountRepo:      repo,
				rateLimitService: &RateLimitService{accountRepo: repo},
				httpUpstream:     &httpUpstreamRecorder{},
				cache:            &stubGatewayCache{},
				cfg:              cfg,
				openaiWSResolver: NewOpenAIWSProtocolResolver(cfg),
				toolCorrector:    NewCodexToolCorrector(),
				openaiWSPool:     pool,
			}

			serverDone := make(chan struct{})
			wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(serverDone)
				conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{CompressionMode: coderws.CompressionContextTakeover})
				if err != nil {
					return
				}
				defer func() { _ = conn.CloseNow() }()

				rec := httptest.NewRecorder()
				ginCtx, _ := gin.CreateTestContext(rec)
				req := r.Clone(r.Context())
				req.Header = req.Header.Clone()
				req.Header.Set("User-Agent", "unit-test-agent/1.0")
				ginCtx.Request = req

				readCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
				msgType, firstMessage, readErr := conn.Read(readCtx)
				cancel()
				if readErr != nil || (msgType != coderws.MessageText && msgType != coderws.MessageBinary) {
					return
				}
				_ = svc.ProxyResponsesWebSocketFromClient(r.Context(), ginCtx, conn, &account, "sk-test", firstMessage, nil)
			}))
			defer wsServer.Close()

			dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
			clientConn, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(wsServer.URL, "http"), nil)
			cancelDial()
			require.NoError(t, err)
			defer func() { _ = clientConn.CloseNow() }()

			writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
			err = clientConn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","stream":false}`))
			cancelWrite()
			require.NoError(t, err)

			var frames []string
			for len(frames) < len(tt.upstreamEvents) {
				readCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				_, message, readErr := clientConn.Read(readCtx)
				cancel()
				if readErr != nil {
					break
				}
				frames = append(frames, string(message))
			}
			// 本轮已终止，主动断开客户端让 ingress 退出 turn 循环。
			_ = clientConn.CloseNow()

			require.NotEmpty(t, frames, "客户端应至少收到一个下发事件")
			joined := strings.Join(frames, "\n")
			for _, want := range tt.wantContains {
				require.Contains(t, joined, want, "客户端收到的事件:\n%s", joined)
			}
			for _, absent := range tt.wantAbsent {
				require.NotContains(t, joined, absent, "客户端收到的事件:\n%s", joined)
			}

			select {
			case <-serverDone:
			case <-time.After(5 * time.Second):
				t.Fatal("等待 ingress websocket 结束超时")
			}
		})
	}
}

func TestProxyResponsesWebSocketFromClient_MarksCyberPolicyBeforeEarlyReturn(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name          string
		upstreamEvent []byte
		wantFailover  bool
		wantClientMsg bool
		wantInput     int
		wantOutput    int
	}{
		{
			name:          "error_before_rate_limit_failover",
			upstreamEvent: []byte(`{"type":"error","error":{"type":"rate_limit_error","code":"cyber_policy","message":"rate limit exceeded by cyber policy"},"usage":{"input_tokens":5,"output_tokens":1}}`),
			wantFailover:  true,
			wantInput:     5,
			wantOutput:    1,
		},
		{
			name:          "response_failed_terminal",
			upstreamEvent: []byte(`{"type":"response.failed","response":{"id":"resp_cyber","status":"failed","error":{"code":"cyber_policy","message":"blocked by cyber policy"},"usage":{"input_tokens":9,"output_tokens":2}}}`),
			wantClientMsg: true,
			wantInput:     9,
			wantOutput:    2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newOpenAIWSV2TestConfig()
			cfg.Security.URLAllowlist.Enabled = false
			cfg.Security.URLAllowlist.AllowInsecureHTTP = true
			cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
			cfg.Gateway.OpenAIWS.IngressModeDefault = OpenAIWSIngressModeCtxPool
			cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0

			captureConn := &openAIWSCaptureConn{events: [][]byte{append([]byte(nil), tt.upstreamEvent...)}}
			pool := newOpenAIWSConnPool(cfg)
			pool.setClientDialerForTest(&openAIWSCaptureDialer{conn: captureConn})
			svc := &OpenAIGatewayService{
				cfg:              cfg,
				httpUpstream:     &httpUpstreamRecorder{},
				cache:            &stubGatewayCache{},
				openaiWSResolver: NewOpenAIWSProtocolResolver(cfg),
				toolCorrector:    NewCodexToolCorrector(),
				openaiWSPool:     pool,
			}
			account := &Account{
				ID: 5402, Name: "openai-ingress-cyber", Platform: PlatformOpenAI,
				Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1,
				Credentials: map[string]any{"api_key": "sk-test"},
				Extra: map[string]any{
					"openai_apikey_responses_websockets_v2_mode": OpenAIWSIngressModeCtxPool,
				},
			}

			markCh := make(chan *CyberPolicyMark, 1)
			serverErrCh := make(chan error, 1)
			wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{CompressionMode: coderws.CompressionContextTakeover})
				if err != nil {
					serverErrCh <- err
					return
				}
				defer func() { _ = conn.CloseNow() }()

				readCtx, cancelRead := context.WithTimeout(r.Context(), 3*time.Second)
				_, firstMessage, err := conn.Read(readCtx)
				cancelRead()
				if err != nil {
					serverErrCh <- err
					return
				}

				recorder := httptest.NewRecorder()
				ginCtx, _ := gin.CreateTestContext(recorder)
				ginCtx.Request = r.Clone(r.Context())
				hooks := &OpenAIWSIngressHooks{AfterTurn: func(_ int, _ *OpenAIForwardResult, _ error) {
					markCh <- GetOpsCyberPolicy(ginCtx)
				}}
				serverErrCh <- svc.ProxyResponsesWebSocketFromClient(r.Context(), ginCtx, conn, account, "sk-test", firstMessage, hooks)
			}))
			defer wsServer.Close()

			dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
			clientConn, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(wsServer.URL, "http"), nil)
			cancelDial()
			require.NoError(t, err)
			defer func() { _ = clientConn.CloseNow() }()

			writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
			err = clientConn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","stream":false}`))
			cancelWrite()
			require.NoError(t, err)

			if tt.wantClientMsg {
				readCtx, cancelRead := context.WithTimeout(context.Background(), 3*time.Second)
				_, message, readErr := clientConn.Read(readCtx)
				cancelRead()
				require.NoError(t, readErr)
				require.Equal(t, "response.failed", gjson.GetBytes(message, "type").String())
			}

			select {
			case mark := <-markCh:
				require.NotNil(t, mark)
				require.Equal(t, "cyber_policy", mark.Code)
				require.Equal(t, tt.wantInput, mark.UpstreamInTok)
				require.Equal(t, tt.wantOutput, mark.UpstreamOutTok)
			case <-time.After(3 * time.Second):
				t.Fatal("AfterTurn did not observe the cyber mark")
			}

			_ = clientConn.CloseNow()
			select {
			case serverErr := <-serverErrCh:
				var failoverErr *UpstreamFailoverError
				require.Equal(t, tt.wantFailover, errors.As(serverErr, &failoverErr))
			case <-time.After(5 * time.Second):
				t.Fatal("waiting for ingress websocket shutdown timed out")
			}
		})
	}
}

func newOpenAIWSIngressCapacityShedTestHarness(
	t *testing.T,
	account Account,
	dialer openAIWSClientDialer,
	hooks *OpenAIWSIngressHooks,
) (*httptest.Server, <-chan error) {
	t.Helper()
	cfg := newOpenAIWSV2TestConfig()
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 2
	cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 2
	cfg.Gateway.OpenAIWS.QueueLimitPerConn = 8
	cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3

	pool := newOpenAIWSConnPool(cfg)
	pool.setClientDialerForTest(dialer)
	repo := &openAIWSIngressCapacityShedRepo{stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}}}
	svc := &OpenAIGatewayService{
		accountRepo:      repo,
		rateLimitService: &RateLimitService{accountRepo: repo},
		httpUpstream:     &httpUpstreamRecorder{},
		cache:            &stubGatewayCache{},
		cfg:              cfg,
		openaiWSResolver: NewOpenAIWSProtocolResolver(cfg),
		toolCorrector:    NewCodexToolCorrector(),
		openaiWSPool:     pool,
	}

	serverErrCh := make(chan error, 1)
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{CompressionMode: coderws.CompressionContextTakeover})
		if err != nil {
			serverErrCh <- err
			return
		}
		defer func() { _ = conn.CloseNow() }()

		rec := httptest.NewRecorder()
		ginCtx, _ := gin.CreateTestContext(rec)
		req := r.Clone(r.Context())
		req.Header = req.Header.Clone()
		req.Header.Set("User-Agent", "unit-test-agent/1.0")
		ginCtx.Request = req

		readCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		_, firstMessage, readErr := conn.Read(readCtx)
		cancel()
		if readErr != nil {
			serverErrCh <- readErr
			return
		}
		serverErrCh <- svc.ProxyResponsesWebSocketFromClient(r.Context(), ginCtx, conn, &account, "test-token", firstMessage, hooks)
	}))
	t.Cleanup(wsServer.Close)
	return wsServer, serverErrCh
}

func TestProxyResponsesWebSocketFromClient_PreOutputCapacityShedStagesAndRetriesInPlace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	prevDelay := openAIWSIngressCapacityShedRetryDelay
	openAIWSIngressCapacityShedRetryDelay = time.Millisecond
	t.Cleanup(func() { openAIWSIngressCapacityShedRetryDelay = prevDelay })

	captureConn := &openAIWSCaptureConn{
		events: [][]byte{
			// Attempt 1: preamble frames + server_is_overloaded error + response.failed
			[]byte(`{"type":"response.created","response":{"id":"resp_shed_1","model":"gpt-5.1"}}`),
			[]byte(`{"type":"response.in_progress","response":{"id":"resp_shed_1","status":"in_progress"}}`),
			[]byte(`{"type":"response.output_item.added","item":{"id":"rs_1","type":"reasoning","summary":[]}}`),
			[]byte(`{"type":"error","error":{"type":"service_unavailable_err","code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}`),
			[]byte(`{"type":"response.failed","response":{"id":"resp_shed_1","status":"failed","error":{"code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}}`),
			// Attempt 2: clean success on the same upstream WS connection
			[]byte(`{"type":"response.created","response":{"id":"resp_ok_1","model":"gpt-5.1"}}`),
			[]byte(`{"type":"response.output_text.delta","delta":"hello world"}`),
			[]byte(`{"type":"response.completed","response":{"id":"resp_ok_1","model":"gpt-5.1","usage":{"input_tokens":10,"output_tokens":4}}}`),
		},
	}
	dialer := &openAIWSCaptureDialer{conn: captureConn}
	account := Account{
		ID:          5410,
		Name:        "openai-ingress-capacity-retry-turn1",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-test-token"},
		Extra:       map[string]any{"responses_websockets_v2_enabled": true},
	}

	var mu sync.Mutex
	beforeTurnCalls := 0
	afterTurnCalls := 0
	hooks := &OpenAIWSIngressHooks{
		BeforeTurn: func(int) error {
			mu.Lock()
			beforeTurnCalls++
			mu.Unlock()
			return nil
		},
		AfterTurn: func(int, *OpenAIForwardResult, error) {
			mu.Lock()
			afterTurnCalls++
			mu.Unlock()
		},
	}

	wsServer, serverErrCh := newOpenAIWSIngressCapacityShedTestHarness(t, account, dialer, hooks)

	dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
	clientConn, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(wsServer.URL, "http"), nil)
	cancelDial()
	require.NoError(t, err)
	defer func() { _ = clientConn.CloseNow() }()

	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	require.NoError(t, clientConn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","input":"hi"}`)))
	cancelWrite()

	var frames []string
	for len(frames) < 3 {
		readCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, msg, readErr := clientConn.Read(readCtx)
		cancel()
		require.NoError(t, readErr)
		frames = append(frames, string(msg))
	}
	_ = clientConn.Close(coderws.StatusNormalClosure, "done")

	select {
	case serverErr := <-serverErrCh:
		require.NoError(t, serverErr)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for ingress server to finish")
	}

	require.Equal(t, 1, dialer.dialCount, "upstream WS connection should be reused after response.failed")
	require.Len(t, captureConn.writes, 2, "expected 1 initial request + 1 in-place retry")
	require.Equal(t, "response.created", gjson.Get(frames[0], "type").String())
	require.Equal(t, "resp_ok_1", gjson.Get(frames[0], "response.id").String())
	require.Equal(t, "response.output_text.delta", gjson.Get(frames[1], "type").String())
	require.Equal(t, "hello world", gjson.Get(frames[1], "delta").String())
	require.Equal(t, "response.completed", gjson.Get(frames[2], "type").String())
	joined := strings.Join(frames, "\n")
	require.NotContains(t, joined, "resp_shed_1")
	require.NotContains(t, joined, "server_is_overloaded")

	mu.Lock()
	require.Equal(t, 1, beforeTurnCalls)
	require.Equal(t, 1, afterTurnCalls)
	mu.Unlock()
}

func TestProxyResponsesWebSocketFromClient_LaterTurnStrictAffinityCapacityShedRetriesOnSameConn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	prevDelay := openAIWSIngressCapacityShedRetryDelay
	prevPreflightPingIdle := openAIWSIngressPreflightPingIdle
	openAIWSIngressCapacityShedRetryDelay = time.Millisecond
	openAIWSIngressPreflightPingIdle = time.Hour
	t.Cleanup(func() {
		openAIWSIngressCapacityShedRetryDelay = prevDelay
		openAIWSIngressPreflightPingIdle = prevPreflightPingIdle
	})

	captureConn := &openAIWSCaptureConn{
		events: [][]byte{
			// Turn 1: succeeds
			[]byte(`{"type":"response.created","response":{"id":"resp_turn1","model":"gpt-5.1"}}`),
			[]byte(`{"type":"response.output_text.delta","delta":"turn1"}`),
			[]byte(`{"type":"response.completed","response":{"id":"resp_turn1","model":"gpt-5.1","usage":{"input_tokens":5,"output_tokens":2}}}`),
			// Turn 2 attempt 1 (strict affinity previous_response_id=resp_turn1): server_is_overloaded
			[]byte(`{"type":"response.created","response":{"id":"resp_turn2_shed","model":"gpt-5.1"}}`),
			[]byte(`{"type":"response.in_progress","response":{"id":"resp_turn2_shed","status":"in_progress"}}`),
			[]byte(`{"type":"error","error":{"type":"service_unavailable_err","code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}`),
			[]byte(`{"type":"response.failed","response":{"id":"resp_turn2_shed","status":"failed","error":{"code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}}`),
			// Turn 2 attempt 2 on same pinned WS conn: succeeds
			[]byte(`{"type":"response.created","response":{"id":"resp_turn2_ok","model":"gpt-5.1"}}`),
			[]byte(`{"type":"response.output_text.delta","delta":"turn2"}`),
			[]byte(`{"type":"response.completed","response":{"id":"resp_turn2_ok","model":"gpt-5.1","usage":{"input_tokens":8,"output_tokens":3}}}`),
		},
	}
	dialer := &openAIWSCaptureDialer{conn: captureConn}
	account := Account{
		ID:          5411,
		Name:        "openai-ingress-capacity-retry-turn2-strict",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-test-token"},
		Extra:       map[string]any{"responses_websockets_v2_enabled": true},
	}

	wsServer, serverErrCh := newOpenAIWSIngressCapacityShedTestHarness(t, account, dialer, nil)

	dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
	clientConn, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(wsServer.URL, "http"), nil)
	cancelDial()
	require.NoError(t, err)
	defer func() { _ = clientConn.CloseNow() }()

	readTurnFrames := func(expected int) []string {
		out := make([]string, 0, expected)
		for len(out) < expected {
			readCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_, msg, readErr := clientConn.Read(readCtx)
			cancel()
			require.NoError(t, readErr)
			out = append(out, string(msg))
		}
		return out
	}

	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	require.NoError(t, clientConn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","store":false,"input":[{"type":"message","role":"user","content":"turn1"}]}`)))
	cancelWrite()
	turn1Frames := readTurnFrames(3)
	require.Equal(t, "resp_turn1", gjson.Get(turn1Frames[2], "response.id").String())

	writeCtx2, cancelWrite2 := context.WithTimeout(context.Background(), 3*time.Second)
	require.NoError(t, clientConn.Write(writeCtx2, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","store":false,"previous_response_id":"resp_turn1","input":[{"type":"message","role":"user","content":"turn2"}]}`)))
	cancelWrite2()
	turn2Frames := readTurnFrames(3)
	_ = clientConn.Close(coderws.StatusNormalClosure, "done")

	select {
	case serverErr := <-serverErrCh:
		require.NoError(t, serverErr)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for ingress server to finish")
	}

	require.Equal(t, 1, dialer.dialCount, "strict-affinity turn 2 retry must reuse the same upstream WS connection")
	require.Len(t, captureConn.writes, 3, "expected turn1 + turn2 initial + turn2 retry")
	require.Equal(t, "resp_turn1", captureConn.writes[2]["previous_response_id"])
	require.Equal(t, "resp_turn2_ok", gjson.Get(turn2Frames[0], "response.id").String())
	require.Equal(t, "turn2", gjson.Get(turn2Frames[1], "delta").String())
	require.Equal(t, "resp_turn2_ok", gjson.Get(turn2Frames[2], "response.id").String())
	require.NotContains(t, strings.Join(turn2Frames, "\n"), "resp_turn2_shed")
}

func TestProxyResponsesWebSocketFromClient_BareCapacityShedErrorWithoutResponseCreatedRedials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	prevDelay := openAIWSIngressCapacityShedRetryDelay
	openAIWSIngressCapacityShedRetryDelay = time.Millisecond
	t.Cleanup(func() { openAIWSIngressCapacityShedRetryDelay = prevDelay })

	conn1 := &openAIWSCaptureConn{
		events: [][]byte{
			[]byte(`{"type":"error","error":{"type":"service_unavailable_err","code":"slow_down","message":"Please slow down."}}`),
		},
	}
	conn2 := &openAIWSCaptureConn{
		events: [][]byte{
			[]byte(`{"type":"response.created","response":{"id":"resp_fresh","model":"gpt-5.1"}}`),
			[]byte(`{"type":"response.output_text.delta","delta":"recovered"}`),
			[]byte(`{"type":"response.completed","response":{"id":"resp_fresh","model":"gpt-5.1","usage":{"input_tokens":4,"output_tokens":2}}}`),
		},
	}
	dialer := &openAIWSQueueDialer{conns: []openAIWSClientConn{conn1, conn2}}
	account := Account{
		ID:          5412,
		Name:        "openai-ingress-bare-capacity-shed-redial",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-test-token"},
		Extra:       map[string]any{"responses_websockets_v2_enabled": true},
	}

	wsServer, serverErrCh := newOpenAIWSIngressCapacityShedTestHarness(t, account, dialer, nil)

	dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
	clientConn, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(wsServer.URL, "http"), nil)
	cancelDial()
	require.NoError(t, err)
	defer func() { _ = clientConn.CloseNow() }()

	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	require.NoError(t, clientConn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","input":"hi"}`)))
	cancelWrite()

	var frames []string
	for len(frames) < 3 {
		readCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, msg, readErr := clientConn.Read(readCtx)
		cancel()
		require.NoError(t, readErr)
		frames = append(frames, string(msg))
	}
	_ = clientConn.Close(coderws.StatusNormalClosure, "done")

	select {
	case serverErr := <-serverErrCh:
		require.NoError(t, serverErr)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for ingress server to finish")
	}

	require.Equal(t, 2, dialer.dialCount, "bare error without response.created should mark lease broken and dial a fresh conn")
	require.Equal(t, "recovered", gjson.Get(frames[1], "delta").String())
}

func TestProxyResponsesWebSocketFromClient_CapacityShedRetriesExhaustedDeliversRewrittenError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	prevLimit := openAIWSIngressCapacityShedRetryLimit
	prevDelay := openAIWSIngressCapacityShedRetryDelay
	openAIWSIngressCapacityShedRetryLimit = 2
	openAIWSIngressCapacityShedRetryDelay = time.Millisecond
	t.Cleanup(func() {
		openAIWSIngressCapacityShedRetryLimit = prevLimit
		openAIWSIngressCapacityShedRetryDelay = prevDelay
	})

	captureConn := &openAIWSCaptureConn{
		events: [][]byte{
			// Attempt 0
			[]byte(`{"type":"response.created","response":{"id":"resp_ex_0","model":"gpt-5.1"}}`),
			[]byte(`{"type":"response.failed","response":{"id":"resp_ex_0","status":"failed","error":{"code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}}`),
			// Attempt 1 (retry 1)
			[]byte(`{"type":"response.created","response":{"id":"resp_ex_1","model":"gpt-5.1"}}`),
			[]byte(`{"type":"response.failed","response":{"id":"resp_ex_1","status":"failed","error":{"code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}}`),
			// Attempt 2 (retry 2 — final, retries exhausted)
			[]byte(`{"type":"response.created","response":{"id":"resp_ex_2","model":"gpt-5.1"}}`),
			[]byte(`{"type":"error","error":{"type":"service_unavailable_err","code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}`),
			[]byte(`{"type":"response.failed","response":{"id":"resp_ex_2","status":"failed","error":{"code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}}`),
		},
	}
	dialer := &openAIWSCaptureDialer{conn: captureConn}
	account := Account{
		ID:          5413,
		Name:        "openai-ingress-capacity-retry-exhausted",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-test-token"},
		Extra:       map[string]any{"responses_websockets_v2_enabled": true},
	}

	wsServer, serverErrCh := newOpenAIWSIngressCapacityShedTestHarness(t, account, dialer, nil)

	dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
	clientConn, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(wsServer.URL, "http"), nil)
	cancelDial()
	require.NoError(t, err)
	defer func() { _ = clientConn.CloseNow() }()

	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	require.NoError(t, clientConn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","input":"hi"}`)))
	cancelWrite()

	var frames []string
	for len(frames) < 3 {
		readCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, msg, readErr := clientConn.Read(readCtx)
		cancel()
		require.NoError(t, readErr)
		frames = append(frames, string(msg))
	}
	_ = clientConn.Close(coderws.StatusNormalClosure, "done")

	select {
	case serverErr := <-serverErrCh:
		require.NoError(t, serverErr)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for ingress server to finish")
	}

	require.Len(t, captureConn.writes, 3, "expected 1 initial attempt + 2 retries")
	joined := strings.Join(frames, "\n")
	require.Contains(t, joined, "resp_ex_2")
	require.NotContains(t, joined, "resp_ex_0")
	require.NotContains(t, joined, "resp_ex_1")
	require.Contains(t, joined, `"code":"server_error"`)
	require.Contains(t, joined, "Our servers are currently overloaded. Please try again later.")
	require.NotContains(t, joined, "server_is_overloaded")
}
