//go:build integration

package repository

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/credentialfence"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// This is deliberately opt-in: the supplied binary MUST be a normal cmd/server
// build, with production Wire wiring. No replacement handler or verifier is
// injected into these gateway processes. Only the external provider is a mock.
func TestCredentialFullGatewayOfflineRollout(t *testing.T) {
	server := os.Getenv("SUB2API_ACCEPTANCE_SERVER_BINARY")
	mock := os.Getenv("SUB2API_ACCEPTANCE_MOCK_BINARY")
	if server == "" || mock == "" {
		t.Skip("requires separately built Linux cmd/server and testdata/credential-rollout-mock binaries")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	fixture := newFullGatewayRollout(t, ctx, server, mock)
	var pgVersion string
	require.NoError(t, integrationDB.QueryRow(`SELECT version()`).Scan(&pgVersion))
	policy, err := integrationRedis.ConfigGet(ctx, "maxmemory-policy").Result()
	require.NoError(t, err)
	require.Equal(t, "noeviction", policy["maxmemory-policy"])
	t.Logf("topology postgres=%s redis_policy=%s gateway_processes=3 database_max_connections_per_process=8 isolated_network=true", pgVersion, policy["maxmemory-policy"])
	var actor int64
	require.NoError(t, integrationDB.QueryRow(`INSERT INTO users(email,password_hash,role,balance,concurrency) VALUES($1,'fixture','admin',100,10) RETURNING id`, uuid.NewString()+"@example.test").Scan(&actor))
	settings := NewSettingRepository(integrationEntClient)
	require.NoError(t, settings.Set(ctx, service.SettingKeyRiskControlEnabled, "false"))
	a := fixture.account(actor)
	b := fixture.account(actor)
	old := fixture.startNodes(false)
	for _, node := range old {
		require.Equal(t, http.StatusUnauthorized, fixture.requestStatus(node, "invalid-key"))
	}
	// Run a complete legacy HTTP request on every actual server before the fence.
	for _, node := range old {
		require.Equal(t, http.StatusOK, fixture.requestStatus(node, a.key))
	}
	require.Eventually(t, func() bool { return fixture.scalar(`SELECT count(*) FROM usage_logs WHERE user_id=$1`, actor) == 3 }, 10*time.Second, 20*time.Millisecond)
	before := fixture.mockStats()["calls"]
	require.Equal(t, int64(3), before)
	// The existing account INSERT trigger canonicalizes billing defaults. Pin
	// the persisted pre-cutover state after the legacy nodes ran, rather than
	// comparing with the pre-trigger fixture literal.
	require.NoError(t, integrationDB.QueryRow(`SELECT extra::text FROM accounts WHERE id=$1`, a.id).Scan(&a.extra))
	require.NoError(t, integrationDB.QueryRow(`SELECT extra::text FROM accounts WHERE id=$1`, b.id).Scan(&b.extra))
	vault, err := service.NewCredentialVault(strings.Repeat("ab", 32))
	require.NoError(t, err)
	imports := service.NewCredentialImportService(NewCredentialImportRepository(integrationDB), vault, mockCredentialVerifier{})
	fence := credentialfence.Docker{Project: fixture.project, Service: "gateway"}
	rollout := NewCredentialRollout(integrationDB, vault, fence)
	migrate := func(a fullRolloutAccount) int64 {
		// This test verifier only recognizes synthetic mock credentials. The
		// production importer has no verifier and remains UNVERIFIED.
		imported, err := imports.Import(ctx, actor, uuid.NewString(), a.secret)
		require.NoError(t, err)
		id, err := rollout.Migrate(ctx, CredentialMigrationInput{ActorID: actor, AccountID: a.id, ImportID: imported.ID, OperationID: uuid.NewString(), RequestedLimit: 5, DrainEvidence: "isolated mock has completed all 3 old HTTP requests; usage rows persisted; no active old requests or reused sessions"})
		require.NoError(t, err)
		return id
	}
	pa, pb := migrate(a), migrate(b)
	fixture.verifyFence(fence)
	// A stopped old cmd/server cannot regain connectivity simply by restarting.
	fixture.docker("start", old[0].id)
	require.Equal(t, "0", fixture.docker("inspect", "--format", "{{len .NetworkSettings.Networks}}", old[0].id))
	fixture.docker("stop", "--time", "1", old[0].id)
	fixture.verifyFence(fence)
	require.Equal(t, before, fixture.mockStats()["calls"])
	require.NoError(t, rollout.Canary(ctx, actor, pa, 1))
	require.Equal(t, int64(1), fixture.scalar(`SELECT count(*) FROM upstream_principals WHERE id=$1 AND admin_state='PAUSED' AND routing_mode='SHADOW'`, pb))
	newNodes := fixture.startNodes(true)
	for _, node := range newNodes {
		require.Equal(t, http.StatusOK, fixture.requestStatus(node, a.key))
	}
	require.NotEqual(t, http.StatusOK, fixture.requestStatus(newNodes[0], b.key), "an unrelated shadow principal must remain unavailable")
	require.Equal(t, before+3, fixture.mockStats()["calls"])
	require.Eventually(t, func() bool {
		return fixture.scalar(`SELECT count(*) FROM credential_billing_outbox b JOIN request_leases l ON l.id=b.lease_id WHERE l.principal_id=$1 AND b.settled_at IS NOT NULL`, pa) == 3
	}, 10*time.Second, 20*time.Millisecond)
	fixture.assertPreserved(a)
	// Durable terminal receipt, failed local Finish, then real process restart.
	_, err = integrationDB.Exec(fmt.Sprintf(`CREATE FUNCTION full_rollout_block_finish() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.principal_id=%d AND NEW.state='RELEASED' THEN RAISE EXCEPTION 'fixture Finish unavailable'; END IF; RETURN NEW; END $$; CREATE TRIGGER full_rollout_block_finish BEFORE UPDATE ON request_leases FOR EACH ROW EXECUTE FUNCTION full_rollout_block_finish()`, pa))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, fixture.requestStatus(newNodes[0], a.key))
	require.Equal(t, int64(1), fixture.scalar(`SELECT count(*) FROM request_leases l JOIN credential_usage_receipts c ON c.lease_id=l.id WHERE l.principal_id=$1 AND l.state<>'RELEASED'`, pa))
	identityBefore, bindingsBefore := fixture.identityAndBindings(pa)
	_, err = fence.Fence(ctx)
	require.NoError(t, err)
	_, err = integrationDB.Exec(`DROP TRIGGER full_rollout_block_finish ON request_leases; DROP FUNCTION full_rollout_block_finish()`)
	require.NoError(t, err)
	recoveryStarted := time.Now()
	recovered := fixture.startNodes(true)
	require.Eventually(t, func() bool {
		return fixture.scalar(`SELECT occupied FROM upstream_principals WHERE id=$1`, pa) == 0 && fixture.scalar(`SELECT count(*) FROM credential_billing_outbox b JOIN request_leases l ON l.id=b.lease_id WHERE l.principal_id=$1 AND b.settled_at IS NOT NULL`, pa) == 4
	}, 25*time.Second, 50*time.Millisecond)
	require.Equal(t, before+4, fixture.mockStats()["calls"], "receipt recovery cannot replay the upstream")
	identityAfter, bindingsAfter := fixture.identityAndBindings(pa)
	require.JSONEq(t, identityBefore, identityAfter, "restart must preserve the stable installation profile")
	require.JSONEq(t, bindingsBefore, bindingsAfter, "restart must preserve hard session binding and generation")
	t.Logf("full_gateway_receipt_recovery=%s", time.Since(recoveryStarted))
	// Introduce one genuinely unknown response on a second, separately canaried
	// principal. Its missing terminal evidence must survive restart and rollback.
	require.NoError(t, rollout.Canary(ctx, actor, pb, 1))
	recovered = fixture.startNodes(true)
	fixture.mockCommand("/next-unknown")
	_ = fixture.requestStatus(recovered[0], b.key)
	require.Eventually(t, func() bool {
		return fixture.scalar(`SELECT count(*) FROM request_leases WHERE principal_id=$1 AND state='ORPHANED'`, pb) == 1
	}, 10*time.Second, 20*time.Millisecond)
	require.Equal(t, before+5, fixture.mockStats()["calls"])
	err = rollout.Rollback(ctx, actor, pb, 2)
	require.ErrorContains(t, err, "ROLLBACK_PAUSED_UNRESOLVED")
	require.Equal(t, int64(1), fixture.scalar(`SELECT occupied FROM upstream_principals WHERE id=$1 AND admin_state='PAUSED'`, pb))
	unknownView, err := NewCredentialOperations(integrationDB).CredentialRuntime(ctx, pb)
	require.NoError(t, err)
	require.Equal(t, 1, unknownView.Orphaned)
	require.Equal(t, 1, unknownView.UnknownUsage)
	require.False(t, unknownView.CounterMismatch)
	require.GreaterOrEqual(t, fixture.scalar(`SELECT count(*) FROM credential_audit_outbox WHERE principal_id=$1 AND event_type='LEASE_ORPHANED'`, pb), int64(1))
	require.GreaterOrEqual(t, fixture.scalar(`SELECT count(*) FROM credential_audit_outbox WHERE principal_id=$1 AND actor_id=$2 AND event_type='PRINCIPAL_UPDATED'`, pb, actor), int64(1), "manual review retains the administrator's pause audit")
	var unknownSlot string
	require.NoError(t, integrationDB.QueryRow(`SELECT global_user_slot FROM request_leases WHERE principal_id=$1 AND state='ORPHANED'`, pb).Scan(&unknownSlot))
	restartedAt := time.Now()
	fixture.startNodes(true)
	// Observe an actual periodic global-user reconcile after process restart,
	// rather than merely reading the already persisted unknown state.
	require.Eventually(t, func() bool {
		score, err := integrationRedis.ZScore(ctx, fmt.Sprintf("concurrency:user:%d", actor), unknownSlot).Result()
		return err == nil && score >= float64(restartedAt.Unix()+1)
	}, 20*time.Second, 50*time.Millisecond)
	require.Equal(t, int64(1), fixture.scalar(`SELECT occupied FROM upstream_principals WHERE id=$1 AND admin_state='PAUSED'`, pb))
	require.Equal(t, before+5, fixture.mockStats()["calls"])
	_, err = fence.Fence(ctx)
	require.NoError(t, err)
	// Rotate the synthetic credential through the versioned store while fenced.
	// Safe rollback must restore this latest version, never the old snapshot.
	var instance int64
	var generation, installation string
	require.NoError(t, integrationDB.QueryRow(`SELECT i.id,i.identity_generation,p.installation_id FROM credential_instances i JOIN credential_identity_profiles p ON p.instance_id=i.id WHERE i.principal_id=$1`, pa).Scan(&instance, &generation, &installation))
	refresh := &credentialRefreshStore{db: integrationDB}
	op, err := refresh.BeginCredentialRefresh(ctx, instance)
	require.NoError(t, err)
	latest := service.CredentialSecret{AccessToken: "mock:" + uuid.NewString() + ":rotated", RefreshToken: "mock-rotated-refresh", AccountSubject: a.secret.AccountSubject, UserSubject: a.secret.UserSubject}
	ciphertext, err := vault.Seal(op.ID, latest)
	require.NoError(t, err)
	require.NoError(t, refresh.CompleteCredentialRefresh(ctx, op, service.CredentialRefreshResult{Ciphertext: ciphertext, AAD: op.ID, ExpiresAt: time.Now().Add(time.Hour), AccessFingerprint: vault.Fingerprint("token", latest.AccessToken), RefreshFingerprint: vault.Fingerprint("token", latest.RefreshToken)}))
	require.NoError(t, rollout.Rollback(ctx, actor, pa, 2))
	fixture.verifyFence(fence)
	fixture.assertPreserved(a)
	var raw []byte
	var schedulable bool
	require.NoError(t, integrationDB.QueryRow(`SELECT credentials,schedulable FROM accounts WHERE id=$1`, a.id).Scan(&raw, &schedulable))
	var restored map[string]any
	require.NoError(t, json.Unmarshal(raw, &restored))
	require.Equal(t, latest.AccessToken, restored["access_token"])
	require.Equal(t, latest.RefreshToken, restored["refresh_token"])
	require.False(t, schedulable, "rollback cannot automatically authorize old scheduling")
	var afterGeneration, afterInstallation string
	require.NoError(t, integrationDB.QueryRow(`SELECT i.identity_generation,p.installation_id FROM credential_instances i JOIN credential_identity_profiles p ON p.instance_id=i.id WHERE i.id=$1`, instance).Scan(&afterGeneration, &afterInstallation))
	require.Equal(t, generation, afterGeneration)
	require.Equal(t, installation, afterInstallation)
	require.Equal(t, int64(1), fixture.scalar(`SELECT count(*) FROM request_leases WHERE principal_id=$1 AND state='ORPHANED'`, pb))
	require.Equal(t, int64(0), fixture.scalar(`SELECT count(*) FROM credential_usage_receipts c JOIN request_leases l ON l.id=c.lease_id WHERE l.principal_id=$1 AND (c.receipt->>'Complete')::boolean IS TRUE`, pb), "an unknown response cannot manufacture terminal usage evidence")
	require.Equal(t, int64(1), fixture.scalar(`SELECT count(*) FROM credential_usage_receipts c JOIN request_leases l ON l.id=c.lease_id WHERE l.principal_id=$1 AND c.state='REVIEW_REQUIRED' AND c.reason='USAGE_UNKNOWN'`, pb), "partial observed facts remain explicitly unknown for manual review")
	require.Equal(t, int64(7), fixture.scalar(`SELECT count(*) FROM usage_logs WHERE user_id=$1`, actor), "three legacy and four grouped completions are billed once")
	require.Equal(t, before+5, fixture.mockStats()["calls"])
	fixture.scanLogs([]string{a.secret.AccessToken, a.secret.RefreshToken, b.secret.AccessToken, b.secret.RefreshToken, latest.AccessToken, latest.RefreshToken, a.key, b.key})
	t.Logf("full_gateway_rollout PASS: old=3, canary=3, recovered=3, requests=%d, unknown_preserved=1, topology=single-PG/single-Redis/single-Docker-host", before+5)
}

type fullRolloutNode struct{ id, url string }
type fullRolloutAccount struct {
	id, group  int64
	key, extra string
	secret     service.CredentialSecret
}
type fullGatewayRollout struct {
	t                                                          *testing.T
	ctx                                                        context.Context
	project, network, directory, server, mock, mockURL, mockID string
	containers                                                 []string
	nodes                                                      []fullRolloutNode
}

func newFullGatewayRollout(t *testing.T, ctx context.Context, server, mock string) *fullGatewayRollout {
	t.Helper()
	f := &fullGatewayRollout{t: t, ctx: ctx, project: "credential-full-" + uuid.NewString(), directory: t.TempDir(), server: server, mock: mock}
	t.Cleanup(f.stop)
	for _, binary := range []string{server, mock} {
		data, err := os.ReadFile(binary)
		require.NoError(t, err)
		sum := sha256.Sum256(data)
		t.Logf("binary=%s sha256=%s", filepath.Base(binary), hex.EncodeToString(sum[:]))
	}
	f.network = f.docker("network", "create", "--internal", f.project)
	pgURL, err := url.Parse(integrationDSN)
	require.NoError(t, err)
	_, redisPort, err := net.SplitHostPort(integrationRedis.Options().Addr)
	require.NoError(t, err)
	for _, endpoint := range []struct{ port, alias string }{{pgURL.Port(), "database"}, {redisPort, "redis"}} {
		// Match the harness's exact mapped host port. Docker's publish filter
		// semantics differ for host versus container ports across engines.
		var matching []string
		for _, id := range strings.Fields(f.docker("ps", "--quiet")) {
			var ports map[string][]struct{ HostPort string }
			require.NoError(t, json.Unmarshal([]byte(f.docker("inspect", "--format", "{{json .NetworkSettings.Ports}}", id)), &ports))
			for _, bindings := range ports {
				for _, binding := range bindings {
					if binding.HostPort == endpoint.port {
						matching = append(matching, id)
						break
					}
				}
			}
		}
		require.Len(t, matching, 1)
		f.docker("network", "connect", "--alias", endpoint.alias, f.network, matching[0])
	}
	f.certificate()
	for _, mode := range []bool{false, true} {
		dir := filepath.Join(f.directory, fmt.Sprint(mode))
		require.NoError(t, os.Mkdir(dir, 0700))
		cfg := fmt.Sprintf("run_mode: standard\ntimezone: UTC\nserver:\n  port: 8080\n  host: 0.0.0.0\n  mode: release\ndatabase:\n  host: database\n  port: 5432\n  user: postgres\n  password: postgres\n  dbname: sub2api_test\n  sslmode: disable\n  max_open_conns: 8\n  max_idle_conns: 4\nredis:\n  host: redis\n  port: 6379\njwt:\n  secret: %s\ngateway:\n  multi_credential_http_enabled: %v\n  credential_vault_key: %s\npricing:\n  remote_url: http://mock:8081/no-pricing\n  hash_url: http://mock:8081/no-hash\n  fallback_file: /pricing/model_prices_and_context_window.json\n", strings.Repeat("f", 64), mode, strings.Repeat("ab", 32))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfg), 0600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".installed"), []byte("isolated acceptance fixture"), 0600))
	}
	mockID := f.docker("run", "--detach", "--platform", "linux/amd64", "--network", f.network, "--network-alias", "mock", "--volume", mock+":/mock:ro", "--volume", f.directory+":/fixture:ro", "alpine:3.20", "/mock")
	f.containers = append(f.containers, mockID)
	f.mockID = mockID
	f.mockURL = "http://mock:8081"
	require.Eventually(t, func() bool {
		status, _, err := f.httpRequest("GET", f.mockURL+"/stats", "", nil)
		return err == nil && status == 200
	}, 10*time.Second, 50*time.Millisecond)
	return f
}

func (f *fullGatewayRollout) docker(args ...string) string {
	f.t.Helper()
	cmd := exec.CommandContext(f.ctx, "docker", args...)
	var out []byte
	var err error
	if len(args) > 0 && args[0] == "logs" {
		out, err = cmd.CombinedOutput()
	} else {
		out, err = cmd.Output()
	}
	if failure, ok := err.(*exec.ExitError); ok {
		out = append(out, failure.Stderr...)
	}
	require.NoError(f.t, err, "docker %s: %s", strings.Join(args, " "), string(out))
	return strings.TrimSpace(string(out))
}
func (f *fullGatewayRollout) stop() {
	for _, id := range f.containers {
		_ = exec.Command("docker", "stop", "--time", "1", id).Run()
	}
}
func (f *fullGatewayRollout) address(id, port string) string {
	value := f.docker("inspect", "--format", `{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}`, id)
	return "http://" + net.JoinHostPort(value, port)
}
func (f *fullGatewayRollout) startNodes(enabled bool) []fullRolloutNode {
	f.t.Helper()
	var nodes []fullRolloutNode
	pricing, err := filepath.Abs("../../resources/model-pricing")
	require.NoError(f.t, err)
	for range 3 {
		id := f.docker("run", "--detach", "--platform", "linux/amd64", "--restart=always", "--network", f.network, "--label", "com.docker.compose.project="+f.project, "--label", "com.docker.compose.service=gateway", "--env", "DATA_DIR=/fixture", "--env", "SSL_CERT_FILE=/certificate/mock.crt", "--volume", f.server+":/server:ro", "--volume", filepath.Join(f.directory, fmt.Sprint(enabled))+":/fixture", "--volume", f.directory+":/certificate:ro", "--volume", pricing+":/pricing:ro", "alpine:3.20", "/server")
		f.containers = append(f.containers, id)
		node := fullRolloutNode{id: id, url: f.address(id, "8080")}
		f.nodes = append(f.nodes, node)
		nodes = append(nodes, node)
		health := false
		until := time.Now().Add(40 * time.Second)
		for time.Now().Before(until) {
			status, _, e := f.httpRequest("GET", node.url+"/health", "", nil)
			if e == nil {
				if status == 200 {
					health = true
					break
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		if !health {
			f.t.Logf("gateway startup log: %s", f.docker("logs", id))
		}
		require.True(f.t, health, "actual cmd/server must start")
	}
	return nodes
}
func (f *fullGatewayRollout) requestStatus(node fullRolloutNode, key string) int {
	f.t.Helper()
	status, body, err := f.httpRequest("POST", node.url+"/v1/responses", `{"model":"gpt-5.4","stream":false,"input":[{"role":"user","content":"acceptance fixture"}]}`, map[string]string{"Authorization": "Bearer " + key, "Content-Type": "application/json", "session-id": uuid.NewString(), "User-Agent": "codex_cli_rs/0.144.1", "originator": "codex_cli_rs"})
	require.NoError(f.t, err)
	if status != 200 {
		f.t.Logf("gateway response status=%d body=%s", status, body)
	}
	return status
}
func (f *fullGatewayRollout) account(actor int64) fullRolloutAccount {
	f.t.Helper()
	var a fullRolloutAccount
	nonce := uuid.NewString()
	a.key = uuid.NewString()
	a.secret = service.CredentialSecret{AccessToken: "mock:" + nonce + ":old", RefreshToken: "refresh:" + nonce + ":old", AccountSubject: "workspace-" + nonce, UserSubject: "user-a"}
	credentials, _ := json.Marshal(map[string]any{"access_token": a.secret.AccessToken, "refresh_token": a.secret.RefreshToken, "chatgpt_account_id": a.secret.AccountSubject, "chatgpt_user_id": a.secret.UserSubject, "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
	a.extra = fmt.Sprintf(`{"codex_fingerprint_seed":%q,"codex_fingerprint_mode":"device","custom":"preserved"}`, uuid.NewString())
	var proxy int64
	require.NoError(f.t, integrationDB.QueryRow(`INSERT INTO proxies(name,protocol,host,port) VALUES('isolated mock','http','mock',8081) RETURNING id`).Scan(&proxy))
	require.NoError(f.t, integrationDB.QueryRow(`INSERT INTO accounts(name,platform,type,credentials,extra,status,schedulable,concurrency,rate_multiplier,proxy_id) VALUES('isolated legacy','openai','oauth',$1,$2,'active',true,5,1.25,$3) RETURNING id`, string(credentials), a.extra, proxy).Scan(&a.id))
	require.NoError(f.t, integrationDB.QueryRow(`INSERT INTO groups(name,platform) VALUES($1,'openai') RETURNING id`, nonce).Scan(&a.group))
	_, err := integrationDB.Exec(`INSERT INTO account_groups(account_id,group_id) VALUES($1,$2)`, a.id, a.group)
	require.NoError(f.t, err)
	_, err = integrationDB.Exec(`INSERT INTO api_keys(user_id,key,name,group_id) VALUES($1,$2,'isolated gateway',$3)`, actor, a.key, a.group)
	require.NoError(f.t, err)
	return a
}
func (f *fullGatewayRollout) scalar(query string, args ...any) int64 {
	f.t.Helper()
	var n int64
	require.NoError(f.t, integrationDB.QueryRow(query, args...).Scan(&n))
	return n
}
func (f *fullGatewayRollout) mockStats() map[string]int64 {
	f.t.Helper()
	status, body, err := f.httpRequest("GET", f.mockURL+"/stats", "", nil)
	require.NoError(f.t, err)
	require.Equal(f.t, 200, status)
	var out map[string]int64
	require.NoError(f.t, json.Unmarshal([]byte(body), &out))
	return out
}
func (f *fullGatewayRollout) mockCommand(path string) {
	f.t.Helper()
	status, _, err := f.httpRequest("POST", f.mockURL+path, "", nil)
	require.NoError(f.t, err)
	require.Equal(f.t, 204, status)
}

func (f *fullGatewayRollout) httpRequest(method, address, body string, headers map[string]string) (int, string, error) {
	data, err := json.Marshal(struct {
		URL, Method, Body string
		Headers           map[string]string
	}{address, method, body, headers})
	if err != nil {
		return 0, "", err
	}
	cmd := exec.CommandContext(f.ctx, "docker", "exec", "-i", f.mockID, "/mock", "client")
	cmd.Stdin = strings.NewReader(string(data))
	out, err := cmd.Output()
	if err != nil {
		return 0, "", err
	}
	var response struct {
		Status      int
		Body, Error string
	}
	if err = json.Unmarshal(out, &response); err != nil {
		return 0, "", err
	}
	if response.Error != "" {
		return response.Status, response.Body, fmt.Errorf("fixture HTTP: %s", response.Error)
	}
	return response.Status, response.Body, nil
}
func (f *fullGatewayRollout) verifyFence(fence credentialfence.Docker) {
	f.t.Helper()
	e := credentialfence.Evidence{Project: f.project, Service: "gateway"}
	for _, n := range f.nodes {
		e.IDs = append(e.IDs, n.id)
	}
	sort.Strings(e.IDs)
	require.NoError(f.t, fence.Verify(f.ctx, e))
}
func (f *fullGatewayRollout) assertPreserved(a fullRolloutAccount) {
	f.t.Helper()
	var extra string
	var multiplier float64
	require.NoError(f.t, integrationDB.QueryRow(`SELECT extra::text,rate_multiplier FROM accounts WHERE id=$1`, a.id).Scan(&extra, &multiplier))
	require.JSONEq(f.t, a.extra, extra)
	require.Equal(f.t, 1.25, multiplier)
	require.Equal(f.t, int64(1), f.scalar(`SELECT count(*) FROM account_groups WHERE account_id=$1 AND group_id=$2`, a.id, a.group))
}

func (f *fullGatewayRollout) identityAndBindings(principal int64) (string, string) {
	f.t.Helper()
	var identity, bindings string
	require.NoError(f.t, integrationDB.QueryRow(`SELECT COALESCE(jsonb_agg(to_jsonb(p) ORDER BY p.instance_id),'[]'::jsonb)::text FROM credential_identity_profiles p WHERE p.principal_id=$1`, principal).Scan(&identity))
	require.NoError(f.t, integrationDB.QueryRow(`SELECT COALESCE(jsonb_agg(to_jsonb(b) ORDER BY b.id),'[]'::jsonb)::text FROM session_bindings b WHERE b.principal_id=$1`, principal).Scan(&bindings))
	require.NotEqual(f.t, "[]", identity)
	require.NotEqual(f.t, "[]", bindings)
	return identity, bindings
}
func (f *fullGatewayRollout) certificate() {
	f.t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(f.t, err)
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "isolated credential mock"}, DNSNames: []string{"chatgpt.com"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true, IsCA: true}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(f.t, err)
	private, err := x509.MarshalECPrivateKey(key)
	require.NoError(f.t, err)
	require.NoError(f.t, os.WriteFile(filepath.Join(f.directory, "mock.crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600))
	require.NoError(f.t, os.WriteFile(filepath.Join(f.directory, "mock.key"), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: private}), 0600))
}
func (f *fullGatewayRollout) scanLogs(secrets []string) {
	f.t.Helper()
	artifacts := os.Getenv("SUB2API_ACCEPTANCE_ARTIFACT_DIR")
	if artifacts != "" {
		require.NoError(f.t, os.MkdirAll(artifacts, 0700))
	}
	for index, node := range f.nodes {
		raw := f.docker("logs", node.id)
		for _, secret := range secrets {
			require.NotContains(f.t, raw, secret, "server logs must not contain fixture token or API key")
		}
		require.NotContains(f.t, raw, "panic:")
		sum := sha256.Sum256([]byte(raw))
		f.t.Logf("gateway_log container=%s bytes=%d sha256=%x secret_scan=PASS", node.id[:12], len(raw), sum)
		if artifacts != "" {
			require.NoError(f.t, os.WriteFile(filepath.Join(artifacts, fmt.Sprintf("gateway-%02d.log", index+1)), []byte(raw), 0600))
		}
	}
}
