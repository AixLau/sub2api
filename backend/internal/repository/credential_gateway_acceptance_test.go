//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/pkg/moderationcoverage"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Only the provider address is replaced. The real Gin entrypoint, auth,
// permission checks, Redis slots, PostgreSQL admission and billing run unchanged.
type acceptanceUpstream struct {
	url      string
	mu       sync.Mutex
	accounts []int64
}

func (u *acceptanceUpstream) Do(req *http.Request, _ string, id int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	u.accounts = append(u.accounts, id)
	u.mu.Unlock()
	next, err := http.NewRequestWithContext(req.Context(), req.Method, u.url+req.URL.Path, req.Body)
	if err != nil {
		return nil, err
	}
	next.Header = req.Header.Clone()
	return http.DefaultClient.Do(next)
}
func (u *acceptanceUpstream) DoWithTLS(req *http.Request, p string, id int64, n int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, p, id, n)
}

type acceptanceGatewayFixture struct {
	*httptest.Server
	gateway    *service.OpenAIGatewayService
	keys       service.APIKeyRepository
	keyService *service.APIKeyService
}

func acceptanceGateway(t *testing.T, upstream service.HTTPUpstream, store service.PrincipalAdmissionStore) *acceptanceGatewayFixture {
	t.Helper()
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	cfg.Gateway.MultiCredentialHTTPEnabled = true
	cfg.Gateway.CredentialVaultKey = strings.Repeat("ab", 32)
	accounts := NewAccountRepository(integrationEntClient, integrationDB, nil)
	users := NewUserRepository(integrationEntClient, integrationDB)
	groups := NewGroupRepository(integrationEntClient, integrationDB)
	keys := NewAPIKeyRepository(integrationEntClient, integrationDB)
	subs := NewUserSubscriptionRepository(integrationEntClient)
	rates := NewUserGroupRateRepository(integrationDB)
	cc := service.NewConcurrencyService(NewConcurrencyCache(integrationRedis, 15, 15))
	bc := service.NewBillingCacheService(NewBillingCache(integrationRedis), users, subs, keys, nil, rates, cfg, nil)
	t.Cleanup(bc.Stop)
	billing := service.NewBillingService(cfg, nil)
	deferred := service.NewDeferredService(accounts, nil, time.Second)
	gateway := service.NewOpenAIGatewayService(nil, accounts, NewUsageLogRepository(integrationEntClient, integrationDB), NewUsageBillingRepository(integrationEntClient, integrationDB), users, subs, rates, NewGatewayCache(integrationRedis), cfg, nil, cc, billing, nil, bc, upstream, deferred, nil, nil, nil, nil, nil, nil, nil)
	settings := NewSettingRepository(integrationEntClient)
	require.NoError(t, settings.Set(context.Background(), service.SettingKeyRiskControlEnabled, "false"))
	moderation := service.NewContentModerationService(settings, NewContentModerationRepository(integrationDB), nil, groups, users, nil, nil)
	keyService := service.NewAPIKeyService(keys, users, groups, subs, rates, nil, cfg)
	runtime, err := service.NewCredentialHTTPRuntime(store, NewCredentialRouteStore(integrationDB), cfg)
	require.NoError(t, err)
	h := handler.ProvideOpenAIGatewayHandler(runtime, gateway, cc, bc, keyService, nil, nil, moderation, nil, nil, cfg, nil)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(gin.HandlerFunc(middleware.NewAPIKeyAuthMiddleware(keyService, nil, cfg)))
	for _, path := range []string{"/v1/responses", "/v1/responses/compact"} {
		meta := moderationcoverage.AnnotatePipelineCoverage(moderationcoverage.Entry{Method: "POST", Path: path, Handler: "OpenAIGatewayHandler.Responses", Upstream: true, ModerationRequired: true, Protocol: service.ContentModerationProtocolOpenAIResponses, Pipeline: moderationcoverage.PipelineOpenAIHTTP})
		router.POST(path, func(c *gin.Context) {
			moderationcoverage.SetRouteMeta(c, meta)
			if entry := h.EnterOpenAIHTTPGatewayPipeline(c, meta); !entry.Stop {
				h.Responses(c)
			}
		})
	}
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return &acceptanceGatewayFixture{Server: server, gateway: gateway, keys: keys, keyService: keyService}
}
func prepareAcceptanceIdentity(t *testing.T, f admissionFixture, user int64) {
	t.Helper()
	vault, err := service.NewCredentialVault(strings.Repeat("ab", 32))
	require.NoError(t, err)
	sealed, err := vault.Seal("fixture", service.CredentialSecret{AccessToken: "mock-access", AccountSubject: "mock-account", UserSubject: "mock-user"})
	require.NoError(t, err)
	_, err = integrationDB.Exec(`UPDATE credential_secrets s SET secret_ciphertext=$2 FROM credential_instances i WHERE s.instance_id=i.id AND i.principal_id=$1`, f.principal, sealed)
	require.NoError(t, err)
	_, err = integrationDB.Exec(`UPDATE api_keys SET user_id=$2 WHERE id=$1`, f.key, user)
	require.NoError(t, err)
	_, err = integrationDB.Exec(`UPDATE users SET balance=100,concurrency=1 WHERE id=$1`, user)
	require.NoError(t, err)
}
func acceptanceRequest(ctx context.Context, url, key, session string) (int, string, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(`{"model":"gpt-5.4","stream":false,"input":[{"role":"user","content":"acceptance fixture"}]}`))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("session-id", session)
	req.Header.Set("User-Agent", "codex_cli_rs/0.144.1")
	req.Header.Set("originator", "codex_cli_rs")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body), err
}
func acceptanceKey(t *testing.T, id int64) string {
	var key string
	require.NoError(t, integrationDB.QueryRow(`SELECT key FROM api_keys WHERE id=$1`, id).Scan(&key))
	return key
}
func acceptanceTerminal(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_"+uuid.NewString()+"\",\"status\":\"completed\",\"model\":\"gpt-5.4\",\"output\":[],\"usage\":{\"input_tokens\":100,\"output_tokens\":20}}}\n\ndata: [DONE]\n\n")
}
func TestCredentialGatewayGlobalUserLimitAndBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	first := newAdmissionFixture(t, 10)
	second := newAdmissionFixture(t, 10)
	prepareAcceptanceIdentity(t, first, first.user)
	prepareAcceptanceIdentity(t, second, first.user)
	var starts atomic.Int64
	blocked := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		starts.Add(1)
		once.Do(func() { close(blocked); <-release })
		acceptanceTerminal(w)
	}))
	defer upstream.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	server := acceptanceGateway(t, &acceptanceUpstream{url: upstream.URL}, NewPrincipalAdmissionStore(integrationDB))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	done := make(chan struct{})
	var firstStatus int
	var firstBody string
	var firstErr error
	go func() {
		defer close(done)
		firstStatus, firstBody, firstErr = acceptanceRequest(ctx, server.URL+"/v1/responses", acceptanceKey(t, first.key), "session-a")
	}()
	select {
	case <-blocked:
	case <-done:
		t.Fatalf("request did not reach upstream: status=%d body=%s err=%v", firstStatus, firstBody, firstErr)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// Same user, different key and principal must share one Redis allowance.
	status, body, err := acceptanceRequest(ctx, server.URL+"/v1/responses", acceptanceKey(t, second.key), "session-b")
	require.NoError(t, err)
	require.Equal(t, 503, status, body)
	require.Contains(t, body, "USER_CONCURRENCY_EXCEEDED")
	require.Equal(t, int64(1), starts.Load())
	assertAdmissionLedger(t, first, 1)
	assertAdmissionLedger(t, second, 0)
	require.Equal(t, int64(1), integrationRedis.ZCard(ctx, fmt.Sprintf("concurrency:user:%d", first.user)).Val())
	for _, f := range []admissionFixture{first, second} {
		var ids []int64
		rows, err := integrationDB.Query(`SELECT account_id FROM credential_instances WHERE principal_id=$1`, f.principal)
		require.NoError(t, err)
		for rows.Next() {
			var id int64
			require.NoError(t, rows.Scan(&id))
			ids = append(ids, id)
		}
		rows.Close()
		for _, id := range ids {
			require.Zero(t, integrationRedis.ZCard(ctx, fmt.Sprintf("concurrency:account:%d", id)).Val())
		}
	}
	close(release)
	<-done
	require.NoError(t, firstErr)
	require.Equal(t, 200, firstStatus, firstBody)
	assertAdmissionLedger(t, first, 0)
	require.Zero(t, integrationRedis.ZCard(ctx, fmt.Sprintf("concurrency:user:%d", first.user)).Val())
	var bills int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM credential_billing_outbox b JOIN request_leases l ON l.id=b.lease_id WHERE l.principal_id=$1 AND b.settled_at IS NOT NULL`, first.principal).Scan(&bills))
	require.Equal(t, 1, bills)
	var balance float64
	require.NoError(t, integrationDB.QueryRow(`SELECT balance FROM users WHERE id=$1`, first.user).Scan(&balance))
	require.Less(t, balance, 100.0)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(firstBody), &parsed))
}

func TestCredentialGatewayLegacyAndGroupedShareUserCapacity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	grouped := newAdmissionFixture(t, 10)
	prepareAcceptanceIdentity(t, grouped, grouped.user)
	var group, key, account int64
	secret := uuid.NewString()
	require.NoError(t, integrationDB.QueryRow(`INSERT INTO groups(name,platform) VALUES($1,'openai') RETURNING id`, secret).Scan(&group))
	require.NoError(t, integrationDB.QueryRow(`INSERT INTO accounts(name,platform,type,status,schedulable,concurrency,credentials) VALUES('mock legacy','openai','apikey','active',true,10,'{"api_key":"mock-legacy-key"}') RETURNING id`).Scan(&account))
	_, err := integrationDB.Exec(`INSERT INTO account_groups(account_id,group_id) VALUES($1,$2)`, account, group)
	require.NoError(t, err)
	require.NoError(t, integrationDB.QueryRow(`INSERT INTO api_keys(user_id,group_id,key,name) VALUES($1,$2,$3,'mock legacy') RETURNING id`, grouped.user, group, secret).Scan(&key))
	blocked := make(chan struct{})
	release := make(chan struct{})
	var starts atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		starts.Add(1)
		close(blocked)
		<-release
		acceptanceTerminal(w)
	}))
	defer upstream.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	server := acceptanceGateway(t, &acceptanceUpstream{url: upstream.URL}, NewPrincipalAdmissionStore(integrationDB))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	done := make(chan struct{})
	var code int
	var body string
	var callErr error
	go func() {
		defer close(done)
		code, body, callErr = acceptanceRequest(ctx, server.URL+"/v1/responses", secret, "legacy-session")
	}()
	select {
	case <-blocked:
	case <-done:
		t.Fatalf("legacy did not forward status=%d body=%s error=%v", code, body, callErr)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	status, rejected, err := acceptanceRequest(ctx, server.URL+"/v1/responses", acceptanceKey(t, grouped.key), "grouped-session")
	require.NoError(t, err)
	require.Equal(t, 503, status, rejected)
	require.Contains(t, rejected, "USER_CONCURRENCY_EXCEEDED")
	require.Equal(t, int64(1), starts.Load())
	assertAdmissionLedger(t, grouped, 0)
	require.Equal(t, int64(1), integrationRedis.ZCard(ctx, fmt.Sprintf("concurrency:user:%d", grouped.user)).Val())
	require.Equal(t, int64(1), integrationRedis.ZCard(ctx, fmt.Sprintf("concurrency:account:%d", account)).Val())
	close(release)
	<-done
	require.NoError(t, callErr)
	require.Equal(t, 200, code, body)
	require.Zero(t, integrationRedis.ZCard(ctx, fmt.Sprintf("concurrency:user:%d", grouped.user)).Val())
	require.Zero(t, integrationRedis.ZCard(ctx, fmt.Sprintf("concurrency:account:%d", account)).Val())
}

type acceptanceDispatchFault struct {
	service.PrincipalAdmissionStore
}

func (s acceptanceDispatchFault) BeginDispatch(context.Context, service.LeaseRef) error {
	return service.ErrAdmissionStoreUnavailable
}
func TestCredentialGatewayBeforeDispatchCompensation(t *testing.T) {
	f := newAdmissionFixture(t, 10)
	prepareAcceptanceIdentity(t, f, f.user)
	var starts atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { starts.Add(1); acceptanceTerminal(w) }))
	defer upstream.Close()
	store := acceptanceDispatchFault{NewPrincipalAdmissionStore(integrationDB)}
	server := acceptanceGateway(t, &acceptanceUpstream{url: upstream.URL}, store)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	status, body, err := acceptanceRequest(ctx, server.URL+"/v1/responses", acceptanceKey(t, f.key), "fault")
	require.NoError(t, err)
	require.GreaterOrEqual(t, status, 400, body)
	require.Zero(t, starts.Load())
	assertAdmissionLedger(t, f, 0)
	require.Zero(t, integrationRedis.ZCard(ctx, fmt.Sprintf("concurrency:user:%d", f.user)).Val())
	var bills int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM credential_billing_outbox b JOIN request_leases l ON l.id=b.lease_id WHERE l.principal_id=$1`, f.principal).Scan(&bills))
	require.Zero(t, bills)
}
