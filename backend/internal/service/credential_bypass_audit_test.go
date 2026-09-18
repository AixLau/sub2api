package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type auditControlledRoutes struct{ CredentialRouteStore }

func (auditControlledRoutes) IsControlledCredentialAccount(context.Context, int64) (bool, error) {
	return true, nil
}
func (auditControlledRoutes) CredentialProbeRoute(context.Context, int64) (CredentialRouteCandidate, int64, error) {
	return CredentialRouteCandidate{PrincipalID: 11, InstanceID: 22, AccountID: 1}, 7, nil
}

type auditProbeAdmission struct {
	PrincipalAdmissionStore
	in    AdmissionInput
	calls int
}

func (s *auditProbeAdmission) TryAdmit(_ context.Context, in AdmissionInput) (AdmissionDecision, error) {
	s.calls++
	s.in = in
	return AdmissionDecision{Code: AdmissionRejected, Reason: "MOCK_MAINTENANCE_LIMIT"}, nil
}
func TestCredentialBackgroundProbeUsesMaintenanceAdmission(t *testing.T) {
	vault, err := NewCredentialVault(strings.Repeat("ab", 32))
	require.NoError(t, err)
	store := &auditProbeAdmission{}
	account := Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "stale-cache-token"}}
	svc := &AccountTestService{accountRepo: stubOpenAIAccountRepo{accounts: []Account{account}}, credentialHTTP: &CredentialHTTPRuntime{Enabled: true, Routes: auditControlledRoutes{}, Store: store, Vault: vault}, openaiGatewayService: &OpenAIGatewayService{}}
	result, err := svc.RunTestBackground(context.Background(), 1, "mock-model")
	require.NoError(t, err)
	require.Equal(t, "failed", result.Status)
	require.Equal(t, 1, store.calls)
	require.Equal(t, "MOCK_MAINTENANCE_LIMIT", result.ErrorMessage)
	require.True(t, store.in.Maintenance)
	require.Equal(t, int64(11), store.in.PrincipalID)
	require.Equal(t, int64(7), store.in.UserID)
	require.Equal(t, []int64{22}, store.in.CandidateIDs)
	require.Equal(t, "probe", store.in.Endpoint)
	// A disabled runtime cannot fall through to stale legacy credentials.
	svc.credentialHTTP.Enabled = false
	result, err = svc.RunTestBackground(context.Background(), 1, "mock-model")
	require.NoError(t, err)
	require.Equal(t, "GROUPED_HTTP_DISABLED", result.ErrorMessage)
	require.Equal(t, 1, store.calls)
}
func TestCredentialGroupedPluginNeverReceivesSnapshot(t *testing.T) {
	store := &credentialAdmissionRecorder{}
	execution := &credentialHTTPExecution{store: store, snapshot: CredentialExecutionSnapshot{AccountID: 1}}
	req := httptest.NewRequest("POST", "http://mock/responses", nil)
	req = req.WithContext(context.WithValue(req.Context(), credentialHTTPExecutionKey{}, execution))
	// An uninitialized plugin would panic if invoked. The grouped path rejects
	// before dispatch/transport; no plugin contract is invented by this test.
	svc := &OpenAIGatewayService{pluginManager: &PluginManager{}}
	_, err := svc.doOpenAIUpstream(req, "", &Account{ID: 1})
	require.ErrorIs(t, err, ErrAdmissionOwnership)
	require.Zero(t, store.begins)
}

type panicCredentialHTTP struct{ HTTPUpstream }

func (panicCredentialHTTP) Do(*http.Request, string, int64, int) (*http.Response, error) {
	panic("opaque-upstream-authorization-canary")
}
func TestCredentialHTTPHookPanicDoesNotEscapeOrProveCompletion(t *testing.T) {
	t.Run("send", func(t *testing.T) { checkCredentialHTTPHookPanic(t, panicCredentialHTTP{}) })
	t.Run("after_terminal", func(t *testing.T) {
		upstream := &terminalPanicCredentialHTTP{}
		checkCredentialHTTPHookPanic(t, upstream)
		require.True(t, upstream.sawTerminal, "panic happened after terminal observation but before adapter result/receipt")
	})
}

type terminalPanicCredentialHTTP struct {
	HTTPUpstream
	sawTerminal bool
}
type terminalPanicBody struct {
	io.Reader
	execution *credentialHTTPExecution
	owner     *terminalPanicCredentialHTTP
}

func (b *terminalPanicBody) Close() error {
	b.execution.mu.Lock()
	b.owner.sawTerminal = b.execution.terminal
	b.execution.mu.Unlock()
	panic("opaque-upstream-authorization-canary after terminal")
}
func (u *terminalPanicCredentialHTTP) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body := `data: {"type":"response.completed","response":{"id":"resp_mock","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}` + "\n\ndata: [DONE]\n\n"
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: &terminalPanicBody{Reader: strings.NewReader(body), execution: credentialExecutionFromContext(req.Context()), owner: u}}, nil
}
func checkCredentialHTTPHookPanic(t *testing.T, upstream HTTPUpstream) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	c.Request.Header.Set("User-Agent", "codex_cli_rs/0.144.1")
	body := []byte(`{"model":"gpt-5.4","stream":false,"input":[{"role":"user","content":"hello"}]}`)
	vault, err := NewCredentialVault(strings.Repeat("ab", 32))
	require.NoError(t, err)
	sealed, err := vault.Seal("aad", CredentialSecret{AccessToken: "opaque-upstream-authorization-canary"})
	require.NoError(t, err)
	account := newTestOAuthAccount(9901, map[string]any{codexFingerprintModeExtraKey: "device"})
	snapshot := CredentialExecutionSnapshot{Lease: LeaseRef{ID: uuid.NewString(), Generation: uuid.NewString()}, AccountID: account.ID, Endpoint: "responses", InstallationID: "stable", SecretAAD: "aad", SecretCiphertext: sealed}
	store := &credentialAdmissionRecorder{}
	gateway := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream, toolCorrector: NewCodexToolCorrector()}
	_, err = gateway.ForwardCredentialHTTP(context.Background(), c, account, body, snapshot, vault, store)
	require.EqualError(t, err, "CREDENTIAL_HTTP_EXECUTION_PANIC")
	require.Equal(t, 1, store.begins)
	require.False(t, store.finished.Complete)
	require.Equal(t, snapshot.Lease, store.finished.Lease)
}
