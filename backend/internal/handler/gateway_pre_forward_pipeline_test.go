package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/moderationcoverage"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestGatewayPreForwardPipelineModerationStageBlocksWithProviderErrorShape(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		path           string
		errorFormat    gatewayPreForwardErrorFormat
		protocol       string
		model          string
		body           []byte
		wantBodyParts  []string
		unwantedBody   string
		expectedStatus int
	}{
		{
			name:           "anthropic messages",
			path:           "/v1/messages",
			errorFormat:    gatewayPreForwardErrorAnthropic,
			protocol:       service.ContentModerationProtocolAnthropicMessages,
			model:          "claude-sonnet-4-5",
			body:           []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"risk"}]}`),
			wantBodyParts:  []string{"content_policy_violation", "blocked by pipeline"},
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "gemini generate",
			path:           "/v1beta/models/gemini-2.5-pro:generateContent",
			errorFormat:    gatewayPreForwardErrorGemini,
			protocol:       service.ContentModerationProtocolGemini,
			model:          "gemini-2.5-pro",
			body:           []byte(`{"contents":[{"role":"user","parts":[{"text":"risk"}]}]}`),
			wantBodyParts:  []string{"blocked by pipeline", "PERMISSION_DENIED"},
			unwantedBody:   "content_policy_violation",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "openai chat completions",
			path:           "/v1/chat/completions",
			errorFormat:    gatewayPreForwardErrorOpenAIChat,
			protocol:       service.ContentModerationProtocolOpenAIChat,
			model:          "gpt-5.4",
			body:           []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"risk"}]}`),
			wantBodyParts:  []string{"content_policy_violation", "blocked by pipeline"},
			unwantedBody:   "PERMISSION_DENIED",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "openai responses",
			path:           "/v1/responses",
			errorFormat:    gatewayPreForwardErrorOpenAIResponses,
			protocol:       service.ContentModerationProtocolOpenAIResponses,
			model:          "gpt-5.4",
			body:           []byte(`{"model":"gpt-5.4","input":"risk"}`),
			wantBodyParts:  []string{"content_policy_violation", "blocked by pipeline"},
			unwantedBody:   "PERMISSION_DENIED",
			expectedStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			guard := &gatewayPreForwardPipelineModerationGuardSpy{decision: &service.ContentModerationDecision{
				Blocked:    true,
				StatusCode: tt.expectedStatus,
				Message:    "blocked by pipeline",
				Action:     service.ContentModerationActionBlock,
			}}
			pipeline := newGatewayPreForwardPipeline(guard)

			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, tt.path, nil)
			apiKey := &service.APIKey{ID: 11, Name: "gateway-key"}
			subject := middleware2.AuthSubject{UserID: 42, Concurrency: 2}

			result := pipeline.Run(&GatewayHandler{}, c, zap.NewNop(), gatewayPreForwardPipelineInput{
				APIKey:      apiKey,
				Subject:     subject,
				Protocol:    tt.protocol,
				Model:       tt.model,
				Body:        tt.body,
				ErrorFormat: tt.errorFormat,
			})

			require.True(t, result.Blocked)
			require.Equal(t, tt.expectedStatus, rec.Code)
			for _, part := range tt.wantBodyParts {
				require.Contains(t, rec.Body.String(), part)
			}
			if tt.unwantedBody != "" {
				require.NotContains(t, rec.Body.String(), tt.unwantedBody)
			}
			require.Len(t, guard.calls, 1)
			require.Same(t, apiKey, guard.calls[0].APIKey)
			require.Equal(t, subject, guard.calls[0].Subject)
			require.Equal(t, tt.protocol, guard.calls[0].Protocol)
			require.Equal(t, tt.model, guard.calls[0].Model)
			require.Equal(t, tt.body, guard.calls[0].Body)
			_, ok := moderationcoverage.PipelineAdmissionFromContext(c)
			require.False(t, ok)
			require.Equal(t, []moderationcoverage.PipelineStageExecution{
				{
					Pipeline: moderationcoverage.PipelineGatewayPreForward,
					Stage:    moderationcoverage.StageModeration,
					Source:   moderationcoverage.SourceGatewayPreForward,
				},
			}, moderationcoverage.PipelineStageExecutionsFromContext(c))
		})
	}
}

func TestGatewayPreForwardPipelineMarksAdmissionWhenModerationAllows(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		path        string
		errorFormat gatewayPreForwardErrorFormat
		protocol    string
		model       string
		body        []byte
	}{
		{
			name:        "anthropic messages",
			path:        "/v1/messages",
			errorFormat: gatewayPreForwardErrorAnthropic,
			protocol:    service.ContentModerationProtocolAnthropicMessages,
			model:       "claude-sonnet-4-5",
			body:        []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`),
		},
		{
			name:        "gemini generate",
			path:        "/v1beta/models/gemini-2.5-pro:generateContent",
			errorFormat: gatewayPreForwardErrorGemini,
			protocol:    service.ContentModerationProtocolGemini,
			model:       "gemini-2.5-pro",
			body:        []byte(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`),
		},
		{
			name:        "openai responses",
			path:        "/v1/responses",
			errorFormat: gatewayPreForwardErrorOpenAIResponses,
			protocol:    service.ContentModerationProtocolOpenAIResponses,
			model:       "gpt-5.4",
			body:        []byte(`{"model":"gpt-5.4","input":"hello"}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			guard := &gatewayPreForwardPipelineModerationGuardSpy{decision: &service.ContentModerationDecision{
				Allowed: true,
				Action:  service.ContentModerationActionAllow,
			}}
			pipeline := newGatewayPreForwardPipeline(guard)

			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, tt.path, nil)
			result := pipeline.Run(&GatewayHandler{}, c, zap.NewNop(), gatewayPreForwardPipelineInput{
				APIKey:      &service.APIKey{ID: 12, Name: "gateway-key"},
				Subject:     middleware2.AuthSubject{UserID: 43, Concurrency: 2},
				Protocol:    tt.protocol,
				Model:       tt.model,
				Body:        tt.body,
				ErrorFormat: tt.errorFormat,
			})

			require.False(t, result.Blocked)
			admission, ok := moderationcoverage.PipelineAdmissionFromContext(c)
			require.True(t, ok)
			require.True(t, admission.Admitted)
			require.Equal(t, moderationcoverage.PipelineGatewayPreForward, admission.Pipeline)
			require.Equal(t, moderationcoverage.StagePreForward, admission.Stage)
			require.Equal(t, moderationcoverage.SourceGatewayPreForward, admission.Source)
			require.True(t, moderationcoverage.PipelineAdmittedFromContext(c))
			require.Equal(t, []moderationcoverage.PipelineStageExecution{
				{
					Pipeline: moderationcoverage.PipelineGatewayPreForward,
					Stage:    moderationcoverage.StageModeration,
					Source:   moderationcoverage.SourceGatewayPreForward,
				},
				{
					Pipeline: moderationcoverage.PipelineGatewayPreForward,
					Stage:    moderationcoverage.StagePreForward,
					Source:   moderationcoverage.SourceGatewayPreForward,
				},
			}, moderationcoverage.PipelineStageExecutionsFromContext(c))
		})
	}
}

func TestGatewayPreForwardPipelineExecutionIncludesRouteMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	guard := &gatewayPreForwardPipelineModerationGuardSpy{decision: &service.ContentModerationDecision{
		Allowed: true,
		Action:  service.ContentModerationActionAllow,
	}}
	pipeline := newGatewayPreForwardPipeline(guard)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	moderationcoverage.SetRouteMeta(c, moderationcoverage.Entry{
		Method:             http.MethodPost,
		Path:               "/v1/messages",
		Handler:            "GatewayHandler.Messages",
		Upstream:           true,
		ModerationRequired: true,
		Protocol:           service.ContentModerationProtocolAnthropicMessages,
		Pipeline:           moderationcoverage.PipelineGatewayPreForward,
		Status:             moderationcoverage.StatusCovered,
	})

	result := pipeline.Run(&GatewayHandler{}, c, zap.NewNop(), gatewayPreForwardPipelineInput{
		APIKey:      &service.APIKey{ID: 12, Name: "gateway-key"},
		Subject:     middleware2.AuthSubject{UserID: 43, Concurrency: 2},
		Protocol:    service.ContentModerationProtocolAnthropicMessages,
		Model:       "claude-sonnet-4-5",
		Body:        []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`),
		ErrorFormat: gatewayPreForwardErrorAnthropic,
	})

	require.False(t, result.Blocked)
	require.Equal(t, []moderationcoverage.PipelineStageExecution{
		{
			Pipeline: moderationcoverage.PipelineGatewayPreForward,
			Stage:    moderationcoverage.StageModeration,
			Source:   moderationcoverage.SourceGatewayPreForward,
			Method:   http.MethodPost,
			Path:     "/v1/messages",
			Handler:  "GatewayHandler.Messages",
			Protocol: service.ContentModerationProtocolAnthropicMessages,
		},
		{
			Pipeline: moderationcoverage.PipelineGatewayPreForward,
			Stage:    moderationcoverage.StagePreForward,
			Source:   moderationcoverage.SourceGatewayPreForward,
			Method:   http.MethodPost,
			Path:     "/v1/messages",
			Handler:  "GatewayHandler.Messages",
			Protocol: service.ContentModerationProtocolAnthropicMessages,
		},
	}, moderationcoverage.PipelineStageExecutionsFromContext(c))
}

func TestGatewayForwardStageExecutionIncludesRouteMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", nil)
	moderationcoverage.SetRouteMeta(c, moderationcoverage.Entry{
		Method:                  http.MethodPost,
		Path:                    "/v1/messages/count_tokens",
		Handler:                 "GatewayHandler.CountTokens",
		Upstream:                true,
		ModerationRequired:      true,
		Protocol:                service.ContentModerationProtocolAnthropicMessages,
		Pipeline:                moderationcoverage.PipelineGatewayPreForward,
		Status:                  moderationcoverage.StatusCovered,
		StageAdapterDescriptors: moderationcoverage.StageAdapterDescriptorsForRoute("GatewayHandler.CountTokens", service.ContentModerationProtocolAnthropicMessages),
	})
	markForwardableModerationReceipt(c, service.ContentModerationProtocolAnthropicMessages)

	calls := 0
	result := (&GatewayHandler{}).runGatewayForwardStage(c, ForwardStageAdapter{
		Name: "GatewayCountTokensForwardStage",
		Forward: func(ctx *gin.Context) ExecutableStageResult {
			require.Same(t, c, ctx)
			calls++
			return ExecutableStageResult{}
		},
	})

	require.NoError(t, result.Err)
	require.False(t, result.Stop)
	require.Equal(t, 1, calls)
	require.Equal(t, []moderationcoverage.PipelineStageExecution{
		{
			Pipeline: moderationcoverage.PipelineGatewayPreForward,
			Stage:    moderationcoverage.StageForward,
			Source:   moderationcoverage.SourceGatewayForwardStage,
			Method:   http.MethodPost,
			Path:     "/v1/messages/count_tokens",
			Handler:  "GatewayHandler.CountTokens",
			Protocol: service.ContentModerationProtocolAnthropicMessages,
		},
	}, moderationcoverage.PipelineStageExecutionsFromContext(c))
}

func TestGatewayForwardStageUsesRouteDescriptorRegistry(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", nil)
	moderationcoverage.SetRouteMeta(c, moderationcoverage.Entry{
		Method:             http.MethodPost,
		Path:               "/v1/messages/count_tokens",
		Handler:            "GatewayHandler.CountTokens",
		Upstream:           true,
		ModerationRequired: true,
		Protocol:           service.ContentModerationProtocolAnthropicMessages,
		Pipeline:           moderationcoverage.PipelineGatewayPreForward,
		Status:             moderationcoverage.StatusCovered,
	})
	markForwardableModerationReceipt(c, service.ContentModerationProtocolAnthropicMessages)

	calls := []string{}
	registered := ForwardStageAdapter{
		Name: "GatewayCountTokensForwardStage",
		Forward: func(*gin.Context) ExecutableStageResult {
			calls = append(calls, "registered")
			return ExecutableStageResult{}
		},
	}
	direct := ForwardStageAdapter{
		Name: "GatewayCountTokensForwardStage",
		Forward: func(*gin.Context) ExecutableStageResult {
			calls = append(calls, "direct")
			return ExecutableStageResult{}
		},
	}
	handler := &GatewayHandler{
		forwardStageRegistry: NewForwardStageRegistry(),
	}
	handler.forwardStageRegistry.Register(moderationcoverage.RouteAdapterDescriptor{
		Stage:    moderationcoverage.StageForward,
		Pipeline: moderationcoverage.PipelineGatewayPreForward,
		Name:     "GatewayCountTokensForwardStage",
	}, registered)

	result := handler.runGatewayForwardStage(c, direct)

	require.NoError(t, result.Err)
	require.False(t, result.Stop)
	require.Equal(t, []string{"registered"}, calls)
	require.Equal(t, []moderationcoverage.PipelineStageExecution{
		{
			Pipeline: moderationcoverage.PipelineGatewayPreForward,
			Stage:    moderationcoverage.StageForward,
			Source:   moderationcoverage.SourceGatewayForwardStage,
			Method:   http.MethodPost,
			Path:     "/v1/messages/count_tokens",
			Handler:  "GatewayHandler.CountTokens",
			Protocol: service.ContentModerationProtocolAnthropicMessages,
		},
	}, moderationcoverage.PipelineStageExecutionsFromContext(c))
}

func TestGatewayForwardStageRequiresRegistrarRouteMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	calls := 0

	result := (&GatewayHandler{}).runGatewayForwardStage(c, ForwardStageAdapter{
		Name: "GatewayCountTokensForwardStage",
		Forward: func(*gin.Context) ExecutableStageResult {
			calls++
			return ExecutableStageResult{}
		},
	})

	require.True(t, result.Stop)
	require.ErrorContains(t, result.Err, "pipeline route metadata is required before forward")
	require.Equal(t, 0, calls)
}

func TestGatewayForwardStageRequiresDescriptorBoundAdapter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", nil)
	moderationcoverage.SetRouteMeta(c, moderationcoverage.Entry{
		Method:                  http.MethodPost,
		Path:                    "/v1/messages/count_tokens",
		Handler:                 "GatewayHandler.CountTokens",
		Protocol:                service.ContentModerationProtocolAnthropicMessages,
		Pipeline:                moderationcoverage.PipelineGatewayPreForward,
		StageAdapterDescriptors: moderationcoverage.StageAdapterDescriptorsForRoute("GatewayHandler.CountTokens", service.ContentModerationProtocolAnthropicMessages),
	})
	calls := 0

	result := (&GatewayHandler{}).runGatewayForwardStage(c, ForwardStageAdapter{
		Name: "UnregisteredForwardStage",
		Forward: func(*gin.Context) ExecutableStageResult {
			calls++
			return ExecutableStageResult{}
		},
	})

	require.True(t, result.Stop)
	require.ErrorContains(t, result.Err, "pipeline forward stage adapter is not bound by route descriptor")
	require.Equal(t, 0, calls)
}

func TestGatewayBillingRoutingUsageStagesRequireRegistrarRouteMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name    string
		stage   string
		adapter func(*int) any
		run     func(*GatewayHandler, *gin.Context, any) ExecutableStageResult
	}{
		{
			name:  "billing",
			stage: moderationcoverage.StageBilling,
			adapter: func(calls *int) any {
				return BillingStageAdapter{Name: "GatewayBillingStage", Billing: func(*gin.Context) ExecutableStageResult {
					*calls++
					return ExecutableStageResult{}
				}}
			},
			run: func(h *GatewayHandler, c *gin.Context, adapter any) ExecutableStageResult {
				return h.runGatewayBillingStage(c, adapter.(BillingStage))
			},
		},
		{
			name:  "routing",
			stage: moderationcoverage.StageRouting,
			adapter: func(calls *int) any {
				return RoutingStageAdapter{Name: "GatewayRoutingStage", Routing: func(*gin.Context) ExecutableStageResult {
					*calls++
					return ExecutableStageResult{}
				}}
			},
			run: func(h *GatewayHandler, c *gin.Context, adapter any) ExecutableStageResult {
				return h.runGatewayRoutingStage(c, adapter.(RoutingStage))
			},
		},
		{
			name:  "usage",
			stage: moderationcoverage.StageUsage,
			adapter: func(calls *int) any {
				return UsageStageAdapter{Name: "GatewayUsageStage", Usage: func(*gin.Context) ExecutableStageResult {
					*calls++
					return ExecutableStageResult{}
				}}
			},
			run: func(h *GatewayHandler, c *gin.Context, adapter any) ExecutableStageResult {
				return h.runGatewayUsageStage(c, adapter.(UsageStage))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			calls := 0

			result := tt.run(&GatewayHandler{}, c, tt.adapter(&calls))

			require.True(t, result.Stop)
			require.ErrorContains(t, result.Err, "pipeline route metadata is required before "+tt.stage)
			require.Equal(t, 0, calls)
		})
	}
}

func TestGatewayBillingRoutingUsageStagesRequireDescriptorBoundAdapter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name    string
		stage   string
		adapter func(*int) any
		run     func(*GatewayHandler, *gin.Context, any) ExecutableStageResult
	}{
		{
			name:  "billing",
			stage: moderationcoverage.StageBilling,
			adapter: func(calls *int) any {
				return BillingStageAdapter{Name: "UnregisteredBillingStage", Billing: func(*gin.Context) ExecutableStageResult {
					*calls++
					return ExecutableStageResult{}
				}}
			},
			run: func(h *GatewayHandler, c *gin.Context, adapter any) ExecutableStageResult {
				return h.runGatewayBillingStage(c, adapter.(BillingStage))
			},
		},
		{
			name:  "routing",
			stage: moderationcoverage.StageRouting,
			adapter: func(calls *int) any {
				return RoutingStageAdapter{Name: "UnregisteredRoutingStage", Routing: func(*gin.Context) ExecutableStageResult {
					*calls++
					return ExecutableStageResult{}
				}}
			},
			run: func(h *GatewayHandler, c *gin.Context, adapter any) ExecutableStageResult {
				return h.runGatewayRoutingStage(c, adapter.(RoutingStage))
			},
		},
		{
			name:  "usage",
			stage: moderationcoverage.StageUsage,
			adapter: func(calls *int) any {
				return UsageStageAdapter{Name: "UnregisteredUsageStage", Usage: func(*gin.Context) ExecutableStageResult {
					*calls++
					return ExecutableStageResult{}
				}}
			},
			run: func(h *GatewayHandler, c *gin.Context, adapter any) ExecutableStageResult {
				return h.runGatewayUsageStage(c, adapter.(UsageStage))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			path := "/v1/messages/count_tokens"
			handler := "GatewayHandler.CountTokens"
			if tt.stage == moderationcoverage.StageUsage {
				path = "/v1/messages"
				handler = "GatewayHandler.Messages"
			}
			c.Request = httptest.NewRequest(http.MethodPost, path, nil)
			moderationcoverage.SetRouteMeta(c, moderationcoverage.Entry{
				Method:                  http.MethodPost,
				Path:                    path,
				Handler:                 handler,
				Protocol:                service.ContentModerationProtocolAnthropicMessages,
				Pipeline:                moderationcoverage.PipelineGatewayPreForward,
				StageAdapterDescriptors: moderationcoverage.StageAdapterDescriptorsForRoute(handler, service.ContentModerationProtocolAnthropicMessages),
			})
			calls := 0

			result := tt.run(&GatewayHandler{}, c, tt.adapter(&calls))

			require.True(t, result.Stop)
			require.ErrorContains(t, result.Err, "pipeline "+tt.stage+" stage adapter is not bound by route descriptor")
			require.Equal(t, 0, calls)
		})
	}
}

func TestGatewayBillingRoutingUsageStagesUseRouteDescriptorRegistry(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name     string
		stage    string
		register func(*StageAdapterRegistry, string, *int)
		run      func(*GatewayHandler, *gin.Context) ExecutableStageResult
	}{
		{
			name:  "billing",
			stage: moderationcoverage.StageBilling,
			register: func(registry *StageAdapterRegistry, pipeline string, calls *int) {
				registry.RegisterBilling(moderationcoverage.RouteAdapterDescriptor{Stage: moderationcoverage.StageBilling, Pipeline: pipeline, Name: "GatewayBillingStage"}, BillingStageAdapter{
					Name: "GatewayBillingStage",
					Billing: func(*gin.Context) ExecutableStageResult {
						*calls++
						return ExecutableStageResult{}
					},
				})
			},
			run: func(h *GatewayHandler, c *gin.Context) ExecutableStageResult {
				return h.runGatewayBillingStage(c, BillingStageAdapter{Name: "GatewayBillingStage"})
			},
		},
		{
			name:  "routing",
			stage: moderationcoverage.StageRouting,
			register: func(registry *StageAdapterRegistry, pipeline string, calls *int) {
				registry.RegisterRouting(moderationcoverage.RouteAdapterDescriptor{Stage: moderationcoverage.StageRouting, Pipeline: pipeline, Name: "GatewayRoutingStage"}, RoutingStageAdapter{
					Name: "GatewayRoutingStage",
					Routing: func(*gin.Context) ExecutableStageResult {
						*calls++
						return ExecutableStageResult{}
					},
				})
			},
			run: func(h *GatewayHandler, c *gin.Context) ExecutableStageResult {
				return h.runGatewayRoutingStage(c, RoutingStageAdapter{Name: "GatewayRoutingStage"})
			},
		},
		{
			name:  "usage",
			stage: moderationcoverage.StageUsage,
			register: func(registry *StageAdapterRegistry, pipeline string, calls *int) {
				registry.RegisterUsage(moderationcoverage.RouteAdapterDescriptor{Stage: moderationcoverage.StageUsage, Pipeline: pipeline, Name: "GatewayUsageStage"}, UsageStageAdapter{
					Name: "GatewayUsageStage",
					Usage: func(*gin.Context) ExecutableStageResult {
						*calls++
						return ExecutableStageResult{}
					},
				})
			},
			run: func(h *GatewayHandler, c *gin.Context) ExecutableStageResult {
				return h.runGatewayUsageStage(c, UsageStageAdapter{Name: "GatewayUsageStage"})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			moderationcoverage.SetRouteMeta(c, moderationcoverage.Entry{
				Method:             http.MethodPost,
				Path:               "/v1/messages",
				Handler:            "GatewayHandler.Messages",
				Upstream:           true,
				ModerationRequired: true,
				Protocol:           service.ContentModerationProtocolAnthropicMessages,
				Pipeline:           moderationcoverage.PipelineGatewayPreForward,
				Status:             moderationcoverage.StatusCovered,
			})
			calls := 0
			registry := NewStageAdapterRegistry()
			tt.register(registry, moderationcoverage.PipelineGatewayPreForward, &calls)
			handler := &GatewayHandler{stageAdapterRegistry: registry}

			result := tt.run(handler, c)

			require.False(t, result.Stop)
			require.NoError(t, result.Err)
			require.Equal(t, 1, calls)
			require.Equal(t, []moderationcoverage.PipelineStageExecution{{
				Pipeline: moderationcoverage.PipelineGatewayPreForward,
				Stage:    tt.stage,
				Source: map[string]string{
					moderationcoverage.StageBilling: moderationcoverage.SourceGatewayBillingStage,
					moderationcoverage.StageRouting: moderationcoverage.SourceGatewayRoutingStage,
					moderationcoverage.StageUsage:   moderationcoverage.SourceGatewayUsageStage,
				}[tt.stage],
				Method:   http.MethodPost,
				Path:     "/v1/messages",
				Handler:  "GatewayHandler.Messages",
				Protocol: service.ContentModerationProtocolAnthropicMessages,
			}}, moderationcoverage.PipelineStageExecutionsFromContext(c))
		})
	}
}

func TestGatewayForwardStageDoesNotCacheRequestFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := &GatewayHandler{
		forwardStageRegistry: NewForwardStageRegistry(),
	}
	calls := []string{}
	for _, name := range []string{"first", "second"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", nil)
		moderationcoverage.SetRouteMeta(c, moderationcoverage.Entry{
			Method:             http.MethodPost,
			Path:               "/v1/messages/count_tokens",
			Handler:            "GatewayHandler.CountTokens",
			Upstream:           true,
			ModerationRequired: true,
			Protocol:           service.ContentModerationProtocolAnthropicMessages,
			Pipeline:           moderationcoverage.PipelineGatewayPreForward,
			Status:             moderationcoverage.StatusCovered,
		})
		markForwardableModerationReceipt(c, service.ContentModerationProtocolAnthropicMessages)
		result := handler.runGatewayForwardStage(c, ForwardStageAdapter{
			Name: "GatewayCountTokensForwardStage",
			Forward: func(*gin.Context) ExecutableStageResult {
				calls = append(calls, name)
				return ExecutableStageResult{}
			},
		})
		require.NoError(t, result.Err)
		require.False(t, result.Stop)
	}

	require.Equal(t, []string{"first", "second"}, calls)
}

func TestGatewayBillingStageExecutionIncludesRouteMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	moderationcoverage.SetRouteMeta(c, moderationcoverage.Entry{
		Method:             http.MethodPost,
		Path:               "/v1/messages",
		Handler:            "GatewayHandler.Messages",
		Upstream:           true,
		ModerationRequired: true,
		Protocol:           service.ContentModerationProtocolAnthropicMessages,
		Pipeline:           moderationcoverage.PipelineGatewayPreForward,
		Status:             moderationcoverage.StatusCovered,
	})

	result := (&GatewayHandler{}).runGatewayBillingStage(c, GatewayBillingStage{})

	require.NoError(t, result.Err)
	require.False(t, result.Stop)
	require.Equal(t, []moderationcoverage.PipelineStageExecution{
		{
			Pipeline: moderationcoverage.PipelineGatewayPreForward,
			Stage:    moderationcoverage.StageBilling,
			Source:   moderationcoverage.SourceGatewayBillingStage,
			Method:   http.MethodPost,
			Path:     "/v1/messages",
			Handler:  "GatewayHandler.Messages",
			Protocol: service.ContentModerationProtocolAnthropicMessages,
		},
	}, moderationcoverage.PipelineStageExecutionsFromContext(c))
}

func TestGatewayRoutingStageExecutionIncludesRouteMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	moderationcoverage.SetRouteMeta(c, moderationcoverage.Entry{
		Method:             http.MethodPost,
		Path:               "/v1/messages",
		Handler:            "GatewayHandler.Messages",
		Upstream:           true,
		ModerationRequired: true,
		Protocol:           service.ContentModerationProtocolAnthropicMessages,
		Pipeline:           moderationcoverage.PipelineGatewayPreForward,
		Status:             moderationcoverage.StatusCovered,
	})

	result := (&GatewayHandler{}).runGatewayRoutingStage(c, GatewayRoutingStage{})

	require.NoError(t, result.Err)
	require.False(t, result.Stop)
	require.Equal(t, []moderationcoverage.PipelineStageExecution{
		{
			Pipeline: moderationcoverage.PipelineGatewayPreForward,
			Stage:    moderationcoverage.StageRouting,
			Source:   moderationcoverage.SourceGatewayRoutingStage,
			Method:   http.MethodPost,
			Path:     "/v1/messages",
			Handler:  "GatewayHandler.Messages",
			Protocol: service.ContentModerationProtocolAnthropicMessages,
		},
	}, moderationcoverage.PipelineStageExecutionsFromContext(c))
}

func TestGatewayUsageStageExecutionIncludesRouteMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	moderationcoverage.SetRouteMeta(c, moderationcoverage.Entry{
		Method:             http.MethodPost,
		Path:               "/v1/messages",
		Handler:            "GatewayHandler.Messages",
		Upstream:           true,
		ModerationRequired: true,
		Protocol:           service.ContentModerationProtocolAnthropicMessages,
		Pipeline:           moderationcoverage.PipelineGatewayPreForward,
		Status:             moderationcoverage.StatusCovered,
	})

	calls := 0
	result := (&GatewayHandler{}).runGatewayUsageStage(c, UsageStageAdapter{
		Name: "GatewayUsageStage",
		Usage: func(ctx *gin.Context) ExecutableStageResult {
			require.Same(t, c, ctx)
			calls++
			return ExecutableStageResult{}
		},
	})

	require.NoError(t, result.Err)
	require.False(t, result.Stop)
	require.Equal(t, 1, calls)
	require.Equal(t, []moderationcoverage.PipelineStageExecution{
		{
			Pipeline: moderationcoverage.PipelineGatewayPreForward,
			Stage:    moderationcoverage.StageUsage,
			Source:   moderationcoverage.SourceGatewayUsageStage,
			Method:   http.MethodPost,
			Path:     "/v1/messages",
			Handler:  "GatewayHandler.Messages",
			Protocol: service.ContentModerationProtocolAnthropicMessages,
		},
	}, moderationcoverage.PipelineStageExecutionsFromContext(c))
}

type gatewayPreForwardPipelineModerationGuardSpy struct {
	decision *service.ContentModerationDecision
	calls    []moderationGuardInput
}

func (s *gatewayPreForwardPipelineModerationGuardSpy) Check(c *gin.Context, reqLog *zap.Logger, input moderationGuardInput) *service.ContentModerationDecision {
	s.calls = append(s.calls, input)
	return s.decision
}
