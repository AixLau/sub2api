package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"

	"github.com/Wei-Shaw/sub2api/internal/securityaudit"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type retryAuditScanner func(context.Context, securityaudit.ActiveEndpoint, string, []string) (*securityaudit.NormalizedResult, error)

func (f retryAuditScanner) Scan(ctx context.Context, endpoint securityaudit.ActiveEndpoint, chunk string, scanners []string) (*securityaudit.NormalizedResult, error) {
	return f(ctx, endpoint, chunk, scanners)
}

type retryAuditEngine struct {
	evaluator *securityaudit.GuardEvaluator
	config    securityaudit.ActiveConfig
}

func (*retryAuditEngine) EffectiveMode() securityaudit.Mode                    { return securityaudit.ModeBlocking }
func (*retryAuditEngine) Enqueue(context.Context, securityaudit.Request) error { return nil }
func (e *retryAuditEngine) Evaluate(ctx context.Context, req securityaudit.Request) (*securityaudit.PromptDecision, error) {
	return e.evaluator.Evaluate(ctx, e.config, securityaudit.PromptSnapshot{RequestID: req.RequestID, ScanText: "hello"})
}

func TestPromptGuardRetriesRecordOnlyFinalRequestFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, recoverOnTenth := range []bool{true, false} {
		name := "ten failures record once"
		if recoverOnTenth {
			name = "tenth attempt succeeds without error record"
		}
		t.Run(name, func(t *testing.T) {
			setupOpsErrorLogTestQueue(t, 16)
			synctest.Test(t, func(t *testing.T) {
				calls := 0
				metrics := securityaudit.NewAtomicMetrics()
				scanner := retryAuditScanner(func(context.Context, securityaudit.ActiveEndpoint, string, []string) (*securityaudit.NormalizedResult, error) {
					calls++
					require.Zero(t, OpsErrorLogQueueLength(), "intermediate failures must not create request errors")
					require.Zero(t, metrics.Snapshot().Unavailable, "count only the final audit outcome")
					if calls == 10 && recoverOnTenth {
						return securityaudit.ParseQwen3Guard("Safety: Safe\nCategories: None", securityaudit.AllScannerIDs)
					}
					return nil, &securityaudit.GuardError{Code: securityaudit.ErrorCodeUnavailable, HTTPStatus: http.StatusForbidden}
				})
				engine := &retryAuditEngine{
					evaluator: securityaudit.NewGuardEvaluator(scanner, nil, metrics),
					config: securityaudit.ActiveConfig{
						MaxAttempts: 10, Scanners: securityaudit.AllScannerIDs,
						Endpoints: []securityaudit.ActiveEndpoint{{ID: "guard", Enabled: true, TimeoutMS: 30000, InputLimit: 100}},
					},
				}
				coordinator := securityaudit.NewCoordinator(nil, engine)
				ops := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
				router := gin.New()
				router.Use(OpsErrorLoggerMiddleware(ops))
				router.POST("/v1/responses", func(c *gin.Context) {
					decision := runSecurityAudit(c, nil, coordinator, nil, nil, middleware2.AuthSubject{UserID: 7}, "openai_responses", "gpt-test", nil, "http")
					if !decision.AllowNextStage {
						(&OpenAIGatewayHandler{}).openAISecurityAuditError(c, decision)
						return
					}
					c.JSON(http.StatusOK, gin.H{"ok": true})
				})
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/responses", nil))
				require.Equal(t, 10, calls)
				if recoverOnTenth {
					require.Equal(t, http.StatusOK, recorder.Code)
					require.Zero(t, OpsErrorLogQueueLength())
					require.Zero(t, OpsErrorLogEnqueuedTotal())
					require.Zero(t, metrics.Snapshot().Unavailable)
				} else {
					require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
					require.Equal(t, int64(1), OpsErrorLogQueueLength())
					require.Equal(t, int64(1), OpsErrorLogEnqueuedTotal())
					require.Equal(t, int64(1), metrics.Snapshot().Unavailable)
					job := <-opsErrorLogQueue
					require.Equal(t, http.StatusServiceUnavailable, job.entry.StatusCode)
					require.Contains(t, job.entry.ErrorBody, securityaudit.ErrorCodeUnavailable)
				}
			})
		})
	}
}
