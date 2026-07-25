package handler

import (
	"errors"
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

func TestContentModerationDeferredActionsUseRetryableNonViolationResponses(t *testing.T) {
	tests := []struct {
		action string
		code   string
	}{
		{service.ContentModerationActionSemanticReviewDeferred, "content_review_required"},
		{service.ContentModerationActionSemanticReviewUnavailable, "content_review_unavailable"},
		{service.ContentModerationActionSemanticReviewIncomplete, "content_review_incomplete"},
	}
	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			decision := &service.ContentModerationDecision{Blocked: true, Action: tt.action}
			require.True(t, contentModerationIsNonViolationDeferred(decision))
			require.Equal(t, http.StatusServiceUnavailable, contentModerationStatus(decision))
			require.Equal(t, tt.code, contentModerationErrorCode(decision))
		})
	}
}

func TestContentModerationReceiptUsesDecisionPolicyRevisionAndNeverAllowsNilDecision(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	markContentModerationReceipt(c, service.ContentModerationProtocolOpenAIResponses, "", &service.ContentModerationDecision{
		Allowed:        true,
		Action:         service.ContentModerationActionAllow,
		PolicyRevision: "policy-v2",
	}, false)
	receipt, ok := moderationcoverage.ModerationReceiptFromContext(c)
	require.True(t, ok)
	require.Equal(t, "policy-v2", receipt.PolicyRevision)
	require.True(t, receipt.ForwardAllowed)

	markContentModerationReceipt(c, service.ContentModerationProtocolOpenAIResponses, "", nil, false)
	receipt, ok = moderationcoverage.ModerationReceiptFromContext(c)
	require.True(t, ok)
	require.Equal(t, "error", receipt.Outcome)
	require.False(t, receipt.ForwardAllowed)
}

func TestContentModerationCheckErrorDecisionAllowsRequest(t *testing.T) {
	decision := contentModerationCheckErrorDecision()

	require.NotNil(t, decision)
	require.True(t, decision.Allowed)
	require.False(t, decision.Blocked)
	require.False(t, decision.Flagged)
	require.Zero(t, decision.StatusCode)
	require.Equal(t, service.ContentModerationActionError, decision.Action)
	require.Empty(t, decision.Message)
}

func TestOpenAIContentModerationNilServiceFailsOpen(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	decision := (&OpenAIGatewayHandler{}).checkContentModeration(
		c,
		zap.NewNop(),
		nil,
		middleware2.AuthSubject{UserID: 42},
		service.ContentModerationProtocolOpenAIChat,
		"gpt-test",
		[]byte(`{"messages":[{"role":"user","content":"hello"}]}`),
	)

	require.NotNil(t, decision)
	require.True(t, decision.Allowed)
	require.False(t, decision.Blocked)
	require.Zero(t, decision.StatusCode)
	require.Equal(t, service.ContentModerationActionError, decision.Action)
}

func TestSelectedAccountRequirementSurvivesReceiptUpdates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := []byte(`{"input":"benign"}`)
	markSelectedAccountModerationRequired(c, service.ContentModerationProtocolOpenAIResponses, "gpt-test", body)
	markContentModerationReceipt(c, service.ContentModerationProtocolOpenAIResponses, "", &service.ContentModerationDecision{
		Allowed: true, Action: service.ContentModerationActionAllow,
	}, false)

	require.True(t, selectedAccountModerationRequired(c, service.ContentModerationProtocolOpenAIResponses, "gpt-test", body))
	require.False(t, selectedAccountModerationRequired(c, service.ContentModerationProtocolOpenAIChat, "gpt-test", body))
	require.False(t, selectedAccountModerationRequired(c, service.ContentModerationProtocolOpenAIResponses, "gpt-other", body))
	require.False(t, selectedAccountModerationRequired(c, service.ContentModerationProtocolOpenAIResponses, "gpt-test", []byte(`{"input":"different"}`)))
}

func TestRunSelectedAccountModerationUsesRequestBoundRequirement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := []byte(`{"input":"benign"}`)
	markSelectedAccountModerationRequired(c, service.ContentModerationProtocolOpenAIResponses, "gpt-test", body)
	svc := service.NewContentModerationService(nil, nil, nil, nil, nil, nil, nil)

	result := runSelectedAccountContentModeration(c, nil, svc, nil, middleware2.AuthSubject{UserID: 7},
		service.ContentModerationProtocolOpenAIResponses, "gpt-test", body,
		&service.Account{ID: 11, Type: service.AccountTypeAPIKey})

	require.NotNil(t, result)
	require.Equal(t, service.ContentModerationDispositionProviderErrorOpen, result.Disposition)
}

func TestPromptInjectionBaselineUsesRequestIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	body := []byte(`{"input":"first"}`)
	result := service.ContentModerationBaselineResult{Completed: true, PolicyRevision: "policy-a"}
	markPromptInjectionBaselineCompleted(c, service.ContentModerationProtocolOpenAIResponses, "gpt-test", body, result)

	require.Equal(t, &result, promptInjectionBaselineResult(c, service.ContentModerationProtocolOpenAIResponses, "gpt-test", body))
	require.Nil(t, promptInjectionBaselineResult(c, service.ContentModerationProtocolOpenAIResponses, "gpt-other", body))
	require.Nil(t, promptInjectionBaselineResult(c, service.ContentModerationProtocolOpenAIResponses, "gpt-test", []byte(`{"input":"second"}`)))
}

func TestOpenAIImagesSelectedAccountRequirementUsesModerationBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	rawBody := []byte(`{"model":"gpt-image-2","prompt":"draw"}`)
	auditBody := []byte(`{"prompt":"draw"}`)
	request := openAIHTTPPreForwardRequest{
		Protocol: service.ContentModerationProtocolOpenAIImages, Model: "gpt-image-2",
		Body: rawBody, ModerationBody: auditBody, UsesModerationBody: true,
	}
	markSelectedAccountModerationRequired(c, request.Protocol, request.Model, auditBody)

	require.True(t, selectedAccountModerationRequired(c, request.Protocol, request.Model, request.contentModerationBody()))
	require.False(t, selectedAccountModerationRequired(c, request.Protocol, request.Model, rawBody))
}

func TestOpenAIHTTPPreForwardRequestGetterReusesOwnedLargeBodies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	body := make([]byte, 2<<20)
	moderationBody := make([]byte, 3<<20)
	body[0], moderationBody[0] = 1, 2
	setOpenAIHTTPPreForwardRequest(c, openAIHTTPPreForwardRequest{
		Protocol: service.ContentModerationProtocolOpenAIImages,
		Model:    "gpt-image-2", Body: body, ModerationBody: moderationBody, UsesModerationBody: true,
	})
	body[0], moderationBody[0] = 9, 9

	value, ok := c.Get(openAIHTTPPreForwardRequestContextKey)
	require.True(t, ok)
	owned := value.(openAIHTTPPreForwardRequest)
	require.Equal(t, byte(1), owned.Body[0])
	require.Equal(t, byte(2), owned.ModerationBody[0])

	got, ok := openAIHTTPPreForwardRequestFromContext(c, service.ContentModerationProtocolOpenAIImages)
	require.True(t, ok)
	require.Equal(t, &owned.Body[0], &got.Body[0])
	require.Equal(t, &owned.ModerationBody[0], &got.ModerationBody[0])

	var sink byte
	allocs := testing.AllocsPerRun(100, func() {
		request, found := openAIHTTPPreForwardRequestFromContext(c, service.ContentModerationProtocolOpenAIImages)
		if !found {
			panic("pre-forward request missing")
		}
		sink ^= request.Body[0] ^ request.contentModerationBody()[0]
	})
	require.Zero(t, allocs)
	require.NotEqual(t, byte(255), sink)
}

func TestBeginContentModerationFrameClearsTurnScopedState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	body := []byte(`{"input":"same turn"}`)
	cacheContentModerationDecision(c, service.ContentModerationProtocolOpenAIResponses, "gpt-test", body,
		&service.ContentModerationDecision{Allowed: true, Action: service.ContentModerationActionAllow})
	markSelectedAccountModerationRequired(c, service.ContentModerationProtocolOpenAIResponses, "gpt-test", body)
	markPromptInjectionBaselineCompleted(c, service.ContentModerationProtocolOpenAIResponses, "gpt-test", body,
		service.ContentModerationBaselineResult{Completed: true, PolicyRevision: "policy-a"})
	c.Set(selectedAccountModerationStateContextKey, &service.ContentModerationAttemptState{PolicyRevision: "policy-a"})
	markContentModerationReceipt(c, service.ContentModerationProtocolOpenAIResponses, "", nil, true)
	c.Set(moderationcoverage.PipelineAdmittedContextKey, true)
	c.Set(moderationcoverage.PipelineAdmissionContextKey, moderationcoverage.PipelineAdmission{Admitted: true})

	beginContentModerationFrame(c, c.Request.Context())

	_, cached := contentModerationDecisionFromCache(c, service.ContentModerationProtocolOpenAIResponses, "gpt-test", body)
	require.False(t, cached)
	require.False(t, selectedAccountModerationRequired(c, service.ContentModerationProtocolOpenAIResponses, "gpt-test", body))
	require.Nil(t, promptInjectionBaselineResult(c, service.ContentModerationProtocolOpenAIResponses, "gpt-test", body))
	state, _ := c.Get(selectedAccountModerationStateContextKey)
	require.Nil(t, state)
	_, hasReceipt := moderationcoverage.ModerationReceiptFromContext(c)
	require.False(t, hasReceipt)
	require.False(t, moderationcoverage.PipelineAdmittedFromContext(c))
	_, hasAdmission := moderationcoverage.PipelineAdmissionFromContext(c)
	require.False(t, hasAdmission)
}

func TestCompleteContentModerationFramePreservesOnlyRetryableFirstTurn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	newContext := func() *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set(selectedAccountModerationStateContextKey, &service.ContentModerationAttemptState{PolicyRevision: "policy-a"})
		return c
	}
	statePresent := func(c *gin.Context) bool {
		value, _ := c.Get(selectedAccountModerationStateContextKey)
		state, _ := value.(*service.ContentModerationAttemptState)
		return state != nil
	}

	c := newContext()
	completeContentModerationFrame(c, 1, &service.UpstreamFailoverError{StatusCode: http.StatusTooManyRequests})
	require.True(t, statePresent(c))

	c = newContext()
	completeContentModerationFrame(c, 1, &service.UpstreamFailoverError{
		StatusCode: http.StatusBadRequest, NextAccountAction: service.NextAccountStop,
	})
	require.False(t, statePresent(c))

	c = newContext()
	completeContentModerationFrame(c, 2, &service.UpstreamFailoverError{StatusCode: http.StatusTooManyRequests})
	require.False(t, statePresent(c))

	c = newContext()
	completeContentModerationFrame(c, 1, errors.New("client closed"))
	require.False(t, statePresent(c))

	c = newContext()
	completeContentModerationFrame(c, 1, nil)
	require.False(t, statePresent(c))
}

func TestRunContentModerationCacheUsesHandlerModelIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request = c.Request.WithContext(service.WithCompositeRouteDecision(c.Request.Context(), service.CompositeRouteDecision{
		Matched: true, PublicModel: "public-model", UpstreamModel: "upstream-model", TargetPlatform: service.PlatformOpenAI,
	}))
	body := []byte(`{"input":"benign"}`)
	cacheContentModerationDecision(c, service.ContentModerationProtocolOpenAIResponses, "upstream-model", body,
		&service.ContentModerationDecision{Blocked: true, Message: "cached handler identity"})

	decision := runContentModeration(c, nil, newDisabledContentModerationServiceForHandlerTest(t), nil,
		middleware2.AuthSubject{UserID: 7}, service.ContentModerationProtocolOpenAIResponses, "upstream-model", body)

	require.NotNil(t, decision)
	require.True(t, decision.Blocked)
	require.Equal(t, "cached handler identity", decision.Message)
}

func TestContentModerationDecisionCacheRecordsCompletedNilDecision(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	body := []byte(`{"input":"benign"}`)

	cacheContentModerationDecision(c, service.ContentModerationProtocolOpenAIResponses, "gpt-test", body, nil)
	decision, completed := contentModerationDecisionFromCache(c, service.ContentModerationProtocolOpenAIResponses, "gpt-test", body)

	require.True(t, completed)
	require.Nil(t, decision)
}

func TestEnsureContentModerationReceiptPreservesSelectedAccountDeferral(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	moderationcoverage.SetRouteMeta(c, moderationcoverage.Entry{
		Pipeline: moderationcoverage.PipelineOpenAIHTTP,
	})
	markContentModerationReceipt(c, service.ContentModerationProtocolOpenAIResponses, "", nil, true)
	receiptCountBefore := len(moderationcoverage.ModerationReceiptsFromContext(c))

	ensureContentModerationReceipt(c, service.ContentModerationProtocolOpenAIResponses, receiptCountBefore)
	moderationcoverage.MarkPipelineAdmittedAfterModeration(c, moderationcoverage.PipelineOpenAIHTTP,
		moderationcoverage.StagePreForward, moderationcoverage.SourceOpenAIHTTPPreForward)

	receipt, ok := moderationcoverage.ModerationReceiptFromContext(c)
	require.True(t, ok)
	require.Equal(t, "deferred_selected_account", receipt.Outcome)
	require.False(t, receipt.LocalScanDone)
	require.False(t, receipt.ForwardAllowed)
	require.Len(t, moderationcoverage.ModerationReceiptsFromContext(c), receiptCountBefore)
	admission, ok := moderationcoverage.PipelineAdmissionFromContext(c)
	require.True(t, ok)
	require.False(t, admission.Admitted)

	markContentModerationReceipt(c, service.ContentModerationProtocolOpenAIResponses, "policy-v2", &service.ContentModerationDecision{
		Blocked: true,
		Action:  service.ContentModerationActionBlock,
	}, false)
	markSelectedAccountPipelineAdmission(c)

	require.False(t, moderationcoverage.PipelineAdmittedFromContext(c))
	admission, ok = moderationcoverage.PipelineAdmissionFromContext(c)
	require.True(t, ok)
	require.False(t, admission.Admitted)
}
