package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (s *AccountTestService) testCredentialAccount(c *gin.Context, account *Account, model, prompt, mode string) error {
	runtime := s.credentialHTTP
	if runtime == nil || !runtime.Enabled || runtime.Vault == nil || s.openaiGatewayService == nil {
		return s.sendErrorAndEnd(c, "GROUPED_HTTP_DISABLED")
	}
	route, actor, err := runtime.Routes.CredentialProbeRoute(c.Request.Context(), account.ID)
	if err != nil || actor == 0 {
		return s.sendErrorAndEnd(c, "MAINTENANCE_AUTHORIZATION_REQUIRED")
	}
	endpoint := "probe"
	path := "/v1/responses"
	var payload map[string]any
	if normalizeAccountTestMode(mode) == AccountTestModeCompact {
		endpoint = "compact"
		path += "/compact"
		payload = createOpenAICompactProbePayload(model, true)
	} else {
		payload = createOpenAITestPayload(model, true, prompt)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	input := AdmissionInput{Maintenance: true, RequestID: uuid.NewString(), OwnerNonce: uuid.NewString(), Node: RequestIDPrefix(), PayloadDigest: CredentialDigest(body), PrincipalID: route.PrincipalID, UserID: actor, CandidateIDs: []int64{route.InstanceID}, Endpoint: endpoint, Model: model, Deadline: time.Now().Add(time.Minute)}
	decision, err := runtime.Store.TryAdmit(c.Request.Context(), input)
	if err != nil {
		return s.sendErrorAndEnd(c, "ADMISSION_STORE_UNAVAILABLE")
	}
	if decision.Code != AdmissionAdmitted {
		if q, ok := runtime.Store.(interface {
			CancelQueued(context.Context, AdmissionInput) error
		}); ok {
			_ = q.CancelQueued(c.Request.Context(), input)
		}
		return s.sendErrorAndEnd(c, decision.Reason)
	}
	recorder := httptest.NewRecorder()
	upstreamContext, _ := gin.CreateTestContext(recorder)
	upstreamContext.Request, _ = http.NewRequestWithContext(c.Request.Context(), "POST", path, bytes.NewReader(body))
	SetOpenAIClientTransport(upstreamContext, OpenAIClientTransportHTTP)
	result, err := s.openaiGatewayService.ForwardCredentialHTTP(c.Request.Context(), upstreamContext, account, body, *decision.Snapshot, runtime.Vault, runtime.Store)
	if err != nil {
		return s.sendErrorAndEnd(c, "GROUPED_PROBE_FAILED_OR_UNKNOWN")
	}
	if result == nil {
		return errors.New("GROUPED_PROBE_RESULT_UNAVAILABLE")
	}
	usage := &openAIAccountTestUsage{account: account, model: model, requestID: result.RequestID, usage: result.Usage, stream: result.Stream}
	s.sendEvent(c, TestEvent{Type: "test_start", Model: model})
	return s.completeOpenAIAccountTest(c, newAccountTestMetrics(), usage)
}
