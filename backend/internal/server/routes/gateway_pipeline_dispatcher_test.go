package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/moderationcoverage"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGatewayPipelineEntrypointDispatcherRouteCapabilityMatrix(t *testing.T) {
	gin.SetMode(gin.TestMode)

	openAITextBranch := func(path, handlerName, protocol string) ModeratedRouteMeta {
		return ModeratedRouteMeta{
			Method:             http.MethodPost,
			Path:               path,
			Handler:            handlerName,
			Upstream:           true,
			ModerationRequired: true,
			Protocol:           protocol,
			Pipeline:           moderationcoverage.PipelineOpenAIHTTP,
		}
	}
	genericTextRoute := func(path, handlerName, protocol string) ModeratedRouteMeta {
		return ModeratedRouteMeta{
			Method:             http.MethodPost,
			Path:               path,
			Handler:            handlerName,
			Upstream:           true,
			ModerationRequired: true,
			Protocol:           protocol,
			Pipeline:           moderationcoverage.PipelineGatewayPreForward,
		}
	}
	autoRoute := func(path, genericHandler, genericProtocol, openAIHandler, openAIProtocol string) []ModeratedRouteMeta {
		return []ModeratedRouteMeta{
			genericTextRoute(path, genericHandler, genericProtocol),
			openAITextBranch(path, openAIHandler, openAIProtocol),
		}
	}

	tests := []struct {
		name          string
		platform      string
		forcePlatform string
		metas         []ModeratedRouteMeta
		wantPipeline  string
	}{
		{
			name:     "OpenAI chat",
			platform: service.PlatformOpenAI,
			metas: autoRoute("/v1/chat/completions", "GatewayHandler.ChatCompletions", service.ContentModerationProtocolOpenAIChat,
				"OpenAIGatewayHandler.ChatCompletions", service.ContentModerationProtocolOpenAIChat),
			wantPipeline: moderationcoverage.PipelineOpenAIHTTP,
		},
		{
			name:     "OpenAI messages",
			platform: service.PlatformOpenAI,
			metas: autoRoute("/v1/messages", "GatewayHandler.Messages", service.ContentModerationProtocolAnthropicMessages,
				"OpenAIGatewayHandler.Messages", service.ContentModerationProtocolOpenAIMessages),
			wantPipeline: moderationcoverage.PipelineOpenAIHTTP,
		},
		{
			name:     "OpenAI responses",
			platform: service.PlatformOpenAI,
			metas: autoRoute("/v1/responses", "GatewayHandler.Responses", service.ContentModerationProtocolOpenAIResponses,
				"OpenAIGatewayHandler.Responses", service.ContentModerationProtocolOpenAIResponses),
			wantPipeline: moderationcoverage.PipelineOpenAIHTTP,
		},
		{
			name:     "Grok chat v1 non-streaming",
			platform: service.PlatformGrok,
			metas: autoRoute("/v1/chat/completions", "GatewayHandler.ChatCompletions", service.ContentModerationProtocolOpenAIChat,
				"OpenAIGatewayHandler.ChatCompletions", service.ContentModerationProtocolOpenAIChat),
			wantPipeline: moderationcoverage.PipelineOpenAIHTTP,
		},
		{
			name:     "Grok chat root streaming",
			platform: service.PlatformGrok,
			metas: autoRoute("/chat/completions", "GatewayHandler.ChatCompletions", service.ContentModerationProtocolOpenAIChat,
				"OpenAIGatewayHandler.ChatCompletions", service.ContentModerationProtocolOpenAIChat),
			wantPipeline: moderationcoverage.PipelineOpenAIHTTP,
		},
		{
			name:     "Grok messages non-streaming",
			platform: service.PlatformGrok,
			metas: autoRoute("/v1/messages", "GatewayHandler.Messages", service.ContentModerationProtocolAnthropicMessages,
				"OpenAIGatewayHandler.Messages", service.ContentModerationProtocolOpenAIMessages),
			wantPipeline: moderationcoverage.PipelineOpenAIHTTP,
		},
		{
			name:     "Grok messages streaming",
			platform: service.PlatformGrok,
			metas: autoRoute("/v1/messages", "GatewayHandler.Messages", service.ContentModerationProtocolAnthropicMessages,
				"OpenAIGatewayHandler.Messages", service.ContentModerationProtocolOpenAIMessages),
			wantPipeline: moderationcoverage.PipelineOpenAIHTTP,
		},
		{
			name:     "Grok responses v1 streaming",
			platform: service.PlatformGrok,
			metas: autoRoute("/v1/responses", "GatewayHandler.Responses", service.ContentModerationProtocolOpenAIResponses,
				"OpenAIGatewayHandler.Responses", service.ContentModerationProtocolOpenAIResponses),
			wantPipeline: moderationcoverage.PipelineOpenAIHTTP,
		},
		{
			name:     "Grok responses root non-streaming",
			platform: service.PlatformGrok,
			metas: autoRoute("/responses", "GatewayHandler.Responses", service.ContentModerationProtocolOpenAIResponses,
				"OpenAIGatewayHandler.Responses", service.ContentModerationProtocolOpenAIResponses),
			wantPipeline: moderationcoverage.PipelineOpenAIHTTP,
		},
		{
			name:     "Grok responses codex alias streaming",
			platform: service.PlatformGrok,
			metas: autoRoute("/backend-api/codex/responses", "GatewayHandler.Responses", service.ContentModerationProtocolOpenAIResponses,
				"OpenAIGatewayHandler.Responses", service.ContentModerationProtocolOpenAIResponses),
			wantPipeline: moderationcoverage.PipelineOpenAIHTTP,
		},
		{
			name:     "Grok responses v1 subpath",
			platform: service.PlatformGrok,
			metas: autoRoute("/v1/responses/*subpath", "GatewayHandler.Responses", service.ContentModerationProtocolOpenAIResponses,
				"OpenAIGatewayHandler.Responses", service.ContentModerationProtocolOpenAIResponses),
			wantPipeline: moderationcoverage.PipelineOpenAIHTTP,
		},
		{
			name:     "Grok responses root subpath",
			platform: service.PlatformGrok,
			metas: autoRoute("/responses/*subpath", "GatewayHandler.Responses", service.ContentModerationProtocolOpenAIResponses,
				"OpenAIGatewayHandler.Responses", service.ContentModerationProtocolOpenAIResponses),
			wantPipeline: moderationcoverage.PipelineOpenAIHTTP,
		},
		{
			name:     "Grok responses codex subpath",
			platform: service.PlatformGrok,
			metas: autoRoute("/backend-api/codex/responses/*subpath", "GatewayHandler.Responses", service.ContentModerationProtocolOpenAIResponses,
				"OpenAIGatewayHandler.Responses", service.ContentModerationProtocolOpenAIResponses),
			wantPipeline: moderationcoverage.PipelineOpenAIHTTP,
		},
		{
			name:         "Anthropic messages",
			platform:     service.PlatformAnthropic,
			metas:        []ModeratedRouteMeta{genericTextRoute("/v1/messages", "GatewayHandler.Messages", service.ContentModerationProtocolAnthropicMessages)},
			wantPipeline: moderationcoverage.PipelineGatewayPreForward,
		},
		{
			name:         "Anthropic count tokens",
			platform:     service.PlatformAnthropic,
			metas:        []ModeratedRouteMeta{genericTextRoute("/v1/messages/count_tokens", "GatewayHandler.CountTokens", service.ContentModerationProtocolAnthropicMessages)},
			wantPipeline: moderationcoverage.PipelineGatewayPreForward,
		},
		{
			name:         "Anthropic-compatible chat",
			platform:     service.PlatformAnthropic,
			metas:        []ModeratedRouteMeta{genericTextRoute("/v1/chat/completions", "GatewayHandler.ChatCompletions", service.ContentModerationProtocolOpenAIChat)},
			wantPipeline: moderationcoverage.PipelineGatewayPreForward,
		},
		{
			name:         "Anthropic-compatible responses",
			platform:     service.PlatformAnthropic,
			metas:        []ModeratedRouteMeta{genericTextRoute("/v1/responses", "GatewayHandler.Responses", service.ContentModerationProtocolOpenAIResponses)},
			wantPipeline: moderationcoverage.PipelineGatewayPreForward,
		},
		{
			name:         "Gemini native generate",
			platform:     service.PlatformGemini,
			metas:        []ModeratedRouteMeta{genericTextRoute("/v1beta/models/*modelAction", "GatewayHandler.GeminiV1BetaModels", service.ContentModerationProtocolGemini)},
			wantPipeline: moderationcoverage.PipelineGatewayPreForward,
		},
		{
			name:         "Gemini group messages compatibility",
			platform:     service.PlatformGemini,
			metas:        []ModeratedRouteMeta{genericTextRoute("/v1/messages", "GatewayHandler.Messages", service.ContentModerationProtocolAnthropicMessages)},
			wantPipeline: moderationcoverage.PipelineGatewayPreForward,
		},
		{
			name:          "forced Antigravity messages ignores OpenAI group platform",
			platform:      service.PlatformOpenAI,
			forcePlatform: service.PlatformAntigravity,
			metas:         []ModeratedRouteMeta{genericTextRoute("/antigravity/v1/messages", "GatewayHandler.Messages", service.ContentModerationProtocolAnthropicMessages)},
			wantPipeline:  moderationcoverage.PipelineGatewayPreForward,
		},
		{
			name:          "forced Antigravity count tokens ignores Grok group platform",
			platform:      service.PlatformGrok,
			forcePlatform: service.PlatformAntigravity,
			metas:         []ModeratedRouteMeta{genericTextRoute("/antigravity/v1/messages/count_tokens", "GatewayHandler.CountTokens", service.ContentModerationProtocolAnthropicMessages)},
			wantPipeline:  moderationcoverage.PipelineGatewayPreForward,
		},
		{
			name:         "OpenAI count tokens unique generic exception",
			platform:     service.PlatformOpenAI,
			metas:        []ModeratedRouteMeta{genericTextRoute("/v1/messages/count_tokens", "GatewayHandler.CountTokens", service.ContentModerationProtocolAnthropicMessages)},
			wantPipeline: moderationcoverage.PipelineGatewayPreForward,
		},
		{
			name:     "OpenAI embeddings",
			platform: service.PlatformOpenAI,
			metas: []ModeratedRouteMeta{openAITextBranch("/v1/embeddings", "OpenAIGatewayHandler.Embeddings",
				service.ContentModerationProtocolOpenAIEmbeddings)},
			wantPipeline: moderationcoverage.PipelineOpenAIHTTP,
		},
		{
			name:     "OpenAI images",
			platform: service.PlatformOpenAI,
			metas: []ModeratedRouteMeta{openAITextBranch("/v1/images/generations", "OpenAIGatewayHandler.Images",
				service.ContentModerationProtocolOpenAIImages)},
			wantPipeline: moderationcoverage.PipelineOpenAIHTTP,
		},
		{
			name:     "OpenAI responses WebSocket admission",
			platform: service.PlatformOpenAI,
			metas: []ModeratedRouteMeta{{
				Method:             http.MethodGet,
				Path:               "/v1/responses",
				Handler:            "OpenAIGatewayHandler.ResponsesWebSocket",
				Upstream:           true,
				ModerationRequired: true,
				Protocol:           service.ContentModerationProtocolOpenAIResponses,
				Pipeline:           moderationcoverage.PipelineOpenAIWebSocket,
			}},
			wantPipeline: moderationcoverage.PipelineOpenAIWebSocket,
		},
		{
			name:     "Grok responses WebSocket admission",
			platform: service.PlatformGrok,
			metas: []ModeratedRouteMeta{{
				Method:             http.MethodGet,
				Path:               "/responses",
				Handler:            "OpenAIGatewayHandler.ResponsesWebSocket",
				Upstream:           true,
				ModerationRequired: true,
				Protocol:           service.ContentModerationProtocolOpenAIResponses,
				Pipeline:           moderationcoverage.PipelineOpenAIWebSocket,
			}},
			wantPipeline: moderationcoverage.PipelineOpenAIWebSocket,
		},
		{
			name:     "OpenAI responses WebSocket codex alias admission",
			platform: service.PlatformOpenAI,
			metas: []ModeratedRouteMeta{{
				Method:             http.MethodGet,
				Path:               "/backend-api/codex/responses",
				Handler:            "OpenAIGatewayHandler.ResponsesWebSocket",
				Upstream:           true,
				ModerationRequired: true,
				Protocol:           service.ContentModerationProtocolOpenAIResponses,
				Pipeline:           moderationcoverage.PipelineOpenAIWebSocket,
			}},
			wantPipeline: moderationcoverage.PipelineOpenAIWebSocket,
		},
		{
			name:     "Grok images retain dedicated moderation",
			platform: service.PlatformGrok,
			metas: []ModeratedRouteMeta{openAITextBranch("/v1/images/generations", "OpenAIGatewayHandler.Images",
				service.ContentModerationProtocolOpenAIImages)},
		},
		{
			name:     "Grok video retains dedicated moderation",
			platform: service.PlatformGrok,
			metas: []ModeratedRouteMeta{openAITextBranch("/v1/videos/generations", "OpenAIGatewayHandler.GrokVideoGeneration",
				service.ContentModerationProtocolOpenAIImages)},
		},
		{
			name:     "Grok embeddings remain unsupported",
			platform: service.PlatformGrok,
			metas: []ModeratedRouteMeta{openAITextBranch("/v1/embeddings", "OpenAIGatewayHandler.Embeddings",
				service.ContentModerationProtocolOpenAIEmbeddings)},
		},
		{
			name:     "Grok count tokens remains unsupported",
			platform: service.PlatformGrok,
			metas:    []ModeratedRouteMeta{genericTextRoute("/v1/messages/count_tokens", "GatewayHandler.CountTokens", service.ContentModerationProtocolAnthropicMessages)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
			if tt.forcePlatform != "" {
				middleware.ForcePlatform(tt.forcePlatform)(c)
			}

			calls := map[string]int{}
			dispatcher := NewGatewayPipelineEntrypointDispatcher(GatewayPipelineEntrypointDispatcherConfig{
				GroupPlatform:    func(*gin.Context) string { return tt.platform },
				IsOpenAIPlatform: func(*gin.Context) bool { return tt.platform == service.PlatformOpenAI },
				OpenAIHTTP: GatewayPipelineEntrypointFunc(func(c *gin.Context, meta ModeratedRouteMeta) GatewayPipelineEntryResult {
					calls[moderationcoverage.PipelineOpenAIHTTP]++
					moderationcoverage.MarkPipelineAdmitted(c, meta.Pipeline, moderationcoverage.StagePreForward, "test OpenAI HTTP admission")
					return GatewayPipelineEntryResult{}
				}),
				GatewayPreForward: GatewayPipelineEntrypointFunc(func(c *gin.Context, meta ModeratedRouteMeta) GatewayPipelineEntryResult {
					calls[moderationcoverage.PipelineGatewayPreForward]++
					moderationcoverage.MarkPipelineAdmitted(c, meta.Pipeline, moderationcoverage.StagePreForward, "test generic admission")
					return GatewayPipelineEntryResult{}
				}),
			})

			for _, meta := range tt.metas {
				dispatcher.EnterGatewayPipeline(c, meta)
			}
			if _, ok := moderationcoverage.PipelineEntrypointEnteredFromContext(c, moderationcoverage.PipelineOpenAIWebSocket); ok {
				calls[moderationcoverage.PipelineOpenAIWebSocket]++
			}

			totalCalls := 0
			for _, count := range calls {
				totalCalls += count
			}
			if tt.wantPipeline == "" {
				require.Zero(t, totalCalls, "dedicated or unsupported routes must not be double-moderated by a text admission pipeline")
				_, admitted := moderationcoverage.PipelineAdmissionFromContext(c)
				require.False(t, admitted)
				return
			}

			require.Equal(t, 1, totalCalls, "every supported moderation-required text route must enter exactly one pipeline")
			require.Equal(t, 1, calls[tt.wantPipeline])
			if tt.wantPipeline == moderationcoverage.PipelineOpenAIWebSocket {
				_, admitted := moderationcoverage.PipelineAdmissionFromContext(c)
				require.False(t, admitted, "WebSocket handshake is only an entrypoint; response.create frames own moderation admission")
				return
			}
			admission, admitted := moderationcoverage.PipelineAdmissionFromContext(c)
			require.True(t, admitted)
			require.True(t, admission.Admitted)
			require.Equal(t, tt.wantPipeline, admission.Pipeline)
		})
	}
}
