package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type credentialAdmissionRecorder struct {
	begins   int
	finished FinishAdmissionInput
	beginErr error
}

func (r *credentialAdmissionRecorder) TryAdmit(context.Context, AdmissionInput) (AdmissionDecision, error) {
	return AdmissionDecision{}, nil
}
func (r *credentialAdmissionRecorder) BeginDispatch(context.Context, LeaseRef) error {
	r.begins++
	return r.beginErr
}
func (r *credentialAdmissionRecorder) Heartbeat(context.Context, LeaseRef) error { return nil }
func (r *credentialAdmissionRecorder) Finish(_ context.Context, in FinishAdmissionInput) error {
	r.finished = in
	return nil
}
func (r *credentialAdmissionRecorder) Cancel(context.Context, LeaseRef) error { return nil }

func TestCredentialHTTPFinalReportsThreeEndpoints(t *testing.T) {
	for _, endpoint := range []string{"responses", "passthrough", "compact"} {
		t.Run(endpoint, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			path := "/v1/responses"
			if endpoint == "compact" {
				path += "/compact"
			}
			c.Request = httptest.NewRequest("POST", path, nil)
			c.Request.Header.Set("User-Agent", "codex_cli_rs/0.144.1")
			c.Request.Header.Set("session-id", "original-session")
			c.Request.Header.Set("originator", "codex_cli_rs")
			body := []byte(`{"model":"gpt-5.4","stream":false,"input":[{"role":"user","content":"hello"}],"client_metadata":{"session_id":"original-session","large":9007199254740993,"unknown":null}}`)
			upstreamBody := "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_mock\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\ndata: [DONE]\n\n"
			if endpoint == "compact" {
				upstreamBody = compactProbeSSESuccessBody
			}
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(upstreamBody))}}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream, toolCorrector: NewCodexToolCorrector()}
			account := newTestOAuthAccount(9901, map[string]any{codexFingerprintModeExtraKey: "device"})
			account.Status = StatusActive
			account.Schedulable = true
			account.Concurrency = 10
			account.Credentials = map[string]any{"access_token": "stale-token", "chatgpt_account_id": "mock-account", "chatgpt_user_id": "mock-user"}
			if endpoint == "passthrough" {
				account.Extra["openai_passthrough"] = true
			}
			vault, err := NewCredentialVault(strings.Repeat("ab", 32))
			require.NoError(t, err)
			ciphertext, err := vault.Seal("aad", CredentialSecret{AccessToken: "selected-token"})
			require.NoError(t, err)
			snapshot := CredentialExecutionSnapshot{Lease: LeaseRef{ID: uuid.NewString(), Generation: uuid.NewString()}, AccountID: account.ID, Endpoint: endpoint, InstallationID: "stable-installation", SecretAAD: "aad", SecretCiphertext: ciphertext}
			store := &credentialAdmissionRecorder{}
			_, err = svc.ForwardCredentialHTTP(context.Background(), c, account, body, snapshot, vault, store)
			require.NoError(t, err)
			require.Equal(t, 1, store.begins)
			require.Len(t, upstream.requests, 1)
			require.True(t, store.finished.Complete)
			require.Equal(t, "Bearer selected-token", upstream.lastReq.Header.Get("Authorization"))
			require.Equal(t, "stale-token", account.GetOpenAIAccessToken(), "snapshot must not mutate cached account")
			require.Equal(t, "9007199254740993", gjson.GetBytes(upstream.lastBody, "client_metadata.large").Raw)
			if endpoint != "compact" {
				require.Equal(t, "stable-installation", upstream.lastReq.Header.Get("x-codex-installation-id"))
			} else {
				require.False(t, gjson.GetBytes(upstream.lastBody, "client_metadata.x-codex-installation-id").Exists())
			}
		})
	}
}
func TestCredentialHTTPPartialStreamAndReplay(t *testing.T) {
	e := &credentialHTTPExecution{store: &credentialAdmissionRecorder{}, snapshot: CredentialExecutionSnapshot{AccountID: 1}}
	req := httptest.NewRequest("POST", "http://mock/responses", nil)
	calls := 0
	send := func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n"))}, nil
	}
	resp, err := e.roundTrip(req, 1, send)
	require.NoError(t, err)
	_, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.False(t, e.terminal)
	_, err = e.roundTrip(req, 1, send)
	require.ErrorContains(t, err, "GROUPED_REPLAY_DISABLED")
	require.Equal(t, 1, calls)
}
func TestCredentialFailureRetryAfterNeverShortened(t *testing.T) {
	now := time.Now()
	o := ClassifyCredentialFailure(CredentialExecutionSnapshot{}, 429, http.Header{"Retry-After": []string{"86400"}}, true, now)
	require.Equal(t, now.Add(24*time.Hour), o.RetryAt)
	require.Equal(t, "UNKNOWN", o.Scope)
	o = ClassifyCredentialFailure(CredentialExecutionSnapshot{}, 400, nil, true, now)
	require.Equal(t, "REQUEST", o.Scope)
}

func TestCredentialHTTPAmbiguousCarriersRejected(t *testing.T) {
	for _, body := range []string{`{"a":1,"a":2}`, `{"metadata":{"a":1,"a":2}}`, `{} {}`} {
		require.Error(t, validateCredentialHTTPInput(nil, []byte(body)))
	}
	require.Error(t, validateCredentialHTTPInput(http.Header{"Session-Id": []string{"a", "b"}}, []byte(`{}`)))
	require.NoError(t, validateCredentialHTTPInput(nil, []byte(`{"number":9007199254740993,"null":null}`)))
}
