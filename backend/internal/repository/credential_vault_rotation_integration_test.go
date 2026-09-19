//go:build integration

package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/credentialfence"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

type vaultRotationFence struct {
	calls int
	lost  bool
}

func (f *vaultRotationFence) Fence(context.Context) (credentialfence.Evidence, error) {
	f.calls++
	return credentialfence.Evidence{IDs: []string{"local-test-writers"}}, nil
}
func (f *vaultRotationFence) Verify(context.Context, credentialfence.Evidence) error {
	if f.lost {
		return errors.New("fence lost")
	}
	return nil
}
func vaultRotationTestDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	name := "vault_rotation_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err := integrationDB.Exec(`CREATE DATABASE ` + name)
	require.NoError(t, err)
	dsn, err := url.Parse(integrationDSN)
	require.NoError(t, err)
	dsn.Path = "/" + name
	db, err := sql.Open("postgres", dsn.String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, ApplyMigrations(context.Background(), db))
	return db, dsn.String()
}
func TestCredentialVaultOfflineRotationAllSecretClasses(t *testing.T) {
	ctx := context.Background()
	db, dsn := vaultRotationTestDB(t)
	var actor int64
	require.NoError(t, db.QueryRow(`INSERT INTO users(email,password_hash,role) VALUES('vault@example.test','fixture','admin') RETURNING id`).Scan(&actor))
	old, err := service.NewCredentialVault(strings.Repeat("ab", 32))
	require.NoError(t, err)
	next, err := service.NewCredentialVaultWithFingerprintKey(strings.Repeat("cd", 32), old.FingerprintKeyHex())
	require.NoError(t, err)
	repo := &credentialImportRepository{db: db}
	importer := service.NewCredentialImportService(repo, old, mockCredentialVerifier{})
	original := service.CredentialSecret{AccessToken: "mock:rotation:old-access", RefreshToken: "mock:rotation:old-refresh"}
	imported, err := importer.Import(ctx, actor, "create-import", original)
	require.NoError(t, err)
	principal, err := repo.CreateCredentialPrincipal(ctx, actor, "create-principal", service.CreateCredentialPrincipalInput{Name: "rotation", TotalConcurrency: 2, Instances: []service.CreateCredentialInstanceInput{{ImportID: imported.ID, Name: "instance", Weight: 1}}})
	require.NoError(t, err)
	var instance, account int64
	require.NoError(t, db.QueryRow(`SELECT id,account_id FROM credential_instances WHERE principal_id=$1`, principal).Scan(&instance, &account))
	_, err = db.Exec(`UPDATE upstream_principals SET admin_state='ACTIVE',routing_mode='GROUPED' WHERE id=$1`, principal)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE credential_instances SET admin_state='ACTIVE',capabilities=ARRAY['responses','probe'] WHERE id=$1`, instance)
	require.NoError(t, err)
	refresh := &credentialRefreshStore{db: db}
	op, err := refresh.BeginCredentialRefresh(ctx, instance)
	require.NoError(t, err)
	latest := service.CredentialSecret{AccessToken: "mock:rotation:latest-access", RefreshToken: "mock:rotation:latest-refresh"}
	cipher, err := old.Seal(op.ID, latest)
	require.NoError(t, err)
	require.NoError(t, refresh.CompleteCredentialRefresh(ctx, op, service.CredentialRefreshResult{Ciphertext: cipher, AAD: op.ID, ExpiresAt: time.Now().Add(time.Hour), AccessFingerprint: old.Fingerprint("token", latest.AccessToken), RefreshFingerprint: old.Fingerprint("token", latest.RefreshToken)}))
	store := NewPrincipalAdmissionStore(db)
	input := service.AdmissionInput{RequestID: uuid.NewString(), OwnerNonce: uuid.NewString(), Node: "vault-local-test", PayloadDigest: "fixture", PrincipalID: principal, UserID: actor, CandidateIDs: []int64{instance}, Endpoint: "probe", Maintenance: true, OriginalSession: "stable", Deadline: time.Now().Add(time.Minute)}
	decision, err := store.TryAdmit(ctx, input)
	require.NoError(t, err)
	require.Equal(t, service.AdmissionAdmitted, decision.Code)
	require.NoError(t, store.BeginDispatch(ctx, decision.Snapshot.Lease))
	require.NoError(t, store.Finish(ctx, service.FinishAdmissionInput{Lease: decision.Snapshot.Lease}))
	pending, err := refresh.BeginCredentialRefresh(ctx, instance)
	require.NoError(t, err)
	pendingSecret := service.CredentialSecret{AccessToken: "mock:rotation:unknown-access", RefreshToken: "mock:rotation:unknown-refresh"}
	pendingCipher, err := old.Seal(pending.ID, pendingSecret)
	require.NoError(t, err)
	require.NoError(t, refresh.MarkCredentialRefreshUnknown(ctx, pending, &service.CredentialRefreshResult{Ciphertext: pendingCipher, AAD: pending.ID, ExpiresAt: time.Now().Add(time.Hour), AccessFingerprint: old.Fingerprint("token", pendingSecret.AccessToken), RefreshFingerprint: old.Fingerprint("token", pendingSecret.RefreshToken)}))
	unverified := service.NewCredentialImportService(repo, old, nil)
	unused, err := unverified.Import(ctx, actor, "unused", service.CredentialSecret{AccessToken: "mock:rotation:unused"})
	require.NoError(t, err)
	require.Equal(t, "UNVERIFIED", unused.State)
	archiveAAD := "migration:" + uuid.NewString()
	archive, err := old.SealData(archiveAAD, []byte(`{"access_token":"archive-old-token","other":"preserve"}`))
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO credential_migration_records(principal_id,account_id,actor_id,credentials_ciphertext,credentials_aad,original_status,original_schedulable,original_concurrency,fence_evidence,drain_evidence,operation_id) VALUES($1,$2,$3,$4,$5,'inactive',false,2,'{}','mock evidence',$6)`, principal, account, actor, archive, archiveAAD, uuid.NewString())
	require.NoError(t, err)
	snapshot := func() string {
		var raw string
		require.NoError(t, db.QueryRow(`SELECT jsonb_build_object('principal',(SELECT to_jsonb(p) FROM upstream_principals p WHERE p.id=$1),'instance',(SELECT to_jsonb(i) FROM credential_instances i WHERE i.id=$2),'profile',(SELECT to_jsonb(p) FROM credential_identity_profiles p WHERE p.instance_id=$2),'binding',(SELECT jsonb_agg(to_jsonb(b)) FROM session_bindings b WHERE b.principal_id=$1),'leases',(SELECT jsonb_agg(to_jsonb(l)) FROM request_leases l WHERE l.principal_id=$1),'fingerprints',(SELECT jsonb_agg(to_jsonb(f) ORDER BY f.fingerprint) FROM credential_fingerprints f WHERE f.instance_id=$2))::text`, principal, instance).Scan(&raw))
		return raw
	}
	before := snapshot()
	fence := &vaultRotationFence{}
	rotation := NewCredentialVaultRotation(db, fence)
	operation := uuid.NewString()
	wrongOld, err := service.NewCredentialVaultWithFingerprintKey(strings.Repeat("ef", 32), old.FingerprintKeyHex())
	require.NoError(t, err)
	_, err = rotation.Rotate(ctx, actor, operation, wrongOld, next)
	require.Error(t, err, "wrong encryption key cannot partially rotate")
	// Corruption midway must roll back earlier re-encryptions, including imports.
	_, err = db.Exec(`UPDATE credential_refresh_ops SET result_ciphertext='corrupt' WHERE id=$1`, pending.ID)
	require.NoError(t, err)
	_, err = rotation.Rotate(ctx, actor, operation, old, next)
	require.Error(t, err)
	var preserved []byte
	require.NoError(t, db.QueryRow(`SELECT secret_ciphertext FROM credential_imports WHERE id=$1`, unused.ID).Scan(&preserved))
	_, err = old.Open(unused.ID, preserved)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE credential_refresh_ops SET result_ciphertext=$2 WHERE id=$1`, pending.ID, pendingCipher)
	require.NoError(t, err)
	fence.lost = true
	_, err = rotation.Rotate(ctx, actor, operation, old, next)
	require.Error(t, err)
	fence.lost = false
	connector, err := pq.NewConnector(dsn)
	require.NoError(t, err)
	ackLost := &atomic.Bool{}
	ackLost.Store(true)
	faultDB := sql.OpenDB(vaultCommitLostConnector{Connector: connector, armed: ackLost})
	defer faultDB.Close()
	_, err = NewCredentialVaultRotation(faultDB, fence).Rotate(ctx, actor, operation, old, next)
	require.Error(t, err, "real PostgreSQL COMMIT succeeds but its acknowledgement is lost")
	var committedOperation string
	require.NoError(t, db.QueryRow(`SELECT operation_id FROM credential_vault_state WHERE id=1`).Scan(&committedOperation))
	require.Equal(t, operation, committedOperation)
	counts, err := rotation.Rotate(ctx, actor, operation, old, next)
	require.NoError(t, err)
	require.Equal(t, map[string]int64{"credential_legacy_refresh_operations": 0, "credential_imports": 1, "credential_secrets": 2, "credential_refresh_ops": 1, "credential_migration_records": 1}, counts)
	require.JSONEq(t, before, snapshot())
	legacyRegistry := newAccountRepositoryWithSQL(nil, db, nil)
	for _, token := range []string{original.AccessToken, original.RefreshToken, latest.AccessToken, latest.RefreshToken, pendingSecret.AccessToken, pendingSecret.RefreshToken, "mock:rotation:unused"} {
		known, lookupErr := legacyRegistry.KnownCredentialTokenFingerprints(ctx, []string{next.Fingerprint("token", token)})
		require.NoError(t, lookupErr)
		require.True(t, known)
	}
	known, lookupErr := legacyRegistry.KnownCredentialTokenFingerprints(ctx, []string{next.Fingerprint("token", "unregistered")})
	require.NoError(t, lookupErr)
	require.False(t, known)
	require.ErrorIs(t, CheckCredentialVaultKeys(ctx, db, old.EncryptionKeyID(), old.FingerprintKeyID()), service.ErrCredentialVaultUnavailable)
	require.NoError(t, CheckCredentialVaultKeys(ctx, db, next.EncryptionKeyID(), next.FingerprintKeyID()))
	wrongFP, err := service.NewCredentialVault(strings.Repeat("cd", 32))
	require.NoError(t, err)
	require.Error(t, CheckCredentialVaultKeys(ctx, db, wrongFP.EncryptionKeyID(), wrongFP.FingerprintKeyID()))
	same, err := rotation.Rotate(ctx, actor, operation, old, next)
	require.NoError(t, err)
	require.Equal(t, counts, same)
	require.NoError(t, db.QueryRow(`SELECT secret_ciphertext FROM credential_secrets WHERE instance_id=$1 AND credential_version=2`, instance).Scan(&preserved))
	actual, err := next.Open(op.ID, preserved)
	require.NoError(t, err)
	require.Equal(t, latest, actual)
	require.NoError(t, db.QueryRow(`SELECT result_ciphertext FROM credential_refresh_ops WHERE id=$1 AND state='REFRESH_RESULT_UNKNOWN'`, pending.ID).Scan(&preserved))
	actual, err = next.Open(pending.ID, preserved)
	require.NoError(t, err)
	require.Equal(t, pendingSecret, actual)
	importer = service.NewCredentialImportService(repo, next, mockCredentialVerifier{})
	_, err = importer.Import(ctx, actor, "duplicate-old", original)
	require.ErrorIs(t, err, service.ErrCredentialDuplicate)
	_, err = importer.Import(ctx, actor, "duplicate-latest", latest)
	require.ErrorIs(t, err, service.ErrCredentialDuplicate)
	var safe string
	require.NoError(t, db.QueryRow(`SELECT safe_payload::text FROM credential_audit_outbox WHERE event_id=$1`, operation).Scan(&safe))
	var safeCounts map[string]int64
	require.NoError(t, json.Unmarshal([]byte(safe), &safeCounts))
	require.Equal(t, counts, safeCounts)
	// Rollback re-encrypts CURRENT facts. It never restores old token snapshots.
	_, err = rotation.Rotate(ctx, actor, uuid.NewString(), next, old)
	require.NoError(t, err)
	require.NoError(t, db.QueryRow(`SELECT secret_ciphertext FROM credential_secrets WHERE instance_id=$1 AND credential_version=2`, instance).Scan(&preserved))
	actual, err = old.Open(op.ID, preserved)
	require.NoError(t, err)
	require.Equal(t, latest, actual)
	require.JSONEq(t, before, snapshot())
}

type vaultCommitLostConnector struct {
	driver.Connector
	armed *atomic.Bool
}

func (c vaultCommitLostConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &vaultCommitLostConn{Conn: conn, armed: c.armed}, nil
}

type vaultCommitLostConn struct {
	driver.Conn
	armed *atomic.Bool
}

func (c *vaultCommitLostConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	tx, err := c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &vaultCommitLostTx{Tx: tx, armed: c.armed}, nil
}
func (c *vaultCommitLostConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, q, args)
}
func (c *vaultCommitLostConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, q, args)
}

type vaultCommitLostTx struct {
	driver.Tx
	armed *atomic.Bool
}

func (tx *vaultCommitLostTx) Commit() error {
	if err := tx.Tx.Commit(); err != nil {
		return err
	}
	if tx.armed.CompareAndSwap(true, false) {
		return errors.New("mock lost commit acknowledgement")
	}
	return nil
}
