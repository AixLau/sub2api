package service

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/tidwall/gjson"
)

func (s *OpenAIGatewayService) SetPluginManager(manager *PluginManager) {
	s.pluginManager = manager
}

// doOpenAIUpstream 只在 OpenAI OAuth 能力绑定已启用时把真实请求交给插件。
// 插件返回标准 http.Response，响应解析、错误映射、SSE 和计费仍由现有核心链处理。
func (s *OpenAIGatewayService) doOpenAIUpstream(request *http.Request, proxyURL string, account *Account) (*http.Response, error) {
	model := openAIOutboundRequestModel(request)
	transport := openAIOutboundRequestTransport(request)
	sentTicketState := ""
	if s.pluginManager != nil {
		sentTicketState = request.Header.Get(pluginOutboundTicketHeader)
		if _, hookErr := s.pluginManager.PrepareOpenAIOutbound(request.Context(), request, proxyURL, account, model, transport); hookErr != nil {
			if _, rejected := hookErr.(*PluginHookRejectedError); rejected {
				return nil, hookErr
			}
			// Hook runtime errors are fail-open by design; the native transport
			// remains available while the ticket worker recovers.
		}
		sentTicketState = request.Header.Get(pluginOutboundTicketHeader)
	}
	var (
		response *http.Response
		err      error
	)
	if s.pluginManager != nil {
		response, handled, err := s.pluginManager.RoundTripOpenAIOAuth(request.Context(), request, proxyURL, account)
		if handled {
			if err == nil {
				s.pluginManager.ObserveOpenAIOutboundResponse(request.Context(), account, model, transport, response, sentTicketState)
			}
			return response, err
		}
	}
	response, err = s.httpUpstream.Do(request, proxyURL, account.ID, account.Concurrency)
	if err == nil && s.pluginManager != nil {
		s.pluginManager.ObserveOpenAIOutboundResponse(request.Context(), account, model, transport, response, sentTicketState)
	}
	return response, err
}

// doOpenAIAccountTestUpstream 让 OpenAI OAuth 账号测试与真实转发使用同一插件路径。
// API Key 和未命中插件的账号保持各自原有的 HTTPUpstream 行为。
func (s *AccountTestService) doOpenAIAccountTestUpstream(
	request *http.Request,
	proxyURL string,
	account *Account,
	useTLSFallback bool,
) (*http.Response, error) {
	model := openAIOutboundRequestModel(request)
	transport := openAIOutboundRequestTransport(request)
	sentTicketState := ""
	if s.pluginManager != nil {
		_, hookErr := s.pluginManager.PrepareOpenAIOutbound(request.Context(), request, proxyURL, account, model, transport)
		if hookErr != nil {
			if _, rejected := hookErr.(*PluginHookRejectedError); rejected {
				return nil, hookErr
			}
		}
		sentTicketState = request.Header.Get(pluginOutboundTicketHeader)
		response, handled, err := s.pluginManager.RoundTripOpenAIOAuth(request.Context(), request, proxyURL, account)
		if handled {
			if err == nil {
				s.pluginManager.ObserveOpenAIOutboundResponse(request.Context(), account, model, transport, response, sentTicketState)
			}
			return response, err
		}
	}
	var response *http.Response
	var err error
	if useTLSFallback {
		response, err = s.httpUpstream.DoWithTLS(
			request,
			proxyURL,
			account.ID,
			account.Concurrency,
			s.tlsFPProfileService.ResolveTLSProfile(account),
		)
	} else {
		response, err = s.httpUpstream.Do(request, proxyURL, account.ID, account.Concurrency)
	}
	if err == nil && s.pluginManager != nil {
		s.pluginManager.ObserveOpenAIOutboundResponse(request.Context(), account, model, transport, response, sentTicketState)
	}
	return response, err
}

func openAIOutboundRequestModel(request *http.Request) string {
	if request == nil || request.GetBody == nil {
		return ""
	}
	body, err := request.GetBody()
	if err != nil || body == nil {
		return ""
	}
	defer func() { _ = body.Close() }()
	payload, err := io.ReadAll(io.LimitReader(body, 1<<20))
	if err != nil || !json.Valid(payload) {
		return ""
	}
	return strings.TrimSpace(gjson.GetBytes(payload, "model").String())
}

func openAIOutboundRequestTransport(request *http.Request) string {
	if request == nil {
		return "http"
	}
	if strings.Contains(strings.ToLower(request.Header.Get("Accept")), "text/event-stream") {
		return "sse"
	}
	if strings.EqualFold(request.Header.Get("Upgrade"), "websocket") {
		return "websocket"
	}
	return "http"
}
