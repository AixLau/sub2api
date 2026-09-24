package transport

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/Wei-Shaw/sub2api/plugins/openai-basispoints-transport/internal/bridge"
	pluginconfig "github.com/Wei-Shaw/sub2api/plugins/openai-basispoints-transport/internal/config"
	hcplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
)

const (
	PluginID      = "local.sub2api.openai-transport"
	PluginVersion = "0.2.0"
	Capability    = "openai.oauth.outbound_transport.v1"
	chunkSize     = 32 * 1024
)

type proxyContextKey struct{}

type runtimeState struct {
	cfg       pluginconfig.Config
	client    *http.Client
	transport *http.Transport
}

type requestStats struct {
	total     atomic.Uint64
	succeeded atomic.Uint64
	failed    atomic.Uint64
	lastCode  atomic.Int64
	lastMs    atomic.Int64
}

// Plugin implements the public Sub2API transport protocol. It never accepts a
// refresh token and never refreshes or stores an OAuth credential itself.
type Plugin struct {
	pluginv1.UnimplementedTransportPluginServer

	state atomic.Pointer[runtimeState]
	mu    sync.Mutex

	brokerMu sync.RWMutex
	broker   *hcplugin.GRPCBroker
	hostConn *grpc.ClientConn
	host     pluginv1.HostServiceClient

	stats           requestStats
	lastBridgeError atomic.Value
	omittedTools    atomic.Value
}

func New() *Plugin {
	p := &Plugin{}
	state, err := buildRuntimeState(pluginconfig.Defaults())
	if err != nil {
		panic(err)
	}
	p.state.Store(state)
	return p
}

func (p *Plugin) SetHostBroker(broker *hcplugin.GRPCBroker) {
	p.brokerMu.Lock()
	p.broker = broker
	p.brokerMu.Unlock()
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
	statusJSON, _ := json.Marshal(map[string]any{
		"upstream_base_url":    state.cfg.UpstreamBaseURL,
		"proxy_mode":           state.cfg.ProxyMode,
		"host_kv":              p.hostClient() != nil,
		"tool_bridge":          "run_officejs-v1",
		"requests_total":       p.stats.total.Load(),
		"requests_succeeded":   p.stats.succeeded.Load(),
		"requests_failed":      p.stats.failed.Load(),
		"last_status_code":     p.stats.lastCode.Load(),
		"last_latency_ms":      p.stats.lastMs.Load(),
		"last_bridge_error":    p.lastBridgeError.Load(),
		"omitted_hosted_tools": p.omittedTools.Load(),
	})
	return &pluginv1.HealthResponse{Healthy: true, Message: "OpenAI OAuth 传输插件已就绪", StatusJson: string(statusJSON)}, nil
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
	state, err := buildRuntimeState(cfg)
	if err != nil {
		return &pluginv1.ApplyConfigResponse{Applied: false, Message: err.Error()}, nil
	}
	p.mu.Lock()
	old := p.state.Swap(state)
	p.mu.Unlock()
	if old != nil && old.transport != nil {
		old.transport.CloseIdleConnections()
	}
	return &pluginv1.ApplyConfigResponse{Applied: true, Message: "配置已应用"}, nil
}

func (p *Plugin) TestConfig(_ context.Context, request *pluginv1.TestConfigRequest) (*pluginv1.TestConfigResponse, error) {
	started := time.Now()
	if request == nil {
		return &pluginv1.TestConfigResponse{Success: false, Message: "配置请求为空"}, nil
	}
	cfg, _, err := pluginconfig.Parse(request.ConfigJson)
	if err != nil {
		return &pluginv1.TestConfigResponse{Success: false, Message: err.Error(), LatencyMs: time.Since(started).Milliseconds()}, nil
	}
	statusJSON, _ := json.Marshal(map[string]any{
		"upstream_base_url": cfg.UpstreamBaseURL,
		"auth_mode":         cfg.AuthMode,
		"proxy_mode":        cfg.ProxyMode,
		"network_probe":     false,
	})
	return &pluginv1.TestConfigResponse{
		Success:    true,
		Message:    "配置有效；实际请求将使用宿主为绑定 OAuth 账号解析出的身份",
		LatencyMs:  time.Since(started).Milliseconds(),
		StatusJson: string(statusJSON),
	}, nil
}

func (p *Plugin) InitHostServices(ctx context.Context, request *pluginv1.InitHostServicesRequest) (*pluginv1.InitHostServicesResponse, error) {
	if request == nil || request.HostServiceId == 0 || request.HostServiceApiVersion < 1 {
		return &pluginv1.InitHostServicesResponse{Ready: false, Message: "宿主服务标识无效"}, nil
	}
	p.brokerMu.RLock()
	broker := p.broker
	p.brokerMu.RUnlock()
	if broker == nil {
		return &pluginv1.InitHostServicesResponse{Ready: false, Message: "宿主 broker 未注入"}, nil
	}
	conn, err := broker.Dial(request.HostServiceId)
	if err != nil {
		return &pluginv1.InitHostServicesResponse{Ready: false, Message: "连接宿主服务失败"}, nil
	}
	client := pluginv1.NewHostServiceClient(conn)
	p.brokerMu.Lock()
	old := p.hostConn
	p.hostConn = conn
	p.host = client
	p.brokerMu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	return &pluginv1.InitHostServicesResponse{Ready: true, Message: "宿主 KV 服务已连接"}, nil
}

func (p *Plugin) Forward(stream grpc.BidiStreamingServer[pluginv1.ForwardRequest, pluginv1.ForwardResponse]) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	start := first.GetStart()
	if start == nil {
		return p.sendError(stream, "PLUGIN_INVALID_REQUEST", "缺少请求元数据", false)
	}
	if err := validateStart(start); err != nil {
		return p.sendError(stream, "PLUGIN_INVALID_REQUEST", err.Error(), false)
	}
	state := p.state.Load()
	target, err := resolveTarget(start.Url, state.cfg)
	if err != nil {
		return p.sendError(stream, "PLUGIN_TARGET_INVALID", err.Error(), false)
	}
	// This adapter implements only Responses creation. It must not silently
	// send compact/images/count requests to an unverified endpoint.
	if start.Method != http.MethodPost || !strings.HasSuffix(target.Path, "/responses") {
		return p.sendError(stream, "PLUGIN_UNSUPPORTED_ENDPOINT", "此插件目前仅支持 POST /responses", false)
	}
	body, err := readRequestBody(stream, start)
	if err != nil {
		return p.sendError(stream, "PLUGIN_REQUEST_BODY_FAILED", err.Error(), false)
	}
	headers := headersFromProto(start.Headers)
	if value := headers.Get("Content-Encoding"); value != "" && value != "identity" {
		return p.sendError(stream, "PLUGIN_REQUEST_INVALID", "桥接请求必须是未压缩 JSON", false)
	}
	var inputMeta struct {
		PromptCacheKey string `json:"prompt_cache_key"`
	}
	_ = json.Unmarshal(body, &inputMeta)
	session := ""
	for _, key := range []string{"session_id", "session-id", "conversation_id"} {
		if value := headers.Get(key); value != "" {
			session = value
			break
		}
	}
	if session == "" {
		session = inputMeta.PromptCacheKey
	}
	scope := bridge.Scope(strconv.FormatInt(start.AccountId, 10), state.cfg.UpstreamBaseURL, session)
	var store bridge.Store
	if client := p.hostClient(); client != nil {
		store = hostStateStore{client: client}
	}
	adapted, err := bridge.Prepare(stream.Context(), body, scope, store)
	if err != nil {
		return p.sendError(stream, "TOOL_BRIDGE_REQUEST_INVALID", err.Error(), false)
	}
	p.omittedTools.Store(strings.Join(adapted.OmittedTools, ", "))
	// The host has already authorized this exact outbound request. Do not mint
	// another token or query unrelated accounts through HostService.
	identity := outboundIdentity{headers: headers, proxy: start.ProxyUrl}
	request, err := p.buildRequest(stream.Context(), start, target, state.cfg, identity, io.NopCloser(bytes.NewReader(adapted.Body)))
	if err != nil {
		return p.sendError(stream, "PLUGIN_REQUEST_INVALID", err.Error(), false)
	}
	request.ContentLength = int64(len(adapted.Body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept-Encoding", "identity")
	requestCtx, cancel := context.WithTimeout(request.Context(), time.Duration(state.cfg.RequestTimeoutSeconds)*time.Second)
	defer cancel()
	request = request.WithContext(requestCtx)
	p.stats.total.Add(1)
	started := time.Now()
	succeeded := false
	defer func() {
		p.stats.lastMs.Store(time.Since(started).Milliseconds())
		if succeeded {
			p.stats.succeeded.Add(1)
		} else {
			p.stats.failed.Add(1)
		}
	}()
	response, err := state.client.Do(request)
	if err != nil {
		return p.sendError(stream, "UPSTREAM_REQUEST_FAILED", "上游连接失败或超时", true)
	}
	defer response.Body.Close()
	p.stats.lastCode.Store(int64(response.StatusCode))
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		response.Header.Del("Content-Length")
		response.Header.Del("ETag")
		response.ContentLength = -1
		if !strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
			raw, readErr := io.ReadAll(io.LimitReader(response.Body, bridge.MaxBodyBytes+1))
			if readErr != nil || len(raw) > bridge.MaxBodyBytes {
				return p.sendError(stream, "UPSTREAM_RESPONSE_FAILED", "上游响应读取失败或过大", true)
			}
			converted, convertErr := adapted.Response(requestCtx, raw)
			if convertErr != nil {
				return p.sendError(stream, "TOOL_BRIDGE_RESPONSE_FAILED", convertErr.Error(), true)
			}
			response.Body.Close()
			response.Body = io.NopCloser(bytes.NewReader(converted))
			response.ContentLength = int64(len(converted))
		}
	}
	if err := stream.Send(&pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_Start{Start: responseStart(response)}}); err != nil {
		return err
	}
	var received int64
	emit := func(data []byte) error {
		for len(data) > 0 {
			n := min(len(data), chunkSize)
			chunk := append([]byte(nil), data[:n]...)
			if err := stream.Send(&pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_BodyChunk{BodyChunk: chunk}}); err != nil {
				return err
			}
			received += int64(n)
			data = data[n:]
		}
		return nil
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 && strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
		if err := adapted.Stream(requestCtx, response.Body, emit); err != nil {
			return p.sendError(stream, "TOOL_BRIDGE_STREAM_FAILED", err.Error(), true)
		}
	} else {
		buffer := make([]byte, chunkSize)
		for {
			n, err := response.Body.Read(buffer)
			if n > 0 {
				if sendErr := emit(buffer[:n]); sendErr != nil {
					return sendErr
				}
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				return p.sendError(stream, "UPSTREAM_RESPONSE_FAILED", "读取上游响应失败", true)
			}
		}
	}
	succeeded = response.StatusCode >= 200 && response.StatusCode < 300 && !adapted.Failed
	return stream.Send(&pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_End{End: &pluginv1.ForwardResponseEnd{BytesReceived: received, DurationMs: time.Since(started).Milliseconds()}}})
}

type outboundIdentity struct {
	token   string
	headers http.Header
	proxy   string
}

func (p *Plugin) buildRequest(ctx context.Context, start *pluginv1.ForwardRequestStart, target *url.URL, cfg pluginconfig.Config, identity outboundIdentity, body io.ReadCloser) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, start.Method, target.String(), body)
	if err != nil {
		return nil, err
	}
	request.ContentLength = start.ContentLength
	if !start.HasBody {
		request.Body = http.NoBody
		request.ContentLength = 0
	}
	for key, values := range headersFromProto(start.Headers) {
		if isHopByHopOrProtectedHeader(key) {
			continue
		}
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	for key, values := range identity.headers {
		if isHopByHopHeader(key) {
			continue
		}
		request.Header.Del(key)
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	if strings.TrimSpace(identity.token) != "" {
		request.Header.Set("Authorization", "Bearer "+identity.token)
	}
	accountID := strings.TrimSpace(request.Header.Get("chatgpt-account-id"))
	if accountID == "" {
		accountID = strings.TrimSpace(request.Header.Get("x-openai-account-id"))
	}
	if accountID != "" {
		request.Header.Set("chatgpt-account-id", accountID)
		request.Header.Set("x-openai-account-id", accountID)
	}
	{
		if request.Header.Get("Authorization") == "" || accountID == "" {
			return nil, errors.New("Basis Points 上游需要 OAuth 授权和 ChatGPT 账号 ID")
		}
		request.Header.Set("x-basispoints-auth-mode", cfg.AuthMode)
	}
	for key, value := range cfg.ExtraHeaders {
		request.Header.Set(key, value)
	}
	if cfg.ProxyMode == "account" {
		proxy := strings.TrimSpace(identity.proxy)
		if proxy == "" {
			proxy = strings.TrimSpace(start.ProxyUrl)
		}
		if proxy != "" {
			parsed, err := url.Parse(proxy)
			if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				return nil, errors.New("账号代理地址无效")
			}
			request = request.WithContext(context.WithValue(request.Context(), proxyContextKey{}, proxy))
		}
	}
	return request, nil
}

func buildRuntimeState(cfg pluginconfig.Config) (*runtimeState, error) {
	minTLS, err := pluginconfig.TLSVersion(cfg.TLSMinVersion)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		Proxy: func(request *http.Request) (*url.URL, error) {
			proxy, _ := request.Context().Value(proxyContextKey{}).(string)
			if proxy == "" {
				return nil, nil
			}
			return url.Parse(proxy)
		},
		TLSClientConfig:       &tls.Config{MinVersion: minTLS, Renegotiation: tls.RenegotiateNever},
		ResponseHeaderTimeout: time.Duration(cfg.ResponseHeaderTimeoutSeconds) * time.Second,
		IdleConnTimeout:       time.Duration(cfg.IdleConnectionTimeoutSeconds) * time.Second,
		MaxIdleConns:          cfg.MaxIdleConnections,
		MaxIdleConnsPerHost:   cfg.MaxIdleConnectionsPerHost,
		ForceAttemptHTTP2:     cfg.EnableHTTP2,
		DisableCompression:    false,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}
	return &runtimeState{cfg: cfg, transport: transport, client: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (p *Plugin) hostClient() pluginv1.HostServiceClient {
	p.brokerMu.RLock()
	defer p.brokerMu.RUnlock()
	return p.host
}

// Read the bounded JSON body before dialing upstream. This avoids an upload
// goroutine blocked in Recv when an upstream rejects a request before body_end.
func readRequestBody(stream grpc.BidiStreamingServer[pluginv1.ForwardRequest, pluginv1.ForwardResponse], start *pluginv1.ForwardRequestStart) ([]byte, error) {
	if start.ContentLength > bridge.MaxBodyBytes {
		return nil, errors.New("请求体超过桥接大小限制")
	}
	var body bytes.Buffer
	for {
		frame, err := stream.Recv()
		if err != nil {
			return nil, errors.New("请求体在 body_end 前中断")
		}
		switch value := frame.Frame.(type) {
		case *pluginv1.ForwardRequest_BodyChunk:
			if !start.HasBody || len(value.BodyChunk) > bridge.MaxBodyBytes-body.Len() {
				return nil, errors.New("请求体帧无效或超过大小限制")
			}
			body.Write(value.BodyChunk)
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

func validateStart(start *pluginv1.ForwardRequestStart) error {
	if start.AccountId <= 0 || start.Platform != "openai" || start.AccountType != "oauth" {
		return errors.New("请求账号必须是 OpenAI OAuth 账号")
	}
	if start.Method == "" || start.Url == "" {
		return errors.New("请求方法或 URL 为空")
	}
	return nil
}

func resolveTarget(raw string, cfg pluginconfig.Config) (*url.URL, error) {
	incoming, err := url.Parse(raw)
	if err != nil || incoming.Scheme == "" || incoming.Host == "" || incoming.User != nil || (incoming.Scheme != "http" && incoming.Scheme != "https") {
		return nil, errors.New("请求 URL 无效")
	}
	base, err := url.Parse(cfg.UpstreamBaseURL)
	if err != nil {
		return nil, errors.New("上游 Base URL 无效")
	}
	suffix := basisPointsPathSuffix(incoming.Path)
	base.Path = strings.TrimRight(base.Path, "/") + suffix
	base.RawPath = ""
	base.RawQuery = incoming.RawQuery
	base.Fragment = ""
	return base, nil
}

func basisPointsPathSuffix(path string) string {
	if index := strings.Index(path, "/responses"); index >= 0 {
		return path[index:]
	}
	if strings.HasPrefix(path, "/v1/") {
		return strings.TrimPrefix(path, "/v1")
	}
	if path == "/v1" {
		return "/"
	}
	if path == "" {
		return "/responses"
	}
	return path
}

func responseStart(response *http.Response) *pluginv1.ForwardResponseStart {
	return &pluginv1.ForwardResponseStart{
		StatusCode:    int32(response.StatusCode),
		Status:        response.Status,
		Protocol:      response.Proto,
		ProtocolMajor: int32(response.ProtoMajor),
		ProtocolMinor: int32(response.ProtoMinor),
		Headers:       headersToProto(response.Header),
		ContentLength: response.ContentLength,
	}
}

func (p *Plugin) sendError(stream grpc.BidiStreamingServer[pluginv1.ForwardRequest, pluginv1.ForwardResponse], code, message string, requestSent bool) error {
	p.lastBridgeError.Store(code)
	return stream.Send(&pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_Error{Error: &pluginv1.ForwardResponseError{
		Code: code, Message: message, RequestSent: requestSent,
	}}})
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

func isHopByHopOrProtectedHeader(key string) bool {
	return isHopByHopHeader(key) || strings.EqualFold(strings.TrimSpace(key), "authorization")
}
