package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// PluginHookRejectedError is returned when an enabled outbound hook explicitly
// rejects a request. Transport/runtime failures remain fail-open at the host
// boundary so a ticket outage cannot silently replace the native transport.
type PluginHookRejectedError struct {
	Code    string
	Message string
}

func (e *PluginHookRejectedError) Error() string {
	if e == nil {
		return "插件出站钩子拒绝请求"
	}
	code := strings.TrimSpace(e.Code)
	if code == "" {
		return "插件出站钩子拒绝请求: " + strings.TrimSpace(e.Message)
	}
	return fmt.Sprintf("插件出站钩子拒绝请求 [%s]: %s", code, strings.TrimSpace(e.Message))
}

const (
	// Ticket plugins are deliberately limited to this header. Authorization,
	// cookies, account identity, proxy and billing headers remain host-owned.
	pluginOutboundTicketHeader = "x-codex-turn-state"
)

func (route *pluginHookRoute) matchesAccount(account *Account) bool {
	if route == nil || account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth {
		return false
	}
	if route.allAccounts {
		return true
	}
	_, selected := route.accountIDs[account.ID]
	return selected
}

func hasEnabledOpenAIHookBinding(bindings []PluginBinding) bool {
	for _, binding := range bindings {
		if binding.Enabled && binding.Capability == PluginCapabilityOpenAICodexTicketHook &&
			binding.Platform == PlatformOpenAI && binding.AccountType == AccountTypeOAuth {
			return true
		}
	}
	return false
}

func hookBindingAccountIDs(bindings []PluginBinding) []int64 {
	for _, binding := range bindings {
		if binding.Capability == PluginCapabilityOpenAICodexTicketHook {
			return append([]int64(nil), binding.AccountIDs...)
		}
	}
	return nil
}

func (m *PluginManager) publishUnavailableHookRoute(pluginID int64, accountIDs []int64, allAccounts bool, message string) {
	m.mu.Lock()
	stale := make([]*pluginRuntime, 0, len(m.hookRuntimes))
	for id, runtime := range m.hookRuntimes {
		runtime.draining.Store(true)
		stale = append(stale, runtime)
		delete(m.hookRuntimes, id)
	}
	m.hookRoute.Store(&pluginHookRoute{
		pluginID: pluginID, accountIDs: pluginAccountIDSet(accountIDs), allAccounts: allAccounts, unavailable: message,
	})
	m.mu.Unlock()
	for _, runtime := range stale {
		runtime.drain(10 * time.Second)
	}
}

func (m *PluginManager) publishHookRuntimeLocked(installation *PluginInstallation, runtime *pluginRuntime) {
	if m.hookRuntimes == nil {
		m.hookRuntimes = make(map[int64]*pluginRuntime)
	}
	if old := m.hookRuntimes[installation.ID]; old != nil && old != runtime {
		old.draining.Store(true)
	}
	m.hookRuntimes[installation.ID] = runtime
	m.hookRoute.Store(&pluginHookRoute{
		pluginID: installation.ID, runtime: runtime,
		accountIDs: pluginAccountIDSet(hookBindingAccountIDs(installation.Bindings)),
	})
}

func (m *PluginManager) removeHookRuntimeLocked(id int64) *pluginRuntime {
	runtime := m.hookRuntimes[id]
	delete(m.hookRuntimes, id)
	if route := m.hookRoute.Load(); route != nil && route.pluginID == id {
		m.hookRoute.Store(nil)
	}
	return runtime
}

func (m *PluginManager) updateHookRouteAccounts(pluginID int64, accountIDs []int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.hookRoute.Load()
	if current == nil || current.pluginID != pluginID || (!current.allAccounts && samePluginAccountIDs(current.accountIDs, accountIDs)) {
		return
	}
	updated := *current
	updated.accountIDs = pluginAccountIDSet(accountIDs)
	updated.allAccounts = false
	m.hookRoute.Store(&updated)
}

func (m *PluginManager) reconcileHookRoutes(ctx context.Context, installations []*PluginInstallation) error {
	m.mu.Lock()
	if m.hookRuntimes == nil {
		m.hookRuntimes = make(map[int64]*pluginRuntime)
	}
	m.mu.Unlock()
	var enabled *PluginInstallation
	for _, installation := range installations {
		if !hasEnabledOpenAIHookBinding(installation.Bindings) {
			continue
		}
		if enabled != nil {
			err := errors.New("检测到多个 OpenAI Codex ticket 插件同时启用")
			m.publishUnavailableHookRoute(enabled.ID, hookBindingAccountIDs(enabled.Bindings), false, err.Error())
			return err
		}
		enabled = installation
	}
	if enabled == nil {
		m.mu.Lock()
		stale := make([]*pluginRuntime, 0, len(m.hookRuntimes))
		for id, runtime := range m.hookRuntimes {
			runtime.draining.Store(true)
			stale = append(stale, runtime)
			delete(m.hookRuntimes, id)
		}
		m.hookRoute.Store(nil)
		m.mu.Unlock()
		for _, runtime := range stale {
			runtime.drain(10 * time.Second)
		}
		return nil
	}

	accountIDs := hookBindingAccountIDs(enabled.Bindings)
	if len(accountIDs) == 0 {
		err := errors.New("已启用的 OpenAI Codex ticket 插件未绑定账号")
		m.publishUnavailableHookRoute(enabled.ID, nil, false, err.Error())
		return err
	}
	current := m.hookRoute.Load()
	if current != nil && current.pluginID == enabled.ID && current.runtime != nil &&
		!current.runtime.client.Exited() && current.runtime.installation.BinarySHA256 == enabled.BinarySHA256 &&
		current.runtime.installation.ConfigEncrypted == enabled.ConfigEncrypted {
		healthCtx, cancel := context.WithTimeout(ctx, pluginHealthTimeout)
		healthErr := current.runtime.checkHealth(healthCtx)
		cancel()
		if healthErr != nil {
			m.publishUnavailableHookRoute(enabled.ID, accountIDs, false, healthErr.Error())
			return healthErr
		}
		m.updateHookRouteAccounts(enabled.ID, accountIDs)
		current.runtime.updateAccountScope(accountIDs)
		return nil
	}
	if enabled.State == PluginStateStarting && !m.startingStateExpired(enabled) {
		if current == nil {
			m.hookRoute.Store(&pluginHookRoute{pluginID: enabled.ID, accountIDs: pluginAccountIDSet(accountIDs), unavailable: "插件正在其他实例中启动"})
		}
		return nil
	}
	local, err := m.ensureLocalInstallation(ctx, enabled)
	if err != nil {
		m.publishUnavailableHookRoute(enabled.ID, accountIDs, false, err.Error())
		return err
	}
	runtime, err := m.prepareRuntime(ctx, local, true)
	if err != nil {
		m.publishUnavailableHookRoute(enabled.ID, accountIDs, false, err.Error())
		return err
	}
	latest, err := m.repo.GetByID(ctx, enabled.ID)
	if err != nil {
		runtime.kill()
		return err
	}
	if !hasEnabledOpenAIHookBinding(latest.Bindings) || latest.BinarySHA256 != enabled.BinarySHA256 || latest.ConfigEncrypted != enabled.ConfigEncrypted ||
		!samePluginAccountIDs(pluginAccountIDSet(hookBindingAccountIDs(latest.Bindings)), accountIDs) {
		runtime.kill()
		return nil
	}
	if err := m.repo.MarkRuntimeHealthy(ctx, enabled.ID, enabled.BinarySHA256, enabled.ConfigEncrypted); err != nil {
		runtime.kill()
		if errors.Is(err, ErrPluginStateChanged) {
			return nil
		}
		return err
	}
	m.mu.Lock()
	stale := m.hookRuntimes[enabled.ID]
	m.hookRuntimes[enabled.ID] = runtime
	m.hookRoute.Store(&pluginHookRoute{pluginID: enabled.ID, runtime: runtime, accountIDs: pluginAccountIDSet(accountIDs)})
	m.mu.Unlock()
	if stale != nil && stale != runtime {
		stale.drain(10 * time.Second)
	}
	return nil
}

// PrepareOpenAIOutbound invokes the enabled ticket/header hook and applies only
// host-approved header mutations. Runtime errors are returned with handled=false
// so callers can fail open to the native transport; explicit plugin rejection is
// handled=true and must be surfaced to the caller.
func (m *PluginManager) PrepareOpenAIOutbound(ctx context.Context, request *http.Request, proxyURL string, account *Account, model, transport string) (handled bool, err error) {
	if m == nil || request == nil || account == nil {
		return false, nil
	}
	route := m.hookRoute.Load()
	if !route.matchesAccount(account) {
		return false, nil
	}
	if route.runtime == nil || route.runtime.client == nil || route.runtime.client.Exited() {
		return false, errors.New("OpenAI Codex ticket 插件不可用")
	}
	requestID, _ := ctx.Value(ctxkey.RequestID).(string)
	hookHeaders := headersToPlugin(request.Header)
	// Do not disclose bearer/cookie material to a ticket lifecycle hook. The
	// plugin resolves account identity through HostService when it harvests.
	for key := range hookHeaders {
		switch strings.ToLower(key) {
		case "authorization", "cookie", "proxy-authorization", "x-api-key":
			delete(hookHeaders, key)
		}
	}
	prepared := &pluginv1.PrepareOutboundRequest{
		RequestId: requestID, Method: request.Method, Url: request.URL.String(), Host: request.Host,
		Headers: hookHeaders, ProxyUrl: proxyURL, AccountId: account.ID, Model: model, Transport: transport,
		Platform: account.Platform, AccountType: account.Type, ContentLength: request.ContentLength,
		HasBody: request.Body != nil && request.Body != http.NoBody,
	}
	if !route.runtime.beginRequest() {
		return false, errors.New("OpenAI Codex ticket 插件正在停止")
	}
	response, callErr := route.runtime.api.PrepareOutbound(ctx, prepared)
	route.runtime.finishRequest()
	if callErr != nil {
		if status.Code(callErr) == codes.Unimplemented {
			return false, nil
		}
		return false, callErr
	}
	if response == nil {
		return false, errors.New("插件未返回出站钩子结果")
	}
	if response.GetAction() == pluginv1.HeaderHookAction_HEADER_HOOK_ACTION_REJECT {
		return true, &PluginHookRejectedError{Code: response.GetReasonCode(), Message: response.GetMessage()}
	}
	applyPluginOutboundHeaderMutation(request.Header, response)
	return true, nil
}

func applyPluginOutboundHeaderMutation(headers http.Header, response *pluginv1.PrepareOutboundResponse) {
	if headers == nil || response == nil {
		return
	}
	for key, values := range response.GetHeadersToSet() {
		if !strings.EqualFold(strings.TrimSpace(key), pluginOutboundTicketHeader) || values == nil {
			continue
		}
		clean := make([]string, 0, len(values.GetValues()))
		for _, value := range values.GetValues() {
			if strings.ContainsAny(value, "\r\n") {
				continue
			}
			clean = append(clean, value)
		}
		if len(clean) > 0 {
			headers[http.CanonicalHeaderKey(pluginOutboundTicketHeader)] = clean
		}
	}
	for _, key := range response.GetHeadersToDelete() {
		if strings.EqualFold(strings.TrimSpace(key), pluginOutboundTicketHeader) {
			headers.Del(pluginOutboundTicketHeader)
		}
	}
}

// ObserveOpenAIOutboundResponse gives the active ticket hook response-header
// feedback. It is advisory: unsupported/failed observers are ignored so an
// observer outage cannot corrupt the response already received by the client.
func (m *PluginManager) ObserveOpenAIOutboundResponse(ctx context.Context, account *Account, model, transport string, response *http.Response, sentTicketState string) {
	if m == nil || account == nil || response == nil {
		return
	}
	route := m.hookRoute.Load()
	if !route.matchesAccount(account) || route.runtime == nil || route.runtime.client == nil || route.runtime.client.Exited() || !route.runtime.beginRequest() {
		return
	}
	defer route.runtime.finishRequest()
	requestID, _ := ctx.Value(ctxkey.RequestID).(string)
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := route.runtime.api.ObserveOutboundResponse(callCtx, &pluginv1.ObserveOutboundResponseRequest{
		RequestId: requestID, AccountId: account.ID, Model: model, Transport: transport,
		StatusCode: int32(response.StatusCode), Status: response.Status, Headers: headersToPlugin(response.Header),
		RequestSent: true, SentTicketState: sentTicketState, Platform: account.Platform, AccountType: account.Type,
	})
	if err != nil && status.Code(err) != codes.Unimplemented {
		// Deliberately do not return an error after response headers are visible.
		return
	}
}
