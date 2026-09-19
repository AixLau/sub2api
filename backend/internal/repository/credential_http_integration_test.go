//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type credentialMockHTTP struct {
	url     string
	request *http.Request
	body    []byte
}

func (u *credentialMockHTTP) Do(r *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.request = r.Clone(r.Context())
	u.body, _ = io.ReadAll(r.Body)
	r.Body.Close()
	req, err := http.NewRequestWithContext(r.Context(), r.Method, u.url+r.URL.Path, strings.NewReader(string(u.body)))
	if err != nil {
		return nil, err
	}
	req.Header = r.Header.Clone()
	return http.DefaultClient.Do(req)
}
func (u *credentialMockHTTP) DoWithTLS(r *http.Request, p string, a int64, c int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(r, p, a, c)
}
func TestCredentialHTTPWithPostgresLedgerAndMockUpstream(t *testing.T) {
	for _, endpoint := range []string{"responses", "passthrough", "compact"} {
		t.Run(endpoint, func(t *testing.T) {
			f := newAdmissionFixture(t, 1)
			ctx := context.Background()
			vault, err := service.NewCredentialVault(strings.Repeat("ab", 32))
			require.NoError(t, err)
			sealed, err := vault.Seal("fixture", service.CredentialSecret{AccessToken: "mock-selected-token", AccountSubject: "mock-account", UserSubject: "mock-user"})
			require.NoError(t, err)
			_, err = integrationDB.Exec(`UPDATE credential_secrets SET secret_ciphertext=$2 WHERE instance_id=$1`, f.instances[0], sealed)
			require.NoError(t, err)
			var accountID int64
			require.NoError(t, integrationDB.QueryRow(`SELECT account_id FROM credential_instances WHERE id=$1`, f.instances[0]).Scan(&accountID))
			if endpoint == "passthrough" {
				tx, err := integrationDB.BeginTx(ctx, nil)
				require.NoError(t, err)
				_, err = tx.Exec(`SET LOCAL sub2api.credential_control='on'`)
				require.NoError(t, err)
				_, err = tx.Exec(`UPDATE accounts SET extra='{"openai_passthrough":true}' WHERE id=$1`, accountID)
				require.NoError(t, err)
				require.NoError(t, tx.Commit())
			}
			repo := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, nil)
			account, err := repo.GetByID(ctx, accountID)
			require.NoError(t, err)
			store := NewPrincipalAdmissionStore(integrationDB)
			in := f.input()
			in.CandidateIDs = f.instances[:1]
			in.Endpoint = endpoint
			d, err := store.TryAdmit(ctx, in)
			require.NoError(t, err)
			require.Equal(t, service.AdmissionAdmitted, d.Code)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "Bearer mock-selected-token", r.Header.Get("Authorization"))
				assertAdmissionLedger(t, f, 1)
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_fixture\",\"status\":\"completed\",\"output\":[{\"type\":\"compaction\",\"encrypted_content\":\"mock\"}],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\ndata: [DONE]\n\n")
			}))
			defer server.Close()
			upstream := &credentialMockHTTP{url: server.URL}
			svc := service.NewOpenAIGatewayService(nil, repo, nil, nil, nil, nil, nil, nil, &config.Config{}, nil, nil, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			path := "/v1/responses"
			if endpoint == "compact" {
				path += "/compact"
			}
			c.Request = httptest.NewRequest("POST", path, nil)
			c.Request.Header.Set("User-Agent", "codex_cli_rs/0.144.1")
			c.Request.Header.Set("session-id", "mock-session")
			c.Request.Header.Set("originator", "codex_cli_rs")
			service.SetOpenAIClientTransport(c, service.OpenAIClientTransportHTTP)
			body := []byte(`{"model":"gpt-5.4","stream":false,"input":[{"role":"user","content":"mock"}],"client_metadata":{"number":9007199254740993,"session_id":"mock-session"}}`)
			_, err = svc.ForwardCredentialHTTP(ctx, c, account, body, *d.Snapshot, vault, store)
			require.NoError(t, err)
			assertAdmissionLedger(t, f, 0)
			var parsed map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(upstream.body, &parsed))
			require.Contains(t, string(parsed["client_metadata"]), "9007199254740993")
		})
	}
}
