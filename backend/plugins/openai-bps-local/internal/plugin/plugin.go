// Package plugin adapts the bps-local Responses→BPS bridge to the Sub2API
// OpenAI OAuth outbound transport contract. The host decides which accounts
// are bound to this plugin; only their requests ever reach Forward.
package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	pluginconfig "github.com/Wei-Shaw/sub2api/plugins/openai-bps-local/internal/config"
	"github.com/Wei-Shaw/sub2api/plugins/openai-bps-local/internal/proxy"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const (
	PluginID      = "local.sub2api.openai-bps-local"
	PluginVersion = "0.1.0"
	Capability    = "openai.oauth.outbound_transport.v1"

	chunkSize       = 32 * 1024
	maxProxyServers = 64
)

type forwardStream = grpc.BidiStreamingServer[pluginv1.ForwardRequest, pluginv1.ForwardResponse]

// runtimeState is replaced atomically on ApplyConfig.
type runtimeState struct {
	cfg    pluginconfig.Config
	root   *proxy.Server
	native *http.Transport

	mu      sync.Mutex
	servers map[string]*proxy.Server
	natives map[string]*http.Client
}

func (s *runtimeState) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, server := range s.servers {
		server.CloseIdleConnections()
	}
	for _, client := range s.natives {
		client.CloseIdleConnections()
	}
	s.root.CloseIdleConnections()
}

// serverFor returns the bridge bound to one outbound proxy. Per-proxy servers
// share the root replay cache, so tool replay survives proxy changes.
func (s *runtimeState) serverFor(proxyURL string) (*proxy.Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if server := s.servers[proxyURL]; server != nil {
		return server, nil
	}
	if len(s.servers) >= maxProxyServers {
		for key, server := range s.servers {
			server.CloseIdleConnections()
			delete(s.servers, key)
		}
	}
	server, err := s.root.ForProxy(proxyURL)
	if err != nil {
		return nil, err
	}
	s.servers[proxyURL] = server
	return server, nil
}

func (s *runtimeState) nativeClientFor(proxyURL string) (*http.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if client := s.natives[proxyURL]; client != nil {
		return client, nil
	}
	if len(s.natives) >= maxProxyServers {
		for key, client := range s.natives {
			client.CloseIdleConnections()
			delete(s.natives, key)
		}
	}
	transport := s.native.Clone()
	transport.Proxy = nil
	if proxyURL != "" {
		parsed, err := url.Parse(proxyURL)
		if err != nil || parsed.Host == "" {
			return nil, errors.New("账号代理地址无效")
		}
		transport.Proxy = http.ProxyURL(parsed)
	}
	client := &http.Client{
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	s.natives[proxyURL] = client
	return client, nil
}

type stats struct {
	total     atomic.Int64
	bridged   atomic.Int64
	native    atomic.Int64
	succeeded atomic.Int64
	failed    atomic.Int64
	lastCode  atomic.Int64
	lastMs    atomic.Int64
	lastError atomic.Value
}

// Plugin implements pluginv1.TransportPluginServer.
type Plugin struct {
	pluginv1.UnimplementedTransportPluginServer

	mu    sync.Mutex
	state atomic.Pointer[runtimeState]
	stats stats
}

// New returns a plugin initialised with the default configuration so it can
// serve before the host pushes ApplyConfig.
func New() *Plugin {
	p := &Plugin{}
	cfg, _, _ := pluginconfig.Parse(nil)
	if state, err := buildState(cfg); err == nil {
		p.state.Store(state)
	}
	p.stats.lastError.Store("")
	return p
}

func buildState(cfg pluginconfig.Config) (*runtimeState, error) {
	root, err := proxy.NewShared(proxy.Config{
		Upstream:          cfg.UpstreamURL,
		UpstreamTransport: cfg.UpstreamTransport,
		WSIdleTimeout:     cfg.WSIdleTimeout(),
		ToolsVersion:      cfg.ToolsVersion,
		ARCTextSession:    cfg.ARCTextSession,
		Headers:           cfg.ExtraHeaders,
	})
	if err != nil {
		return nil, err
	}
	return &runtimeState{
		cfg:  cfg,
		root: root,
		native: &http.Transport{
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          64,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 120 * time.Second,
		},
		servers: make(map[string]*proxy.Server),
		natives: make(map[string]*http.Client),
	}, nil
}

func (p *Plugin) GetInfo(context.Context, *pluginv1.GetInfoRequest) (*pluginv1.GetInfoResponse, error) {
	return &pluginv1.GetInfoResponse{
		PluginId:            PluginID,
		PluginVersion:       PluginVersion,
		ProtocolVersion:     pluginv1.ProtocolVersion,
		TransportApiVersion: pluginv1.TransportAPIVersion,
		Capabilities:        []string{Capability},
	}, nil
}

func (p *Plugin) Health(context.Context, *pluginv1.HealthRequest) (*pluginv1.HealthResponse, error) {
	state := p.state.Load()
	if state == nil {
		return &pluginv1.HealthResponse{Healthy: false, Message: "传输配置未初始化"}, nil
	}
	lastError, _ := p.stats.lastError.Load().(string)
	statusJSON, _ := json.Marshal(map[string]any{
		"upstream_url":       state.cfg.UpstreamURL,
		"upstream_transport": state.cfg.UpstreamTransport,
		"proxy_mode":         state.cfg.ProxyMode,
		"arc_text_session":   state.cfg.ARCTextSession,
		"native_passthrough": state.cfg.NativePassthroughEnabled(),
		"requests_total":     p.stats.total.Load(),
		"requests_bridged":   p.stats.bridged.Load(),
		"requests_native":    p.stats.native.Load(),
		"requests_succeeded": p.stats.succeeded.Load(),
		"requests_failed":    p.stats.failed.Load(),
		"last_status_code":   p.stats.lastCode.Load(),
		"last_latency_ms":    p.stats.lastMs.Load(),
		"last_error":         lastError,
	})
	return &pluginv1.HealthResponse{Healthy: true, Message: "bps-local 传输插件已就绪", StatusJson: string(statusJSON)}, nil
}

func (p *Plugin) ValidateConfig(_ context.Context, request *pluginv1.ValidateConfigRequest) (*pluginv1.ValidateConfigResponse, error) {
	if request == nil {
		return &pluginv1.ValidateConfigResponse{Valid: false, Message: "配置请求为空"}, nil
	}
	_, normalized, err := pluginconfig.Parse(request.ConfigJson)
	if err != nil {
		return &pluginv1.ValidateConfigResponse{Valid: false, Message: err.Error()}, nil
	}
	return &pluginv1.ValidateConfigResponse{Valid: true, NormalizedConfigJson: normalized}, nil
}

func (p *Plugin) ApplyConfig(_ context.Context, request *pluginv1.ApplyConfigRequest) (*pluginv1.ApplyConfigResponse, error) {
	if request == nil {
		return &pluginv1.ApplyConfigResponse{Applied: false, Message: "配置请求为空"}, nil
	}
	cfg, _, err := pluginconfig.Parse(request.ConfigJson)
	if err != nil {
		return &pluginv1.ApplyConfigResponse{Applied: false, Message: err.Error()}, nil
	}
	state, err := buildState(cfg)
	if err != nil {
		return &pluginv1.ApplyConfigResponse{Applied: false, Message: err.Error()}, nil
	}
	p.mu.Lock()
	old := p.state.Swap(state)
	p.mu.Unlock()
	if old != nil {
		old.close()
	}
	return &pluginv1.ApplyConfigResponse{Applied: true, Message: "配置已应用"}, nil
}

func (p *Plugin) TestConfig(_ context.Context, request *pluginv1.TestConfigRequest) (*pluginv1.TestConfigResponse, error) {
	started := time.Now()
	if request == nil {
		return &pluginv1.TestConfigResponse{Success: false, Message: "配置请求为空"}, nil
	}
	cfg, _, err := pluginconfig.Parse(request.ConfigJson)
	if err == nil {
		_, err = buildState(cfg)
	}
	if err != nil {
		return &pluginv1.TestConfigResponse{Success: false, Message: err.Error(), LatencyMs: time.Since(started).Milliseconds()}, nil
	}
	statusJSON, _ := json.Marshal(map[string]any{
		"upstream_url":       cfg.UpstreamURL,
		"upstream_transport": cfg.UpstreamTransport,
		"proxy_mode":         cfg.ProxyMode,
		"network_probe":      false,
	})
	return &pluginv1.TestConfigResponse{
		Success:    true,
		Message:    "配置有效；实际请求使用宿主为已绑定 OAuth 账号解析出的身份（未发起网络探测）",
		LatencyMs:  time.Since(started).Milliseconds(),
		StatusJson: string(statusJSON),
	}, nil
}

// InitHostServices is accepted but unused: bps-local keeps its replay cache in
// process memory and needs no host KV.
func (p *Plugin) InitHostServices(context.Context, *pluginv1.InitHostServicesRequest) (*pluginv1.InitHostServicesResponse, error) {
	return &pluginv1.InitHostServicesResponse{Ready: true, Message: "插件不使用宿主服务"}, nil
}

func (p *Plugin) Forward(stream forwardStream) error {
	started := time.Now()
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	start := first.GetStart()
	if start == nil {
		return p.sendError(stream, "PLUGIN_INVALID_REQUEST", "缺少请求元数据", false)
	}
	p.stats.total.Add(1)
	if err := validateStart(start); err != nil {
		return p.sendError(stream, "PLUGIN_INVALID_REQUEST", err.Error(), false)
	}
	state := p.state.Load()
	if state == nil {
		return p.sendError(stream, "PLUGIN_NOT_CONFIGURED", "传输配置未初始化", false)
	}
	incoming, err := url.Parse(start.Url)
	if err != nil || incoming.Host == "" || (incoming.Scheme != "http" && incoming.Scheme != "https") || incoming.User != nil {
		return p.sendError(stream, "PLUGIN_TARGET_INVALID", "请求 URL 无效", false)
	}
	limit, err := pluginv1.RequestBodyLimit(stream.Context())
	if err != nil {
		return p.sendError(stream, "PLUGIN_REQUEST_INVALID", err.Error(), false)
	}
	body, err := readRequestBody(stream, start, limit)
	if err != nil {
		return p.sendError(stream, "PLUGIN_REQUEST_BODY_FAILED", err.Error(), false)
	}
	headers := headersFromProto(start.Headers)
	proxyURL := ""
	if state.cfg.ProxyMode == pluginconfig.ProxyAccount {
		proxyURL = strings.TrimSpace(start.ProxyUrl)
	}

	if start.Method != http.MethodPost || !strings.HasSuffix(strings.TrimRight(incoming.Path, "/"), "/responses") {
		if !state.cfg.NativePassthroughEnabled() {
			return p.sendError(stream, "PLUGIN_UNSUPPORTED_ENDPOINT", "此插件仅桥接 POST /responses；原生透传已关闭", false)
		}
		p.stats.native.Add(1)
		return p.forwardNative(stream, state, start, incoming, headers, body, proxyURL, started)
	}
	if value := headers.Get("Content-Encoding"); value != "" && !strings.EqualFold(value, "identity") {
		return p.sendError(stream, "PLUGIN_REQUEST_INVALID", "桥接请求必须是未压缩 JSON", false)
	}

	credentials, err := credentialsFromHeaders(headers)
	if err != nil {
		return p.sendError(stream, "PLUGIN_AUTH_MISSING", err.Error(), false)
	}
	server, err := state.serverFor(proxyURL)
	if err != nil {
		return p.sendError(stream, "PLUGIN_PROXY_INVALID", "账号代理地址无效", false)
	}

	ctx := stream.Context()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://bps-local.invalid/v1/responses", bytes.NewReader(body))
	if err != nil {
		return p.sendError(stream, "PLUGIN_REQUEST_INVALID", "无法构造桥接请求", false)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("session_id", replayScope(ctx, start, headers))

	p.stats.bridged.Add(1)
	writer := newStreamWriter(stream)
	server.ServeResponses(writer, request, credentials)
	return p.finish(stream, writer, started)
}

// finish closes a bridged response. A handler that wrote nothing (client gone)
// still needs a terminal frame so the host never waits on a silent stream.
func (p *Plugin) finish(stream forwardStream, writer *streamWriter, started time.Time) error {
	elapsed := time.Since(started).Milliseconds()
	p.stats.lastMs.Store(elapsed)
	if writer.err != nil {
		p.recordFailure("STREAM_SEND_FAILED")
		return writer.err
	}
	if !writer.started {
		if err := stream.Context().Err(); err != nil {
			p.recordFailure("CLIENT_CANCELED")
			return err
		}
		return p.sendError(stream, "PLUGIN_EMPTY_RESPONSE", "桥接未返回响应", true)
	}
	p.stats.lastCode.Store(int64(writer.status))
	if writer.status >= 200 && writer.status < 300 {
		p.stats.succeeded.Add(1)
	} else {
		p.recordFailure("HTTP_" + strconv.Itoa(writer.status))
	}
	return stream.Send(&pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_End{End: &pluginv1.ForwardResponseEnd{
		BytesReceived: writer.bytes, DurationMs: elapsed,
	}}})
}

// forwardNative relays the host-built request unchanged (headers, body, URL)
// for endpoints the bridge does not translate, such as remote compaction.
func (p *Plugin) forwardNative(stream forwardStream, state *runtimeState, start *pluginv1.ForwardRequestStart, target *url.URL, headers http.Header, body []byte, proxyURL string, started time.Time) error {
	client, err := state.nativeClientFor(proxyURL)
	if err != nil {
		return p.sendError(stream, "PLUGIN_PROXY_INVALID", err.Error(), false)
	}
	var reader io.Reader = http.NoBody
	if start.HasBody {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(stream.Context(), start.Method, target.String(), reader)
	if err != nil {
		return p.sendError(stream, "PLUGIN_REQUEST_INVALID", "无法构造原生请求", false)
	}
	for key, values := range headers {
		if isHopByHopHeader(key) {
			continue
		}
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	if start.Host != "" {
		request.Host = start.Host
	}
	response, err := client.Do(request)
	if err != nil {
		if ctxErr := stream.Context().Err(); ctxErr != nil {
			return ctxErr
		}
		return p.sendError(stream, "UPSTREAM_REQUEST_FAILED", "原生上游请求失败", true)
	}
	defer func() { _ = response.Body.Close() }()
	responseHeaders := make(http.Header, len(response.Header))
	for key, values := range response.Header {
		if !isHopByHopHeader(key) {
			responseHeaders[key] = append([]string(nil), values...)
		}
	}
	writer := newStreamWriter(stream)
	for key, values := range responseHeaders {
		writer.Header()[key] = values
	}
	writer.proto, writer.protoMajor, writer.protoMinor = response.Proto, response.ProtoMajor, response.ProtoMinor
	writer.contentLength = response.ContentLength
	writer.WriteHeader(response.StatusCode)
	buffer := make([]byte, chunkSize)
	for writer.err == nil {
		n, readErr := response.Body.Read(buffer)
		if n > 0 {
			_, _ = writer.Write(buffer[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			if ctxErr := stream.Context().Err(); ctxErr != nil {
				return ctxErr
			}
			p.recordFailure("UPSTREAM_BODY_FAILED")
			return fmt.Errorf("读取原生上游响应失败: %w", readErr)
		}
	}
	return p.finish(stream, writer, started)
}

func (p *Plugin) recordFailure(code string) {
	p.stats.failed.Add(1)
	p.stats.lastError.Store(code)
}

func (p *Plugin) sendError(stream forwardStream, code, message string, requestSent bool) error {
	p.recordFailure(code)
	return stream.Send(&pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_Error{Error: &pluginv1.ForwardResponseError{
		Code: code, Message: message, RequestSent: requestSent,
	}}})
}

// credentialsFromHeaders extracts the identity the host already authorised for
// the bound account. The account id may be absent; bps-local then derives it
// from the access token claims.
func credentialsFromHeaders(headers http.Header) (proxy.StaticCredentials, error) {
	authorization := strings.TrimSpace(headers.Get("Authorization"))
	scheme, token, ok := strings.Cut(authorization, " ")
	token = strings.TrimSpace(token)
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return proxy.StaticCredentials{}, errors.New("宿主请求缺少 OAuth Bearer 授权")
	}
	accountID := strings.TrimSpace(headers.Get("Chatgpt-Account-Id"))
	if accountID == "" {
		accountID = strings.TrimSpace(headers.Get("X-Openai-Account-Id"))
	}
	return proxy.StaticCredentials{Token: token, AccountID: accountID}, nil
}

// replayScope isolates bps-local's tool replay cache per host account and
// client session. Without a session the scope degrades to the account.
func replayScope(ctx context.Context, start *pluginv1.ForwardRequestStart, headers http.Header) string {
	session := ""
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if values := md.Get(pluginv1.ClientSessionIDMetadataKey); len(values) > 0 {
			session = strings.TrimSpace(values[0])
		}
	}
	for _, key := range []string{"session_id", "Session-Id", "conversation_id"} {
		if session != "" {
			break
		}
		session = strings.TrimSpace(headers.Get(key))
	}
	if len(session) > 256 {
		session = session[:256]
	}
	return "acct:" + strconv.FormatInt(start.AccountId, 10) + "|" + session
}

func validateStart(start *pluginv1.ForwardRequestStart) error {
	if start.AccountId <= 0 || start.Platform != "openai" || start.AccountType != "oauth" {
		return errors.New("请求账号必须是 OpenAI OAuth 账号")
	}
	if start.Method == "" || start.Url == "" {
		return errors.New("请求方法或 URL 为空")
	}
	return nil
}

func readRequestBody(stream forwardStream, start *pluginv1.ForwardRequestStart, limit int64) ([]byte, error) {
	if limit <= 0 {
		return nil, errors.New("宿主 gateway.max_body_size 请求体限制无效")
	}
	if start.ContentLength > limit {
		return nil, fmt.Errorf("请求体超过宿主 gateway.max_body_size 限制（%d 字节）", limit)
	}
	var body bytes.Buffer
	for {
		frame, err := stream.Recv()
		if err != nil {
			return nil, errors.New("请求体在 body_end 前中断")
		}
		switch value := frame.Frame.(type) {
		case *pluginv1.ForwardRequest_BodyChunk:
			if !start.HasBody {
				return nil, errors.New("请求体帧与 HasBody 不一致")
			}
			if int64(len(value.BodyChunk)) > limit-int64(body.Len()) {
				return nil, fmt.Errorf("请求体超过宿主 gateway.max_body_size 限制（%d 字节）", limit)
			}
			_, _ = body.Write(value.BodyChunk)
		case *pluginv1.ForwardRequest_BodyEnd:
			if !value.BodyEnd || (start.ContentLength > 0 && start.ContentLength != int64(body.Len())) {
				return nil, errors.New("请求体长度或 body_end 无效")
			}
			return body.Bytes(), nil
		default:
			return nil, errors.New("请求体帧顺序无效")
		}
	}
}

func headersFromProto(input map[string]*pluginv1.HeaderValues) http.Header {
	output := make(http.Header, len(input))
	for key, values := range input {
		if values == nil || strings.TrimSpace(key) == "" {
			continue
		}
		for _, value := range values.Values {
			output.Add(key, value)
		}
	}
	return output
}

func headersToProto(input http.Header) map[string]*pluginv1.HeaderValues {
	output := make(map[string]*pluginv1.HeaderValues, len(input))
	for key, values := range input {
		output[key] = &pluginv1.HeaderValues{Values: append([]string(nil), values...)}
	}
	return output
}

func isHopByHopHeader(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "connection", "proxy-connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade", "host", "content-length":
		return true
	default:
		return false
	}
}
