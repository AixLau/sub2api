package handler

import (
	"context"
	"net/http"
	"time"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// Returns handled only when a group has explicitly switched to grouped routing,
// or its authoritative route lookup failed. No legacy slot has been acquired.
func (h *OpenAIGatewayHandler) tryCredentialHTTP(c *gin.Context, apiKey *service.APIKey, subject middleware2.AuthSubject, subscription *service.UserSubscription, body, forwardBody []byte, model string, compact bool, log *zap.Logger, streamStarted bool) bool {
	runtime := h.credentialHTTP
	if runtime == nil || !runtime.Enabled || apiKey.GroupID == nil {
		return false
	}
	routes, mixed, err := runtime.Routes.CredentialRoutes(c.Request.Context(), *apiKey.GroupID)
	fail := func(code string) { h.errorResponse(c, 503, code, "Grouped HTTP request could not be safely admitted") }
	if err != nil {
		fail("ADMISSION_STORE_UNAVAILABLE")
		return true
	}
	if len(routes) == 0 {
		return false
	}
	if mixed {
		fail("GROUPED_ROUTE_MIXED_UNSUPPORTED")
		return true
	}
	if runtime.Vault == nil {
		fail("CREDENTIAL_VAULT_UNAVAILABLE")
		return true
	}
	if billing := h.runOpenAIHTTPBillingStage(c, OpenAIHTTPBillingStage{Handler: h, ReqLog: log, APIKey: apiKey, Subscription: subscription, StreamStarted: streamStarted, ErrorComponent: "openai.billing_eligibility_check_failed"}); billing.Stop {
		return true
	}
	ctx := service.WithCodexRestrictionRequest(c.Request.Context(), c, body)
	ctx, _ = h.gatewayService.WithOpenAIRequestPricingContext(ctx, apiKey.GroupID)
	session := h.gatewayService.ExtractSessionID(c, body)
	bound := int64(0)
	if session != "" {
		bound, err = runtime.Routes.BoundCredentialPrincipal(ctx, service.CredentialScopeHash(subject.UserID, apiKey.ID), service.CredentialDigest([]byte(session)))
		if err != nil {
			fail("ADMISSION_STORE_UNAVAILABLE")
			return true
		}
	}
	principal, candidates, accounts, err := h.gatewayService.BuildCredentialRoute(ctx, *apiKey.GroupID, model, compact, routes, bound)
	if err != nil {
		fail("NO_AUTHORIZED_CREDENTIAL_INSTANCE")
		return true
	}
	endpoint := "responses"
	if compact {
		endpoint = "compact"
	}
	deadline := time.Now().Add(20 * time.Minute)
	if clientDeadline, ok := ctx.Deadline(); ok && clientDeadline.Before(deadline) {
		deadline = clientDeadline
	}
	input := service.AdmissionInput{RequestID: uuid.NewString(), OwnerNonce: uuid.NewString(), Node: service.RequestIDPrefix(), IdempotencyKey: c.GetHeader("Idempotency-Key"), PayloadDigest: service.CredentialDigest(body), PrincipalID: principal, UserID: subject.UserID, APIKeyID: apiKey.ID, CandidateIDs: candidates, OriginalSession: session, Endpoint: endpoint, Model: model, HasState: gjson.GetBytes(body, "previous_response_id").Exists() || gjson.GetBytes(body, "conversation").Exists(), Deadline: deadline}
	queueDeadline := time.Now().Add(15 * time.Second)
	var snap *service.CredentialExecutionSnapshot
	cancelQueue := func() {
		if store, ok := runtime.Store.(interface {
			CancelQueued(context.Context, service.AdmissionInput) error
		}); ok {
			cleanup, end := context.WithTimeout(context.Background(), 3*time.Second)
			defer end()
			_ = store.CancelQueued(cleanup, input)
		}
	}
	for {
		if ctx.Err() != nil || time.Now().After(queueDeadline) {
			cancelQueue()
			fail("ADMISSION_QUEUE_TIMEOUT")
			return true
		}
		decision, err := runtime.Store.TryAdmit(ctx, input)
		if err != nil {
			if recoverer, ok := runtime.Store.(interface {
				RecoverReserved(context.Context, service.AdmissionInput) (*service.CredentialExecutionSnapshot, error)
			}); ok {
				snap, _ = recoverer.RecoverReserved(ctx, input)
			}
			if snap == nil {
				fail("ADMISSION_STORE_UNAVAILABLE")
				return true
			}
			break
		}
		if decision.Code == service.AdmissionAdmitted {
			snap = decision.Snapshot
			break
		}
		if decision.Code != service.AdmissionWait {
			h.errorResponse(c, http.StatusConflict, decision.Reason, "Grouped request was not admitted")
			return true
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			cancelQueue()
			return true
		case <-timer.C:
		}
	}
	// Existing global user authority remains Redis for every provider. This gate
	// never waits while holding a PostgreSQL reservation; failure cancels it.
	release, acquired, err := h.concurrencyHelper.TryAcquireUserSlotForAPIKey(ctx, subject.UserID, subject.Concurrency, apiKey.ID)
	if err != nil || !acquired {
		cleanup, end := context.WithTimeout(context.Background(), 3*time.Second)
		defer end()
		_ = runtime.Store.Cancel(cleanup, snap.Lease)
		fail("USER_CONCURRENCY_EXCEEDED")
		return true
	}
	if release != nil {
		defer release()
	}
	if c.Request.Context().Err() != nil {
		_ = runtime.Store.Cancel(context.Background(), snap.Lease)
		return true
	}
	account := accounts[snap.AccountID]
	result, forwardErr := h.gatewayService.ForwardCredentialHTTP(ctx, c, account, forwardBody, *snap, runtime.Vault, runtime.Store)
	if result != nil {
		// Existing billing authorization and settlement implementation remains intact;
		// the lease outbox separately records unknown usage and deduplicates attempts.
		_ = h.runOpenAIHTTPUsageStage(c, OpenAIHTTPUsageStage{Handler: h, RequestContext: ctx, Result: result, APIKey: apiKey, Account: account, Subscription: subscription, InboundEndpoint: GetInboundEndpoint(c), UpstreamEndpoint: resolveOpenAIUpstreamEndpoint(c, account, result), RequestPayloadHash: service.HashUsageRequestPayload(body), RequestBody: body, ForwardErrored: forwardErr != nil, Mandatory: true})
	}
	if forwardErr != nil && !c.Writer.Written() {
		fail("UPSTREAM_RESULT_UNKNOWN")
	}
	return true
}
