//go:build integration

package repository

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/credentialfence"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCredentialFenceActualLegacyBinary(t *testing.T) {
	binary := os.Getenv("SUB2API_ACCEPTANCE_LEGACY_BINARY")
	if binary == "" {
		t.Skip("build fde7e8ec4 linux/amd64 server and set SUB2API_ACCEPTANCE_LEGACY_BINARY")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	run := func(args ...string) string {
		t.Helper()
		out, err := exec.CommandContext(ctx, "docker", args...).Output()
		require.NoError(t, err)
		return strings.TrimSpace(string(out))
	}
	project := "credential-legacy-" + uuid.NewString()
	network := run("network", "create", project)
	dsn, err := url.Parse(integrationDSN)
	require.NoError(t, err)
	redisPort := strings.Split(integrationRedis.Options().Addr, ":")[1]
	var calls atomic.Int64
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); acceptanceTerminal(w) }))
	defer mock.Close()
	mockURL, err := url.Parse(mock.URL)
	require.NoError(t, err)
	mockURL.Host = "host.docker.internal:" + mockURL.Port()
	var user, group, account int64
	key := uuid.NewString()
	require.NoError(t, integrationDB.QueryRow(`INSERT INTO users(email,password_hash,balance,concurrency) VALUES($1,'fixture',100,2) RETURNING id`, key+"@example.test").Scan(&user))
	require.NoError(t, integrationDB.QueryRow(`INSERT INTO groups(name,platform) VALUES($1,'openai') RETURNING id`, key).Scan(&group))
	creds := fmt.Sprintf(`{"api_key":"mock-legacy-token","base_url":%q}`, mockURL.String())
	require.NoError(t, integrationDB.QueryRow(`INSERT INTO accounts(name,platform,type,status,schedulable,concurrency,credentials) VALUES('legacy-node-fixture','openai','apikey','active',true,2,$1) RETURNING id`, creds).Scan(&account))
	_, err = integrationDB.Exec(`INSERT INTO account_groups(account_id,group_id) VALUES($1,$2)`, account, group)
	require.NoError(t, err)
	_, err = integrationDB.Exec(`INSERT INTO api_keys(user_id,group_id,key,name) VALUES($1,$2,$3,'fixture')`, user, group, key)
	require.NoError(t, err)
	args := []string{"run", "--detach", "--network", network, "--label", "com.docker.compose.project=" + project, "--label", "com.docker.compose.service=gateway", "--publish", "127.0.0.1::8080", "--workdir", "/app", "--volume", filepath.Dir(binary) + ":/fixtures:ro"}
	for _, env := range []string{"SKIP_SETUP=true", "TIMEZONE=UTC", "TZ=UTC", "SERVER_HOST=0.0.0.0", "SERVER_PORT=8080", "SERVER_MODE=release", "JWT_SECRET=fixture-only-not-production-0123456789", "TOTP_ENCRYPTION_KEY=" + strings.Repeat("ab", 32), "DATABASE_HOST=host.docker.internal", "DATABASE_PORT=" + dsn.Port(), "DATABASE_USER=postgres", "DATABASE_PASSWORD=postgres", "DATABASE_DBNAME=sub2api_test", "DATABASE_SSLMODE=disable", "REDIS_HOST=host.docker.internal", "REDIS_PORT=" + redisPort, "RUN_MODE=standard", "PRICING_REMOTE_URL=http://127.0.0.1:1/pricing", "PRICING_HASH_URL=http://127.0.0.1:1/hash"} {
		args = append(args, "--env", env)
	}
	resource, _ := filepath.Abs("../../resources")
	args = append(args, "--volume", resource+":/app/resources:ro", "alpine:3.20", "/fixtures/"+filepath.Base(binary))
	id := run(args...)
	t.Cleanup(func() { _ = exec.Command("docker", "stop", "--time", "1", id).Run() })
	var ready bool
	var endpoint string
	for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline); {
		portBytes, portErr := exec.CommandContext(ctx, "docker", "port", id, "8080/tcp").Output()
		if portErr != nil {
			break
		}
		portText := strings.TrimSpace(string(portBytes))
		parts := strings.Split(portText, ":")
		if len(parts) > 1 {
			if _, err = strconv.Atoi(parts[len(parts)-1]); err == nil {
				endpoint = "http://127.0.0.1:" + parts[len(parts)-1]
				resp, e := http.Get(endpoint + "/health")
				if e == nil {
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
					if resp.StatusCode == 200 {
						ready = true
						break
					}
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !ready {
		logs, _ := exec.Command("docker", "logs", "--tail", "25", id).CombinedOutput()
		t.Fatalf("legacy fixture did not start: %s", logs)
	}
	code, body, err := acceptanceRequest(ctx, endpoint+"/v1/responses", key, "legacy-real")
	require.NoError(t, err)
	require.Equal(t, 200, code, body)
	require.Greater(t, calls.Load(), int64(0))
	before := calls.Load()
	fence := credentialfence.Docker{Project: project, Service: "gateway"}
	evidence, err := fence.Fence(ctx)
	require.NoError(t, err)
	require.NoError(t, fence.Verify(ctx, evidence))
	// A stale fde7 process can be started again, but its network is not restored.
	run("start", id)
	require.Equal(t, "0", run("inspect", "--format", "{{len .NetworkSettings.Networks}}", id))
	require.Error(t, fence.Verify(ctx, evidence))
	require.Equal(t, before, calls.Load())
	run("stop", "--time", "1", id)
	require.NoError(t, fence.Verify(ctx, evidence))
}
