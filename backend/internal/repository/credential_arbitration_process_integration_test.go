//go:build integration

package repository

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const credentialArbitrationWorkerDSN = "SUB2API_CREDENTIAL_ARBITRATION_PROCESS_DSN"

type credentialArbitrationProcessInput struct {
	Fingerprints []string
	RequestHash  string
}
type credentialArbitrationProcessOutput struct {
	Status    string `json:"status"`
	Operation string `json:"operation,omitempty"`
}

// Child processes use the normal configured repository and independent SQL
// pools. Only synthetic keyed fingerprints cross stdin; no provider token or
// network request is involved. Returning deliberately exits an acknowledged
// SENDING owner without calling Finish, to test persistent claim ownership.
func runCredentialArbitrationProcessWorker() bool {
	dsn := os.Getenv(credentialArbitrationWorkerDSN)
	if dsn == "" {
		return false
	}
	var input credentialArbitrationProcessInput
	if json.NewDecoder(os.Stdin).Decode(&input) != nil {
		os.Exit(2)
	}
	output := credentialArbitrationProcessOutput{Status: "fault"}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		_ = json.NewEncoder(os.Stdout).Encode(output)
		return true
	}
	defer db.Close()
	db.SetMaxOpenConns(2)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	defer client.Close()
	cfg := &config.Config{}
	cfg.Gateway.CredentialVaultKey = strings.Repeat("ab", 32)
	repo, err := configuredCredentialAccountRepository(client, db, nil, cfg)
	if err == nil {
		var op service.CredentialLegacyRefreshOperation
		op, err = repo.BeginLegacyCredentialRefresh(ctx, input.Fingerprints, input.RequestHash)
		if err == nil && op.State == "SENDING" {
			output.Status = "owned"
			output.Operation = op.ID
		}
		if errors.Is(err, service.ErrCredentialLegacyBypass) {
			output.Status = "blocked"
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(output)
	return true
}

func TestCredentialOperationArbitrationThreeProcessesOwnerExit(t *testing.T) {
	f := newOperationArbitrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	secret := arbitrationSecret()
	input := credentialArbitrationProcessInput{Fingerprints: f.fingerprints(secret), RequestHash: "process-exit-operation"}
	payload, err := json.Marshal(input)
	require.NoError(t, err)
	executable, err := os.Executable()
	require.NoError(t, err)
	application := "credential-arbitration-" + uuid.NewString()
	dsn, err := url.Parse(f.dsn)
	require.NoError(t, err)
	parameters := dsn.Query()
	parameters.Set("application_name", application)
	dsn.RawQuery = parameters.Encode()
	type child struct {
		cmd            *exec.Cmd
		stdout, stderr bytes.Buffer
	}
	start := func() *child {
		c := &child{cmd: exec.CommandContext(ctx, executable, "-test.run=^$")}
		c.cmd.Env = append(os.Environ(), credentialArbitrationWorkerDSN+"="+dsn.String())
		c.cmd.Stdin = bytes.NewReader(payload)
		c.cmd.Stdout = &c.stdout
		c.cmd.Stderr = &c.stderr
		require.NoError(t, c.cmd.Start())
		t.Cleanup(func() { _ = c.cmd.Process.Kill() })
		return c
	}
	finish := func(c *child) credentialArbitrationProcessOutput {
		require.NoError(t, c.cmd.Wait(), "arbitration fixture child exited unexpectedly")
		var out credentialArbitrationProcessOutput
		require.NoError(t, json.Unmarshal(c.stdout.Bytes(), &out))
		require.NotEqual(t, "fault", out.Status, "child must distinguish arbitration rejection from fixture errors")
		require.NotContains(t, c.stderr.String(), secret.AccessToken)
		require.NotContains(t, c.stderr.String(), secret.RefreshToken)
		return out
	}
	barrier, err := f.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer barrier.Rollback()
	_, err = lockCredentialArbitration(ctx, barrier)
	require.NoError(t, err)
	first, second := start(), start()
	require.Eventually(t, func() bool {
		var count int
		err := f.db.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND application_name=$1 AND wait_event_type='Lock' AND query LIKE '%credential_arbitration_state%'`, application).Scan(&count)
		return err == nil && count == 2
	}, 5*time.Second, 10*time.Millisecond, "both independent processes must reach the real PostgreSQL ownership barrier")
	require.NoError(t, barrier.Commit())
	results := []credentialArbitrationProcessOutput{finish(first), finish(second)}
	owned, blocked := 0, 0
	var operation string
	for _, result := range results {
		switch result.Status {
		case "owned":
			owned++
			operation = result.Operation
		case "blocked":
			blocked++
		}
	}
	require.Equal(t, 1, owned)
	require.Equal(t, 1, blocked)
	require.NotEmpty(t, operation)
	// The acknowledged owner has exited; a genuinely new OS process with a new
	// SQL pool still cannot acquire or replay the pending operation.
	third := finish(start())
	require.Equal(t, "blocked", third.Status)
	_, err = f.imports.Import(ctx, f.actor, "cannot-take-over-exited-owner", secret)
	require.ErrorIs(t, err, service.ErrCredentialLegacyBypass)
	var state string
	var operations, aliases int
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT state FROM credential_legacy_refresh_operations WHERE id=$1`, operation).Scan(&state))
	require.Equal(t, "SENDING", state)
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM credential_legacy_refresh_operations`).Scan(&operations))
	require.Equal(t, 1, operations)
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM credential_legacy_refresh_aliases WHERE operation_id=$1`, operation).Scan(&aliases))
	require.Equal(t, 2, aliases)
	t.Log("three independent OS processes: one acknowledged owner exited, two blocked; SENDING claim and aliases retained; no upstream HTTP was invoked by this repository test")
}
