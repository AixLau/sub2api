// credential-control performs offline single-host Compose cutover. It does not
// synthesize identity verification and never starts old containers after fencing.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/credentialfence"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

type legacyCandidate struct {
	ID          int64
	Credentials []byte
	Concurrency int
}

type batchFence struct {
	delegate repository.CredentialDeploymentFence
	evidence credentialfence.Evidence
	fenced   bool
}

func (f *batchFence) Fence(ctx context.Context) (credentialfence.Evidence, error) {
	if f.fenced {
		return f.evidence, nil
	}
	evidence, err := f.delegate.Fence(ctx)
	if err != nil {
		return credentialfence.Evidence{}, err
	}
	f.evidence = evidence
	f.fenced = true
	return evidence, nil
}

func (f *batchFence) Verify(ctx context.Context, evidence credentialfence.Evidence) error {
	if !f.fenced || evidence.Project != f.evidence.Project || evidence.Service != f.evidence.Service {
		return fmt.Errorf("credential fence evidence mismatch")
	}
	return f.delegate.Verify(ctx, evidence)
}

func main() {
	operation := flag.String("operation", "preview", "preview, migrate, migrate-all, canary, rollback")
	project := flag.String("compose-project", "", "exact Compose project to fence")
	gateway := flag.String("gateway-service", "", "exact gateway service to fence")
	account := flag.Int64("account", 0, "existing account ID")
	principal := flag.Int64("principal", 0, "principal ID")
	actor := flag.Int64("actor", 0, "administrator user ID")
	version := flag.Int64("version", 0, "expected config version")
	total := flag.Int("total", -1, "explicit total concurrency")
	importID := flag.String("import", "", "verified credential import reference")
	opID := flag.String("operation-id", "", "UUID for migration idempotency")
	evidence := flag.String("drain-evidence", "", "audited evidence old executions/bindings were drained")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	db, err := sql.Open("postgres", os.Getenv("SUB2API_CREDENTIAL_CONTROL_DSN"))
	if err != nil {
		fail()
	}
	defer db.Close()
	if db.PingContext(ctx) != nil {
		fail()
	}
	if *operation == "preview" {
		v, err := repository.NewCredentialMigrationStore(db).PreviewCredentialMigration(ctx, []int64{*account})
		if err != nil {
			fail()
		}
		_ = json.NewEncoder(os.Stdout).Encode(v)
		return
	}
	vault, err := service.NewCredentialVaultWithFingerprintKey(os.Getenv("SUB2API_CREDENTIAL_VAULT_KEY"), os.Getenv("SUB2API_CREDENTIAL_FINGERPRINT_KEY"))
	if err != nil {
		fail()
	}
	if repository.CheckCredentialVaultKeys(ctx, db, vault.EncryptionKeyID(), vault.FingerprintKeyID()) != nil {
		fail()
	}
	rollout := repository.NewCredentialRollout(db, vault, credentialfence.Docker{Project: *project, Service: *gateway})
	switch *operation {
	case "migrate":
		var id int64
		id, err = rollout.Migrate(ctx, repository.CredentialMigrationInput{AccountID: *account, ActorID: *actor, ImportID: *importID, OperationID: *opID, DrainEvidence: *evidence, RequestedLimit: *total})
		if err == nil {
			fmt.Printf("{\"principal_id\":%d,\"state\":\"PAUSED\"}\n", id)
		}
	case "migrate-all":
		result, runErr := migrateAll(ctx, db, *actor, *project, *gateway)
		if runErr != nil {
			err = runErr
		} else {
			_ = json.NewEncoder(os.Stdout).Encode(result)
		}
	case "canary":
		err = rollout.Canary(ctx, *actor, *principal, *version)
	case "rollback":
		err = rollout.Rollback(ctx, *actor, *principal, *version)
	default:
		fail()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "credential control rejected; inspect audited preconditions (no credentials logged)")
		os.Exit(1)
	}
}

type migrateAllResult struct {
	Migrated  []int64           `json:"migrated_account_ids"`
	Activated []int64           `json:"activated_principal_ids,omitempty"`
	Pending   []int64           `json:"pending_account_ids"`
	Errors    []migrateAllError `json:"errors,omitempty"`
}

type migrateAllError struct {
	AccountID int64  `json:"account_id"`
	Message   string `json:"message"`
}

// migrateAll verifies every eligible OpenAI OAuth account before taking the
// deployment fence, then reuses the audited single-account rollout for each
// account while keeping the fence in place for the whole batch.
func migrateAll(ctx context.Context, db *sql.DB, actor int64, project, gateway string) (migrateAllResult, error) {
	result := migrateAllResult{}
	if actor <= 0 || strings.TrimSpace(project) == "" || strings.TrimSpace(gateway) == "" {
		return result, fmt.Errorf("actor, compose project, and gateway service are required")
	}
	var admin bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND role='admin' AND status='active' AND deleted_at IS NULL)`, actor).Scan(&admin); err != nil {
		return result, err
	}
	if !admin {
		return result, errors.New("ADMIN_REQUIRED")
	}
	vault, err := service.NewCredentialVaultWithFingerprintKey(os.Getenv("SUB2API_CREDENTIAL_VAULT_KEY"), os.Getenv("SUB2API_CREDENTIAL_FINGERPRINT_KEY"))
	if err != nil {
		return result, err
	}
	if err := repository.CheckCredentialVaultKeys(ctx, db, vault.EncryptionKeyID(), vault.FingerprintKeyID()); err != nil {
		return result, err
	}
	rows, err := db.QueryContext(ctx, `SELECT a.id,a.credentials,a.concurrency
FROM accounts a
WHERE a.deleted_at IS NULL AND a.platform='openai' AND a.type IN ('oauth','setup-token')
  AND COALESCE(a.credentials->>'access_token','') <> ''
  AND NOT EXISTS (SELECT 1 FROM credential_instances i WHERE i.account_id=a.id AND NOT i.retired_to_legacy)
ORDER BY a.id`)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	candidates := make([]legacyCandidate, 0)
	for rows.Next() {
		var candidate legacyCandidate
		if err := rows.Scan(&candidate.ID, &candidate.Credentials, &candidate.Concurrency); err != nil {
			return result, err
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	imports := service.NewCredentialImportService(repository.NewCredentialImportRepository(db), vault, service.NewOpenAICredentialVerifier())
	type plannedMigration struct {
		candidate legacyCandidate
		importID  string
	}
	plans := make([]plannedMigration, 0, len(candidates))
	for _, candidate := range candidates {
		var credentials map[string]any
		if err := json.Unmarshal(candidate.Credentials, &credentials); err != nil {
			result.Pending = append(result.Pending, candidate.ID)
			result.Errors = append(result.Errors, migrateAllError{AccountID: candidate.ID, Message: "INVALID_LEGACY_ACCOUNT"})
			appendPendingMarkError(&result, candidate.ID, markMigrationPending(ctx, db, candidate.ID, "INVALID_LEGACY_ACCOUNT"))
			continue
		}
		accessToken, _ := credentials["access_token"].(string)
		refreshToken, _ := credentials["refresh_token"].(string)
		clientID, _ := credentials["client_id"].(string)
		importID := uuid.NewString()
		view, importErr := imports.Import(ctx, actor, importID, service.CredentialSecret{AccessToken: accessToken, RefreshToken: refreshToken, ClientID: clientID})
		if importErr != nil || view.State != "VERIFIED" {
			result.Pending = append(result.Pending, candidate.ID)
			code := credentialMigrationErrorCode(importErr, view.State)
			result.Errors = append(result.Errors, migrateAllError{AccountID: candidate.ID, Message: code})
			appendPendingMarkError(&result, candidate.ID, markMigrationPending(ctx, db, candidate.ID, code))
			continue
		}
		plans = append(plans, plannedMigration{candidate: candidate, importID: view.ID})
	}
	previouslyMigrated, err := pendingActivationAccounts(ctx, db)
	if err != nil {
		return result, err
	}
	if len(plans) == 0 && len(previouslyMigrated) == 0 {
		return result, nil
	}
	fence := &batchFence{delegate: credentialfence.Docker{Project: project, Service: gateway}}
	rollout := repository.NewCredentialRollout(db, vault, fence)
	for _, item := range plans {
		limit := item.candidate.Concurrency
		if limit < 0 {
			limit = 0
		}
		_, migrateErr := rollout.Migrate(ctx, repository.CredentialMigrationInput{
			AccountID: item.candidate.ID, ActorID: actor, ImportID: item.importID, OperationID: uuid.NewString(),
			DrainEvidence: "automatic-batch-migration-drain", RequestedLimit: limit,
		})
		if migrateErr != nil {
			result.Pending = append(result.Pending, item.candidate.ID)
			code := credentialMigrationErrorCode(migrateErr, "")
			result.Errors = append(result.Errors, migrateAllError{AccountID: item.candidate.ID, Message: code})
			appendPendingMarkError(&result, item.candidate.ID, markMigrationPending(ctx, db, item.candidate.ID, code))
			continue
		}
		result.Migrated = append(result.Migrated, item.candidate.ID)
	}
	activationAccounts := append(append([]int64(nil), result.Migrated...), previouslyMigrated...)
	if len(activationAccounts) > 0 {
		activated, err := rollout.ActivateMigratedBatch(ctx, actor, activationAccounts)
		if err != nil {
			return result, err
		}
		result.Activated = append(result.Activated, activated...)
	}
	return result, nil
}

func pendingActivationAccounts(ctx context.Context, db *sql.DB) ([]int64, error) {
	rows, err := db.QueryContext(ctx, `SELECT account_id FROM credential_migration_records
WHERE state='MIGRATED' AND original_status='active' AND original_schedulable
ORDER BY account_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var accounts []int64
	for rows.Next() {
		var account int64
		if err := rows.Scan(&account); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

func markMigrationPending(ctx context.Context, db *sql.DB, accountID int64, reason string) error {
	if db == nil || accountID <= 0 || reason == "" {
		return errors.New("PENDING_MARK_INVALID")
	}
	_, err := db.ExecContext(ctx, `UPDATE accounts
SET extra=jsonb_set(COALESCE(extra,'{}'::jsonb), '{multi_credential_migration}',
    jsonb_build_object('state','PENDING','reason',$2,'updated_at',CURRENT_TIMESTAMP::text), true)
WHERE id=$1 AND deleted_at IS NULL`, accountID, reason)
	return err
}

func appendPendingMarkError(result *migrateAllResult, accountID int64, err error) {
	if err != nil {
		result.Errors = append(result.Errors, migrateAllError{AccountID: accountID, Message: "PENDING_MARK_FAILED"})
	}
}

// credentialMigrationErrorCode intentionally strips provider/SQL diagnostics
// from the machine-readable report. Token material must never reach command
// output even when a verifier or database driver returns a verbose error.
func credentialMigrationErrorCode(err error, state string) string {
	if err == nil && state == "UNVERIFIED" {
		return "CREDENTIAL_UNVERIFIED"
	}
	if err == nil {
		return "CREDENTIAL_UNVERIFIED"
	}
	switch {
	case errors.Is(err, service.ErrCredentialUnverified):
		return "CREDENTIAL_UNVERIFIED"
	case errors.Is(err, service.ErrCredentialDuplicate):
		return "CREDENTIAL_DUPLICATE"
	case errors.Is(err, service.ErrCredentialLegacyBypass):
		return "CREDENTIAL_LEGACY_BYPASS"
	case errors.Is(err, service.ErrCredentialImportExpired):
		return "CREDENTIAL_IMPORT_EXPIRED"
	default:
		return "MIGRATION_PRECONDITION_FAILED"
	}
}
func fail() {
	fmt.Fprintln(os.Stderr, "credential control unavailable; check configuration and authenticated prerequisites")
	os.Exit(1)
}
