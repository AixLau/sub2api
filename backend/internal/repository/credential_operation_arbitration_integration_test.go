//go:build integration

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

type operationArbitrationFixture struct {
	db      *sql.DB
	dsn     string
	client  *dbent.Client
	repo    *accountRepository
	vault   *service.CredentialVault
	imports *service.CredentialImportService
	actor   int64
}

func newOperationArbitrationFixture(t *testing.T) operationArbitrationFixture {
	t.Helper()
	db, dsn := vaultRotationTestDB(t)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	cfg := &config.Config{}
	cfg.Gateway.CredentialVaultKey = strings.Repeat("ab", 32)
	repo, err := configuredCredentialAccountRepository(client, db, nil, cfg)
	require.NoError(t, err)
	vault, err := service.NewCredentialVault(cfg.Gateway.CredentialVaultKey)
	require.NoError(t, err)
	f := operationArbitrationFixture{db: db, dsn: dsn, client: client, repo: repo, vault: vault}
	require.NoError(t, db.QueryRow(`INSERT INTO users(email,password_hash,role) VALUES($1,'fixture','admin') RETURNING id`, uuid.NewString()+"@example.test").Scan(&f.actor))
	f.imports = service.NewCredentialImportService(NewCredentialImportRepository(db), vault, mockCredentialVerifier{})
	return f
}

func arbitrationSecret() service.CredentialSecret {
	nonce := uuid.NewString()
	return service.CredentialSecret{AccessToken: "mock:" + nonce + ":access", RefreshToken: "refresh:" + nonce + ":refresh"}
}
func arbitrationAccount(secret service.CredentialSecret) *service.Account {
	return &service.Account{Name: "arbitration fixture", Platform: "openai", Type: "oauth", Status: "active", Schedulable: true, Concurrency: 3, Credentials: map[string]any{"access_token": secret.AccessToken, "refresh_token": secret.RefreshToken}, Extra: map[string]any{"codex_fingerprint_seed": uuid.NewString(), "codex_fingerprint_mode": "device"}}
}
func (f operationArbitrationFixture) createPrincipal(ctx context.Context, imported service.CredentialImportView) (int64, error) {
	return NewCredentialPrincipalCreator(f.db).CreateCredentialPrincipal(ctx, f.actor, uuid.NewString(), service.CreateCredentialPrincipalInput{Name: "fixture", TotalConcurrency: 3, Instances: []service.CreateCredentialInstanceInput{{ImportID: imported.ID, Name: "fixture", Weight: 1}}})
}
func (f operationArbitrationFixture) fingerprints(secret service.CredentialSecret) []string {
	return []string{f.vault.Fingerprint("token", secret.AccessToken), f.vault.Fingerprint("token", secret.RefreshToken)}
}
func (f operationArbitrationFixture) waitOwnershipLock(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool {
		var n int
		err := f.db.QueryRow(`SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query LIKE '%credential_arbitration_state%'`).Scan(&n)
		return err == nil && n > 0
	}, 3*time.Second, 5*time.Millisecond, "competing real transaction must be waiting for the ownership row")
}

func TestCredentialOperationArbitrationLegacyCommitBeforeImport(t *testing.T) {
	f := newOperationArbitrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	secret := arbitrationSecret()
	tx, err := f.client.Tx(ctx)
	require.NoError(t, err)
	defer tx.Rollback()
	account := arbitrationAccount(secret)
	require.NoError(t, f.repo.Create(dbent.NewTxContext(ctx, tx), account))
	type importedResult struct {
		view service.CredentialImportView
		err  error
	}
	done := make(chan importedResult, 1)
	go func() {
		v, e := f.imports.Import(ctx, f.actor, "wait-for-legacy", secret)
		done <- importedResult{v, e}
	}()
	f.waitOwnershipLock(t)
	require.NoError(t, tx.Commit())
	result := <-done
	require.NoError(t, result.err, "existing legacy credentials may be staged for fenced migration")
	_, err = f.createPrincipal(ctx, result.view)
	require.ErrorIs(t, err, service.ErrCredentialLegacyBypass, "staging cannot authorize a second controlled carrier")
	var carriers, claims int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM credential_instances`).Scan(&carriers))
	require.Zero(t, carriers)
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM credential_legacy_account_claims WHERE account_id=$1 AND NOT transferred`, account.ID).Scan(&claims))
	require.Equal(t, 2, claims)
}

func TestCredentialOperationArbitrationWaitingLegacyRechecksAtWrite(t *testing.T) {
	f := newOperationArbitrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	secret := arbitrationSecret()
	_, err := f.imports.Import(ctx, f.actor, "import-first", secret)
	require.NoError(t, err)
	tx, err := f.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = lockCredentialArbitration(ctx, tx)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- f.repo.Create(ctx, arbitrationAccount(secret)) }()
	f.waitOwnershipLock(t)
	require.NoError(t, tx.Commit())
	require.ErrorIs(t, <-done, service.ErrCredentialLegacyBypass)
	var count int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM accounts`).Scan(&count))
	require.Zero(t, count)
}

func TestCredentialOperationArbitrationConcurrentFirstUse(t *testing.T) {
	f := newOperationArbitrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for range 4 {
		secret := arbitrationSecret()
		start := make(chan struct{})
		legacy := make(chan error, 1)
		type importResult struct {
			view service.CredentialImportView
			err  error
		}
		controlled := make(chan importResult, 1)
		go func() { <-start; legacy <- f.repo.Create(ctx, arbitrationAccount(secret)) }()
		go func() {
			<-start
			v, e := f.imports.Import(ctx, f.actor, uuid.NewString(), secret)
			controlled <- importResult{v, e}
		}()
		close(start)
		legacyErr := <-legacy
		imported := <-controlled
		require.NoError(t, imported.err)
		_, controlErr := f.createPrincipal(ctx, imported.view)
		if legacyErr == nil {
			require.ErrorIs(t, controlErr, service.ErrCredentialLegacyBypass)
		} else {
			require.ErrorIs(t, legacyErr, service.ErrCredentialLegacyBypass)
			require.NoError(t, controlErr)
		}
	}
}

func TestCredentialOperationArbitrationReverseInputOrder(t *testing.T) {
	f := newOperationArbitrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	fp := f.fingerprints(arbitrationSecret())
	start := make(chan struct{})
	type result struct {
		op  service.CredentialLegacyRefreshOperation
		err error
	}
	done := make(chan result, 2)
	for _, input := range [][]string{fp, {fp[1], fp[0], fp[1]}} {
		go func(v []string) {
			<-start
			o, e := f.repo.BeginLegacyCredentialRefresh(ctx, v, "same-operation")
			done <- result{o, e}
		}(input)
	}
	close(start)
	successful := 0
	for range 2 {
		r := <-done
		if r.err == nil {
			successful++
			require.Equal(t, "SENDING", r.op.State)
		} else {
			require.ErrorIs(t, r.err, service.ErrCredentialLegacyBypass)
		}
	}
	require.Equal(t, 1, successful)
	var ops, aliases int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM credential_legacy_refresh_operations`).Scan(&ops))
	require.Equal(t, 1, ops)
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM credential_legacy_refresh_aliases`).Scan(&aliases))
	require.Equal(t, 2, aliases)
}

func TestCredentialOperationArbitrationRepositoryWritePaths(t *testing.T) {
	f := newOperationArbitrationFixture(t)
	ctx := context.Background()
	controlled := arbitrationSecret()
	_, err := f.imports.Import(ctx, f.actor, "controlled", controlled)
	require.NoError(t, err)
	for _, path := range []string{"create", "create_with_groups", "update", "update_credentials", "bulk"} {
		t.Run(path, func(t *testing.T) {
			original := arbitrationSecret()
			account := arbitrationAccount(original)
			require.NoError(t, f.repo.Create(ctx, account))
			credentials := arbitrationAccount(controlled).Credentials
			var writeErr error
			switch path {
			case "create":
				writeErr = f.repo.Create(ctx, arbitrationAccount(controlled))
			case "create_with_groups":
				writeErr = f.repo.CreateWithAccountGroups(ctx, arbitrationAccount(controlled), nil)
			case "update":
				account.Credentials = credentials
				writeErr = f.repo.Update(ctx, account)
			case "update_credentials":
				writeErr = f.repo.UpdateCredentials(ctx, account.ID, credentials)
			case "bulk":
				_, writeErr = f.repo.BulkUpdate(ctx, []int64{account.ID}, service.AccountBulkUpdate{Credentials: credentials})
			}
			require.ErrorIs(t, writeErr, service.ErrCredentialLegacyBypass)
			var persisted string
			require.NoError(t, f.db.QueryRow(`SELECT credentials->>'access_token' FROM accounts WHERE id=$1`, account.ID).Scan(&persisted))
			require.Equal(t, original.AccessToken, persisted)
		})
	}
}

func TestCredentialOperationArbitrationPendingSurvivesNewConnection(t *testing.T) {
	f := newOperationArbitrationFixture(t)
	ctx := context.Background()
	secret := arbitrationSecret()
	fp := f.fingerprints(secret)
	op, err := f.repo.BeginLegacyCredentialRefresh(ctx, fp, "pending")
	require.NoError(t, err)
	secondDB, err := sql.Open("postgres", f.dsn)
	require.NoError(t, err)
	defer secondDB.Close()
	secondClient := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, secondDB)))
	defer secondClient.Close()
	cfg := &config.Config{}
	cfg.Gateway.CredentialVaultKey = strings.Repeat("ab", 32)
	second, err := configuredCredentialAccountRepository(secondClient, secondDB, nil, cfg)
	require.NoError(t, err)
	_, err = second.BeginLegacyCredentialRefresh(ctx, fp, "pending")
	require.ErrorIs(t, err, service.ErrCredentialLegacyBypass)
	_, err = f.imports.Import(ctx, f.actor, "pending-import", secret)
	require.ErrorIs(t, err, service.ErrCredentialLegacyBypass)
	require.ErrorIs(t, second.Create(ctx, arbitrationAccount(secret)), service.ErrCredentialLegacyBypass)
	require.NoError(t, f.repo.FinishLegacyCredentialRefresh(ctx, op, nil, nil, false))
	_, err = second.BeginLegacyCredentialRefresh(ctx, fp, "pending")
	require.ErrorIs(t, err, service.ErrCredentialLegacyBypass)
	var state string
	require.NoError(t, f.db.QueryRow(`SELECT state FROM credential_legacy_refresh_operations WHERE id=$1`, op.ID).Scan(&state))
	require.Equal(t, "UNKNOWN", state)
}

func TestCredentialOperationArbitrationBeginCommitACKLost(t *testing.T) {
	f := newOperationArbitrationFixture(t)
	ctx := context.Background()
	secret := arbitrationSecret()
	connector, err := pq.NewConnector(f.dsn)
	require.NoError(t, err)
	armed := &atomic.Bool{}
	faultDB := sql.OpenDB(vaultCommitLostConnector{Connector: connector, armed: armed})
	defer faultDB.Close()
	faultRepo := *f.repo
	faultRepo.sql = faultDB
	armed.Store(true)
	op, err := faultRepo.BeginLegacyCredentialRefresh(ctx, f.fingerprints(secret), "lost-begin")
	require.ErrorIs(t, err, service.ErrCredentialVaultUnavailable)
	require.Empty(t, op.ID)
	var state string
	require.NoError(t, f.db.QueryRow(`SELECT state FROM credential_legacy_refresh_operations WHERE request_hash='lost-begin'`).Scan(&state))
	require.Equal(t, "SENDING", state)
	_, err = f.repo.BeginLegacyCredentialRefresh(ctx, f.fingerprints(secret), "lost-begin")
	require.ErrorIs(t, err, service.ErrCredentialLegacyBypass)
	_, err = f.imports.Import(ctx, f.actor, "unknown-begin", secret)
	require.ErrorIs(t, err, service.ErrCredentialLegacyBypass)
}

func TestCredentialOperationArbitrationFinishCommitACKLostRecoversResult(t *testing.T) {
	f := newOperationArbitrationFixture(t)
	ctx := context.Background()
	input, output := arbitrationSecret(), arbitrationSecret()
	op, err := f.repo.BeginLegacyCredentialRefresh(ctx, f.fingerprints(input), "lost-finish")
	require.NoError(t, err)
	cipher, err := f.vault.SealData(op.ID, []byte(`{"fixture":"durable-result"}`))
	require.NoError(t, err)
	connector, err := pq.NewConnector(f.dsn)
	require.NoError(t, err)
	armed := &atomic.Bool{}
	faultDB := sql.OpenDB(vaultCommitLostConnector{Connector: connector, armed: armed})
	defer faultDB.Close()
	faultRepo := *f.repo
	faultRepo.sql = faultDB
	armed.Store(true)
	require.ErrorIs(t, faultRepo.FinishLegacyCredentialRefresh(ctx, op, f.fingerprints(output), cipher, true), service.ErrCredentialVaultUnavailable)
	require.NoError(t, f.repo.FinishLegacyCredentialRefresh(ctx, op, nil, nil, false), "ACK-loss compensation cannot revert durable success")
	again, err := f.repo.BeginLegacyCredentialRefresh(ctx, f.fingerprints(input), "lost-finish")
	require.NoError(t, err)
	require.Equal(t, op.ID, again.ID)
	require.Equal(t, "SUCCEEDED", again.State)
	require.Equal(t, cipher, again.Ciphertext)
	_, err = f.repo.BeginLegacyCredentialRefresh(ctx, f.fingerprints(input), "changed-request")
	require.ErrorIs(t, err, service.ErrCredentialLegacyBypass)
	forged := op
	forged.OwnerNonce = uuid.NewString()
	require.ErrorIs(t, f.repo.FinishLegacyCredentialRefresh(ctx, forged, nil, nil, false), service.ErrAdmissionOwnership)
}

func TestCredentialOperationArbitrationMigrationAndLatestTokenRollback(t *testing.T) {
	f := newOperationArbitrationFixture(t)
	ctx := context.Background()
	original := arbitrationSecret()
	account := arbitrationAccount(original)
	require.NoError(t, f.repo.Create(ctx, account))
	imported, err := f.imports.Import(ctx, f.actor, "migration", original)
	require.NoError(t, err)
	_, err = f.createPrincipal(ctx, imported)
	require.ErrorIs(t, err, service.ErrCredentialLegacyBypass)
	rollout := NewCredentialRollout(f.db, f.vault, &vaultRotationFence{})
	principal, err := rollout.Migrate(ctx, CredentialMigrationInput{AccountID: account.ID, ActorID: f.actor, ImportID: imported.ID, OperationID: uuid.NewString(), RequestedLimit: 3, DrainEvidence: "isolated mock fixture never sent requests; explicit offline no-execution evidence"})
	require.NoError(t, err)
	var instance int64
	var generation, installation string
	require.NoError(t, f.db.QueryRow(`SELECT i.id,i.identity_generation,p.installation_id FROM credential_instances i JOIN credential_identity_profiles p ON p.instance_id=i.id WHERE i.principal_id=$1`, principal).Scan(&instance, &generation, &installation))
	refresh := &credentialRefreshStore{db: f.db}
	op, err := refresh.BeginCredentialRefresh(ctx, instance)
	require.NoError(t, err)
	latest := arbitrationSecret()
	cipher, err := f.vault.Seal(op.ID, latest)
	require.NoError(t, err)
	require.NoError(t, refresh.CompleteCredentialRefresh(ctx, op, service.CredentialRefreshResult{AAD: op.ID, Ciphertext: cipher, ExpiresAt: time.Now().Add(time.Hour), AccessFingerprint: f.fingerprints(latest)[0], RefreshFingerprint: f.fingerprints(latest)[1]}))
	require.NoError(t, rollout.Rollback(ctx, f.actor, principal, 1))
	var raw []byte
	var schedulable bool
	require.NoError(t, f.db.QueryRow(`SELECT credentials,schedulable FROM accounts WHERE id=$1`, account.ID).Scan(&raw, &schedulable))
	var restored map[string]any
	require.NoError(t, json.Unmarshal(raw, &restored))
	require.Equal(t, latest.AccessToken, restored["access_token"])
	require.Equal(t, latest.RefreshToken, restored["refresh_token"])
	require.False(t, schedulable)
	var afterGeneration, afterInstallation string
	require.NoError(t, f.db.QueryRow(`SELECT i.identity_generation,p.installation_id FROM credential_instances i JOIN credential_identity_profiles p ON p.instance_id=i.id WHERE i.id=$1`, instance).Scan(&afterGeneration, &afterInstallation))
	require.Equal(t, generation, afterGeneration)
	require.Equal(t, installation, afterInstallation)
	current, err := f.repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.NoError(t, f.repo.Update(ctx, current), "legitimate own returned carrier remains editable")
	_, err = f.repo.BeginLegacyCredentialRefresh(ctx, f.fingerprints(latest), "after-rollback")
	require.NoError(t, err, "legitimate returned latest token may be refreshed through a journaled operation")
}

func TestCredentialOperationArbitrationRollbackPreservesForeignUnknownClaim(t *testing.T) {
	f := newOperationArbitrationFixture(t)
	ctx := context.Background()
	original := arbitrationSecret()
	account := arbitrationAccount(original)
	require.NoError(t, f.repo.Create(ctx, account))
	imported, err := f.imports.Import(ctx, f.actor, "migrate-owned", original)
	require.NoError(t, err)
	rollout := NewCredentialRollout(f.db, f.vault, &vaultRotationFence{})
	principal, err := rollout.Migrate(ctx, CredentialMigrationInput{AccountID: account.ID, ActorID: f.actor, ImportID: imported.ID, OperationID: uuid.NewString(), RequestedLimit: 3, DrainEvidence: "isolated mock fixture no upstream execution; explicit offline test evidence"})
	require.NoError(t, err)
	foreign, err := f.imports.Import(ctx, f.actor, "foreign-carrier", arbitrationSecret())
	require.NoError(t, err)
	foreignPrincipal, err := f.createPrincipal(ctx, foreign)
	require.NoError(t, err)
	var foreignInstance int64
	require.NoError(t, f.db.QueryRow(`SELECT id FROM credential_instances WHERE principal_id=$1`, foreignPrincipal).Scan(&foreignInstance))
	refresh := &credentialRefreshStore{db: f.db}
	op, err := refresh.BeginCredentialRefresh(ctx, foreignInstance)
	require.NoError(t, err)
	cipher, err := f.vault.Seal(op.ID, original)
	require.NoError(t, err)
	fp := f.fingerprints(original)
	require.NoError(t, refresh.MarkCredentialRefreshUnknown(ctx, op, &service.CredentialRefreshResult{Ciphertext: cipher, AAD: op.ID, ExpiresAt: time.Now().Add(time.Hour), AccessFingerprint: fp[0], RefreshFingerprint: fp[1]}))
	var claimsBefore string
	require.NoError(t, f.db.QueryRow(`SELECT jsonb_agg(jsonb_build_array(fingerprint,kind) ORDER BY fingerprint,kind)::text FROM credential_refresh_alias_claims WHERE operation_id=$1`, op.ID).Scan(&claimsBefore))
	require.ErrorIs(t, rollout.Rollback(ctx, f.actor, principal, 1), service.ErrCredentialLegacyBypass)
	var state, aad string
	var retainedCipher []byte
	require.NoError(t, f.db.QueryRow(`SELECT state,result_aad,result_ciphertext FROM credential_refresh_ops WHERE id=$1`, op.ID).Scan(&state, &aad, &retainedCipher))
	require.Equal(t, "REFRESH_RESULT_UNKNOWN", state)
	retained, err := f.vault.Open(aad, retainedCipher)
	require.NoError(t, err)
	require.Equal(t, original, retained)
	var claimsAfter string
	require.NoError(t, f.db.QueryRow(`SELECT jsonb_agg(jsonb_build_array(fingerprint,kind) ORDER BY fingerprint,kind)::text FROM credential_refresh_alias_claims WHERE operation_id=$1`, op.ID).Scan(&claimsAfter))
	require.JSONEq(t, claimsBefore, claimsAfter, "rejected rollback must preserve the exact input and returned-token alias claim set")
	var paused string
	require.NoError(t, f.db.QueryRow(`SELECT admin_state FROM upstream_principals WHERE id=$1`, principal).Scan(&paused))
	require.Equal(t, "PAUSED", paused)
	var restoredTokens int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM accounts WHERE id=$1 AND credentials ? 'access_token'`, account.ID).Scan(&restoredTokens))
	require.Zero(t, restoredTokens, "conflicting rollback must not publish another plaintext carrier")
}
