package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/securityaudit"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// scopedTurnCountingEngine mimics PromptService in selected-account mode.
type scopedTurnCountingEngine struct {
	turnCountingEngine
	accounts map[int64]bool
}

func (*scopedTurnCountingEngine) RequiresSelectedAccount() bool { return true }
func (e *scopedTurnCountingEngine) AuditsSelectedAccount(req securityaudit.Request) bool {
	return e.accounts[req.AccountID]
}

func newSelectedAccountTestContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	return c
}

func runSelectedAccountAuditForTest(c *gin.Context, coordinator *securityaudit.Coordinator, body []byte, stage string, accountID int64) *securityaudit.Decision {
	account := &service.Account{ID: accountID, Platform: "openai", Type: "oauth"}
	return runSelectedAccountPromptAudit(c, coordinator, nil, middleware2.AuthSubject{UserID: 7}, "openai_responses", "gpt-test", body, stage, account)
}

func TestSelectedAccountPromptAuditReusesAllowAcrossFailover(t *testing.T) {
	engine := &scopedTurnCountingEngine{turnCountingEngine: turnCountingEngine{mode: securityaudit.ModeBlocking}, accounts: map[int64]bool{1: true, 2: true}}
	coordinator := securityaudit.NewCoordinator(nil, engine)
	c := newSelectedAccountTestContext()
	body := []byte(`{"input":"hello"}`)

	first := runSelectedAccountAuditForTest(c, coordinator, body, "http", 1)
	second := runSelectedAccountAuditForTest(c, coordinator, body, "http", 2)

	require.NotNil(t, first)
	require.NotNil(t, second)
	require.True(t, second.AllowNextStage)
	require.Equal(t, int64(1), engine.evaluates.Load(), "failover to another in-scope account must reuse the audit")

	runSelectedAccountAuditForTest(c, coordinator, []byte(`{"input":"changed"}`), "http", 2)
	require.Equal(t, int64(2), engine.evaluates.Load(), "a different body must be audited again")
}

func TestSelectedAccountPromptAuditOutOfScopeDoesNotPopulateCache(t *testing.T) {
	engine := &scopedTurnCountingEngine{turnCountingEngine: turnCountingEngine{mode: securityaudit.ModeBlocking}, accounts: map[int64]bool{2: true}}
	coordinator := securityaudit.NewCoordinator(nil, engine)
	c := newSelectedAccountTestContext()
	body := []byte(`{"input":"hello"}`)

	require.Nil(t, runSelectedAccountAuditForTest(c, coordinator, body, "http", 1))
	_, cached := c.Get(securityAuditSelectedAccountContextKey)
	require.False(t, cached)
	require.Zero(t, engine.evaluates.Load())

	decision := runSelectedAccountAuditForTest(c, coordinator, body, "http", 2)
	require.NotNil(t, decision)
	require.Equal(t, int64(1), engine.evaluates.Load(), "an in-scope account must still be audited after an out-of-scope attempt")
}

func TestSelectedAccountPromptAuditDoesNotCacheNonAllowDecisions(t *testing.T) {
	engine := &scopedTurnCountingEngine{
		turnCountingEngine: turnCountingEngine{
			mode: securityaudit.ModeBlocking,
			decisions: []*securityaudit.PromptDecision{
				{Kind: securityaudit.DecisionUnavailable, AllowNextStage: false},
				{Kind: securityaudit.DecisionFlag, AllowNextStage: true},
			},
		},
		accounts: map[int64]bool{1: true, 2: true},
	}
	coordinator := securityaudit.NewCoordinator(nil, engine)
	c := newSelectedAccountTestContext()
	body := []byte(`{"input":"hello"}`)

	require.False(t, runSelectedAccountAuditForTest(c, coordinator, body, "http", 1).AllowNextStage)
	require.Equal(t, securityaudit.DecisionFlag, runSelectedAccountAuditForTest(c, coordinator, body, "http", 2).Kind)
	require.Equal(t, securityaudit.DecisionAllow, runSelectedAccountAuditForTest(c, coordinator, body, "http", 1).Kind)
	require.Equal(t, int64(3), engine.evaluates.Load())
}

func TestSelectedAccountPromptAuditCacheIsScopedToWebSocketTurn(t *testing.T) {
	engine := &scopedTurnCountingEngine{turnCountingEngine: turnCountingEngine{mode: securityaudit.ModeBlocking}, accounts: map[int64]bool{1: true, 2: true}}
	coordinator := securityaudit.NewCoordinator(nil, engine)
	c := newSelectedAccountTestContext()
	payload := []byte(`{"type":"response.create","response":{"input":"same"}}`)

	c.Set(securityAuditWSTurnContextKey, 2)
	runSelectedAccountAuditForTest(c, coordinator, payload, "subsequent_turn", 1)
	runSelectedAccountAuditForTest(c, coordinator, payload, "subsequent_turn", 2)
	require.Equal(t, int64(1), engine.evaluates.Load())

	c.Set(securityAuditWSTurnContextKey, 3)
	runSelectedAccountAuditForTest(c, coordinator, payload, "subsequent_turn", 1)
	require.Equal(t, int64(2), engine.evaluates.Load(), "a new WebSocket turn must be audited again")
}

