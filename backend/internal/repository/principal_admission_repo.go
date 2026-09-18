package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

type principalAdmissionStore struct {
	// Test seam for controlled retry comparisons; runtime leaves this nil.
	advisoryRetryDelay func(int) time.Duration
	db                 *sql.DB
	lifecycleDB        *sql.DB
	initOnce           sync.Once
	admissionGate      credentialAdmissionTurn
	principalTurns     credentialPrincipalTurns
	waitMu             sync.Mutex
	waiters            map[string]*credentialAdmissionWaiter
	retryReady         map[string]bool
	pumpRunning        bool
	wake               chan struct{}
}

func (s *principalAdmissionStore) initializeQueue() {
	s.initOnce.Do(func() {
		s.retryReady = map[string]bool{}
		s.waiters = map[string]*credentialAdmissionWaiter{}
		s.wake = make(chan struct{}, 1)
	})
}
func (s *principalAdmissionStore) criticalDB() *sql.DB {
	if s.lifecycleDB != nil {
		return s.lifecycleDB
	}
	return s.db
}
func (s *principalAdmissionStore) signalQueue() {
	s.initializeQueue()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func NewPrincipalAdmissionStore(db *sql.DB) service.PrincipalAdmissionStore {
	return &principalAdmissionStore{db: db}
}
func admissionReject(reason string) service.AdmissionDecision {
	return service.AdmissionDecision{Code: service.AdmissionRejected, Reason: reason}
}
func (s *principalAdmissionStore) TryAdmit(ctx context.Context, in service.AdmissionInput) (decision service.AdmissionDecision, admissionErr error) {
	started := time.Now()
	defer func() { service.ObserveCredentialAdmission(time.Since(started).Seconds(), decision.Code, admissionErr) }()
	rejected := admissionReject("ADMISSION_STORE_UNAVAILABLE")
	if _, err := uuid.Parse(in.RequestID); err != nil {
		return admissionReject("INVALID_REQUEST_ID"), nil
	}
	if _, err := uuid.Parse(in.OwnerNonce); err != nil {
		return admissionReject("INVALID_OWNER"), nil
	}
	if in.Node == "" || in.PayloadDigest == "" || len(in.CandidateIDs) == 0 || len(in.CandidateIDs) > 16 {
		return admissionReject("INVALID_ADMISSION_INPUT"), nil
	}
	if in.Endpoint != "responses" && in.Endpoint != "passthrough" && in.Endpoint != "compact" && in.Endpoint != "probe" {
		return admissionReject("GROUPED_TRANSPORT_UNSUPPORTED"), nil
	}
	if in.HasState && in.OriginalSession == "" {
		return admissionReject("SESSION_SCOPE_REQUIRED"), nil
	}
	s.initializeQueue()
	s.waitMu.Lock()
	priority := s.retryReady[in.RequestID]
	delete(s.retryReady, in.RequestID)
	s.waitMu.Unlock()
	releasePrincipal, err := s.principalTurns.acquire(ctx, in.PrincipalID, priority)
	if err != nil {
		if errors.Is(err, service.ErrAdmissionLocalQueueFull) {
			return admissionReject("ADMISSION_LOCAL_QUEUE_FULL"), nil
		}
		return rejected, err
	}
	defer releasePrincipal()
	tx, releaseNode, err := s.beginAdmissionTurn(ctx, in.PrincipalID, priority)
	if err != nil {
		if errors.Is(err, service.ErrAdmissionLocalQueueFull) {
			return admissionReject("ADMISSION_LOCAL_QUEUE_FULL"), nil
		}
		return rejected, err
	}
	defer releaseNode()
	defer tx.Rollback()
	// All operations use user -> principal -> instance -> request/binding/lease.
	_, err = tx.ExecContext(ctx, `INSERT INTO principal_user_capacity(user_id,principal_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, in.UserID, in.PrincipalID)
	if err != nil {
		return rejected, err
	}
	var userOccupied int
	err = tx.QueryRowContext(ctx, `SELECT occupied FROM principal_user_capacity WHERE user_id=$1 AND principal_id=$2 FOR UPDATE`, in.UserID, in.PrincipalID).Scan(&userOccupied)
	if err != nil {
		return rejected, err
	}
	var limit, occupied, queueLimit int
	var version, epoch, lastInstance int64
	var admin, mode, verified string
	var userLimit sql.NullInt64
	var protected, drain sql.NullTime
	var now time.Time
	err = tx.QueryRowContext(ctx, `SELECT requested_limit,occupied,config_version,admission_epoch,admin_state,routing_mode,verification_state,user_concurrency_limit,queue_limit,last_instance_id,protected_until,drain_deadline
 FROM upstream_principals WHERE id=$1 AND tenant_id=1 FOR NO KEY UPDATE`, in.PrincipalID).Scan(&limit, &occupied, &version, &epoch, &admin, &mode, &verified, &userLimit, &queueLimit, &lastInstance, &protected, &drain)
	if err != nil {
		return rejected, err
	}
	// Read wall time in a separate statement AFTER the principal lock. Even a
	// clock_timestamp() expression in a locking SELECT can run before its wait.
	var ledgerCount, instanceCount int
	var competingTicket, quotaBlocked bool
	err = tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM request_leases WHERE principal_id=$1 AND state<>'RELEASED'),
        (SELECT COALESCE(sum(occupied),0) FROM credential_instances WHERE principal_id=$1),clock_timestamp(),
        EXISTS(SELECT 1 FROM admission_tickets WHERE principal_id=$1 AND state='QUEUED' AND request_id<>$2),
        EXISTS(SELECT 1 FROM upstream_principal_quota_domains p JOIN upstream_quota_domains q ON q.id=p.quota_domain_id
          WHERE p.principal_id=$1 AND (q.requires_admin_reset OR q.blocked_until>clock_timestamp()))`, in.PrincipalID, in.RequestID).Scan(&ledgerCount, &instanceCount, &now, &competingTicket, &quotaBlocked)
	if err != nil {
		return rejected, err
	}
	if occupied != ledgerCount || occupied != instanceCount {
		return admissionReject("LEDGER_MISMATCH_FROZEN"), nil
	}
	if in.ExpectedConfigVersion != 0 && in.ExpectedConfigVersion != version {
		return service.AdmissionDecision{Code: service.AdmissionConfigStale, Reason: "CONFIG_STALE"}, nil
	}
	if mode != "GROUPED" || verified != "VERIFIED" || (admin != "ACTIVE" && admin != "DRAINING") {
		return admissionReject("PRINCIPAL_PAUSED"), nil
	}
	// Shared quota writers lock every linked principal before changing the
	// domain. This read is still after that same principal lock, alongside the
	// ledger check, and retains the real-time expiry predicate.
	if quotaBlocked {
		return admissionReject("SHARED_QUOTA_PROTECTED"), nil
	}
	if in.Deadline.IsZero() || !in.Deadline.After(now) {
		return admissionReject("ADMISSION_QUEUE_TIMEOUT"), nil
	}
	var groupID int64
	var allowlistJSON []byte
	if in.Maintenance {
		if in.Endpoint != "probe" && in.Endpoint != "compact" {
			return admissionReject("GROUPED_TRANSPORT_UNSUPPORTED"), nil
		}
		var admin bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND role='admin' AND status='active' AND deleted_at IS NULL)`, in.UserID).Scan(&admin)
		if err != nil {
			return rejected, err
		}
		if !admin {
			return admissionReject("AUTHORIZATION_REVOKED"), nil
		}
		var probes int
		err = tx.QueryRowContext(ctx, `SELECT count(*) FROM request_leases l JOIN logical_requests r ON r.id=l.request_id WHERE l.principal_id=$1 AND r.api_key_id IS NULL AND l.state<>'RELEASED'`, in.PrincipalID).Scan(&probes)
		if err != nil {
			return rejected, err
		}
		if probes > 0 {
			return admissionReject("MAINTENANCE_BUDGET_EXHAUSTED"), nil
		}
	} else {
		// Authoritative key/user revocation and group membership, not a cache boolean.
		err = tx.QueryRowContext(ctx, `SELECT k.group_id,g.model_allowlist FROM api_keys k JOIN users u ON u.id=k.user_id JOIN groups g ON g.id=k.group_id
 WHERE k.id=$1 AND k.user_id=$2 AND k.status='active' AND k.deleted_at IS NULL
 AND u.status='active' AND u.deleted_at IS NULL AND (k.expires_at IS NULL OR k.expires_at>clock_timestamp())
 AND g.status='active' AND g.deleted_at IS NULL AND (g.subscription_type='subscription' OR (NOT g.is_exclusive AND NOT u.restrict_public_groups) OR EXISTS(SELECT 1 FROM user_allowed_groups ag WHERE ag.user_id=u.id AND ag.group_id=g.id))`, in.APIKeyID, in.UserID).Scan(&groupID, &allowlistJSON)
		if errors.Is(err, sql.ErrNoRows) {
			return admissionReject("AUTHORIZATION_REVOKED"), nil
		}
		if err != nil {
			return rejected, err
		}
		var allowlist service.GroupModelAllowlist
		if err = json.Unmarshal(allowlistJSON, &allowlist); err != nil {
			return rejected, err
		}
		if !allowlist.Allows(in.Model) {
			return admissionReject("MODEL_NOT_ALLOWED"), nil
		}
	}
	scope := service.CredentialScopeHash(in.UserID, in.APIKeyID)
	key := in.IdempotencyKey
	if key == "" {
		key = in.RequestID
	}
	key = service.CredentialDigest([]byte(key))
	var requestID, payload, status, ownerNode string
	var requestPrincipal int64
	err = tx.QueryRowContext(ctx, `SELECT id,payload_digest,status,owner_node,principal_id FROM logical_requests WHERE caller_scope_hash=$1 AND endpoint=$2 AND idempotency_hash=$3`, scope, in.Endpoint, key).Scan(&requestID, &payload, &status, &ownerNode, &requestPrincipal)
	if err == nil {
		if payload != in.PayloadDigest {
			return admissionReject("IDEMPOTENCY_PAYLOAD_MISMATCH"), nil
		}
		if status != "QUEUED" {
			return service.AdmissionDecision{Code: service.AdmissionAlreadyRunning, Reason: "REQUEST_ALREADY_RUNNING"}, nil
		}
		// Re-poll uses the original owner; another node cannot adopt a queued body.
		if requestID != in.RequestID || ownerNode != in.Node || requestPrincipal != in.PrincipalID {
			return service.AdmissionDecision{Code: service.AdmissionAlreadyRunning, Reason: "REQUEST_ALREADY_RUNNING"}, nil
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return rejected, err
	}
	var bindingID, bindingGeneration, bindingState string
	var boundInstance int64
	var bindingExpiry time.Time
	sessionHash := service.CredentialDigest([]byte(in.OriginalSession))
	if in.OriginalSession != "" {
		err = tx.QueryRowContext(ctx, `SELECT id,instance_id,generation,state,idle_expires_at FROM session_bindings
 WHERE principal_id=$1 AND caller_scope_hash=$2 AND session_hash=$3 ORDER BY last_used_at DESC LIMIT 1`, in.PrincipalID, scope, sessionHash).Scan(&bindingID, &boundInstance, &bindingGeneration, &bindingState, &bindingExpiry)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return rejected, err
		}
		if bindingID != "" {
			var busy bool
			err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM request_leases WHERE binding_id=$1 AND state<>'RELEASED')
                OR EXISTS(SELECT 1 FROM admission_tickets WHERE binding_id=$1 AND state='QUEUED' AND deadline>clock_timestamp())`, bindingID).Scan(&busy)
			if err != nil {
				return rejected, err
			}
			if bindingState == "REVOKED" {
				return admissionReject("SESSION_BINDING_EXPIRED"), nil
			}
			if bindingState == "EXPIRED" || (!bindingExpiry.After(now) && !busy) {
				if in.HasState {
					return admissionReject("SESSION_BINDING_EXPIRED"), nil
				}
				_, err = tx.ExecContext(ctx, `UPDATE session_bindings SET state='EXPIRED',tombstone_until=clock_timestamp()+INTERVAL '7 days' WHERE id=$1`, bindingID)
				if err != nil {
					return rejected, err
				}
				bindingID = ""
				boundInstance = 0
				bindingGeneration = ""
			}
		}
	}
	if admin == "DRAINING" && (bindingID == "" || !drain.Valid || !drain.Time.After(now)) {
		return admissionReject("PRINCIPAL_DRAINING"), nil
	}
	type instance struct {
		id, account, credential                       int64
		generation, state, credentialState, transport string
		hard, health                                  sql.NullInt64
		occupied                                      int
		weight                                        float64
		drain, cooldown                               sql.NullTime
	}
	rows, err := tx.QueryContext(ctx, `SELECT i.id,i.account_id,i.identity_generation,i.credential_version,i.admin_state,i.credential_state,i.transport_state,i.hard_max,i.health_capacity,i.occupied,i.weight,i.drain_deadline,i.cooldown_until
 FROM credential_instances i JOIN accounts a ON a.id=i.account_id
 WHERE i.principal_id=$1 AND i.id=ANY($2) AND a.deleted_at IS NULL
 AND ($5 OR EXISTS(SELECT 1 FROM account_groups g WHERE g.account_id=a.id AND g.group_id=$3))
 AND $4=ANY(i.capabilities) ORDER BY i.id FOR UPDATE OF i`, in.PrincipalID, pq.Array(in.CandidateIDs), groupID, in.Endpoint, in.Maintenance)
	if err != nil {
		return rejected, err
	}
	var candidates []instance
	for rows.Next() {
		var v instance
		if err = rows.Scan(&v.id, &v.account, &v.generation, &v.credential, &v.state, &v.credentialState, &v.transport, &v.hard, &v.health, &v.occupied, &v.weight, &v.drain, &v.cooldown); err != nil {
			rows.Close()
			return rejected, err
		}
		candidates = append(candidates, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return rejected, err
	}
	// Candidate row locks can also wait. Re-evaluate live limits after acquiring
	// them, rather than carrying the principal-lock timestamp into dispatch.
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return rejected, err
	}
	if !in.Deadline.After(now) {
		return admissionReject("ADMISSION_QUEUE_TIMEOUT"), nil
	}
	if admin == "DRAINING" && (!drain.Valid || !drain.Time.After(now)) {
		return admissionReject("PRINCIPAL_DRAINING"), nil
	}
	var selected *instance
	waitReason := "INSTANCE_UNAVAILABLE"
	for i := range candidates {
		v := &candidates[i]
		if boundInstance != 0 && v.id != boundInstance {
			continue
		}
		if boundInstance != 0 && v.generation != bindingGeneration {
			return admissionReject("SESSION_GENERATION_MISMATCH"), nil
		}
		if v.state != "ACTIVE" && !(v.state == "DRAINING" && bindingID != "" && v.drain.Valid && v.drain.Time.After(now)) {
			continue
		}
		if v.credentialState != "VALID" {
			waitReason = "REFRESH_WAIT"
			continue
		}
		if v.cooldown.Valid && v.cooldown.Time.After(now) {
			waitReason = "COOLDOWN"
			continue
		}
		if v.transport != "HEALTHY" && v.transport != "DEGRADED" && v.transport != "HALF_OPEN" {
			continue
		}
		ceiling := limit
		if v.hard.Valid {
			ceiling = min(ceiling, int(v.hard.Int64))
		}
		if v.health.Valid {
			ceiling = min(ceiling, int(v.health.Int64))
		}
		if v.occupied >= ceiling {
			waitReason = "INSTANCE_CONCURRENCY_EXCEEDED"
			continue
		}
		if selected == nil || float64(v.occupied+1)/v.weight < float64(selected.occupied+1)/selected.weight ||
			(float64(v.occupied+1)/v.weight == float64(selected.occupied+1)/selected.weight && v.id > lastInstance && selected.id <= lastInstance) {
			selected = v
		}
	}
	// Ignore expired tickets for admission, but preserve their terminal history.
	_, err = tx.ExecContext(ctx, `UPDATE admission_tickets SET state='EXPIRED' WHERE principal_id=$1 AND state='QUEUED' AND deadline<=clock_timestamp()`, in.PrincipalID)
	if err != nil {
		return rejected, err
	}
	var priorTicketState string
	err = tx.QueryRowContext(ctx, `SELECT state FROM admission_tickets WHERE request_id=$1`, in.RequestID).Scan(&priorTicketState)
	if err == nil && priorTicketState != "QUEUED" {
		return admissionReject("ADMISSION_QUEUE_TIMEOUT"), nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return rejected, err
	}
	fairWait := false
	if selected != nil && competingTicket {
		// Issue bounded offers once, then only the selected original owners may
		// retry. Every capacity/permission check above remains authoritative.
		if err = advanceCredentialOffers(ctx, tx, in.PrincipalID); err != nil {
			return rejected, err
		}
		var offered bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM admission_tickets WHERE request_id=$1 AND state='QUEUED' AND offer_until>clock_timestamp()) OR NOT EXISTS(SELECT 1 FROM admission_tickets WHERE principal_id=$2 AND state='QUEUED' AND offer_until>clock_timestamp())`, in.RequestID, in.PrincipalID).Scan(&offered)
		if err != nil {
			return rejected, err
		}
		fairWait = !offered
	}
	// Persist a logical request only after authenticating its scope.
	if requestID == "" {
		_, err = tx.ExecContext(ctx, `INSERT INTO logical_requests(id,principal_id,user_id,api_key_id,caller_scope_hash,idempotency_hash,payload_digest,endpoint,model,owner_node,deadline)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, in.RequestID, in.PrincipalID, in.UserID, nullablePositive(in.APIKeyID), scope, key, in.PayloadDigest, in.Endpoint, in.Model, in.Node, in.Deadline)
		if err != nil {
			return rejected, err
		}
	}
	if fairWait || selected == nil || occupied >= limit || (userLimit.Valid && userOccupied >= int(userLimit.Int64)) || (protected.Valid && (protected.Time.After(now) || occupied > 0)) {
		if fairWait {
			waitReason = "FAIRNESS_WAIT"
		}
		if occupied >= limit {
			waitReason = "PRINCIPAL_CONCURRENCY_EXCEEDED"
		}
		if userLimit.Valid && userOccupied >= int(userLimit.Int64) {
			waitReason = "USER_CONCURRENCY_EXCEEDED"
		}
		if protected.Valid && (protected.Time.After(now) || occupied > 0) {
			waitReason = "COOLDOWN"
		}
		var queued int
		err = tx.QueryRowContext(ctx, `SELECT count(*) FROM admission_tickets WHERE principal_id=$1 AND state='QUEUED' AND deadline>clock_timestamp() AND request_id<>$2`, in.PrincipalID, in.RequestID).Scan(&queued)
		if err != nil {
			return rejected, err
		}
		if queued >= queueLimit {
			return admissionReject("ADMISSION_QUEUE_FULL"), nil
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO admission_tickets(id,request_id,principal_id,binding_id,user_id,instance_id,owner_node,reason,deadline,candidate_ids,session_hash)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,LEAST($9,clock_timestamp()+INTERVAL '15 seconds'),$10,$11) ON CONFLICT(request_id) DO UPDATE SET reason=EXCLUDED.reason`, uuid.NewString(), in.RequestID, in.PrincipalID, nullableString(bindingID), in.UserID, nullablePositive(boundInstance), in.Node, waitReason, in.Deadline, pq.Array(in.CandidateIDs), nullableString(sessionHash))
		if err != nil {
			return rejected, err
		}
		if err = tx.Commit(); err != nil {
			return rejected, service.ErrAdmissionStoreUnavailable
		}
		return service.AdmissionDecision{Code: service.AdmissionWait, Reason: waitReason}, nil
	}
	var ticketState string
	var ticketDeadline time.Time
	err = tx.QueryRowContext(ctx, `SELECT state,deadline FROM admission_tickets WHERE request_id=$1 FOR UPDATE`, in.RequestID).Scan(&ticketState, &ticketDeadline)
	if err == nil && (ticketState != "QUEUED" || !ticketDeadline.After(now)) {
		return admissionReject("ADMISSION_QUEUE_TIMEOUT"), nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return rejected, err
	}
	snap := &service.CredentialExecutionSnapshot{Lease: service.LeaseRef{ID: uuid.NewString(), RequestID: in.RequestID, PrincipalID: in.PrincipalID, InstanceID: selected.id, Generation: selected.generation, UserID: in.UserID, OwnerNonce: in.OwnerNonce, Epoch: epoch}, AccountID: selected.account, CredentialVersion: selected.credential, ConfigVersion: version, Endpoint: in.Endpoint, CallerScope: scope, Deadline: in.Deadline}
	err = tx.QueryRowContext(ctx, `SELECT p.installation_id,p.source,s.secret_ciphertext,s.secret_aad,a.proxy_id,a.updated_at
 FROM credential_identity_profiles p JOIN credential_secrets s ON s.instance_id=p.instance_id AND s.credential_version=$2
 JOIN credential_instances i ON i.id=p.instance_id JOIN accounts a ON a.id=i.account_id
 WHERE p.instance_id=$1 AND p.generation=$3 AND s.expires_at>clock_timestamp()+INTERVAL '30 seconds'`, selected.id, selected.credential, selected.generation).Scan(&snap.InstallationID, &snap.IdentitySource, &snap.SecretCiphertext, &snap.SecretAAD, &snap.ProxyID, &snap.AccountUpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return admissionReject("CREDENTIAL_REAUTH_REQUIRED"), nil
	}
	if err != nil {
		return rejected, err
	}
	if err = loadCredentialSnapshotProxy(ctx, tx, snap); err != nil {
		return rejected, err
	}
	if bindingID == "" && in.OriginalSession != "" {
		bindingID = uuid.NewString()
		_, err = tx.ExecContext(ctx, `INSERT INTO session_bindings(id,principal_id,caller_scope_hash,session_hash,instance_id,generation,last_used_at,idle_expires_at) VALUES($1,$2,$3,$4,$5,$6,clock_timestamp(),clock_timestamp()+INTERVAL '24 hours')`, bindingID, in.PrincipalID, scope, sessionHash, selected.id, selected.generation)
		if err != nil {
			return rejected, err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO request_leases(id,request_id,attempt_no,principal_id,instance_id,generation,user_id,owner_nonce,epoch,credential_version,config_version,binding_id,state,deadline,account_updated_at,proxy_id,proxy_updated_at,heartbeat_at)
 VALUES($1,$2,1,$3,$4,$5,$6,$7,$8,$9,$10,$11,'RESERVED',$12,$13,$14,$15,clock_timestamp())`, snap.Lease.ID, in.RequestID, in.PrincipalID, selected.id, selected.generation, in.UserID, in.OwnerNonce, epoch, selected.credential, version, nullableString(bindingID), in.Deadline, snap.AccountUpdatedAt, snap.ProxyID, snap.ProxyUpdatedAt)
	if err != nil {
		return rejected, err
	}
	// Every capacity row is already locked in the global order. Batch writes
	// to distinct rows in one statement; no CTE reads another CTE's table
	// changes. This removes network round trips, not any admission check.
	_, err = tx.ExecContext(ctx, `WITH user_capacity AS (
 UPDATE principal_user_capacity SET occupied=occupied+1,last_admitted_at=CURRENT_TIMESTAMP WHERE user_id=$1 AND principal_id=$2
), principal_capacity AS (
 UPDATE upstream_principals SET occupied=occupied+1,last_instance_id=$3 WHERE id=$2
), instance_capacity AS (
 UPDATE credential_instances SET occupied=occupied+1 WHERE id=$3
), request_state AS (
 UPDATE logical_requests SET status='EXECUTING' WHERE id=$4
), audit AS (
 INSERT INTO credential_audit_outbox(event_id,principal_id,instance_id,version,event_type,safe_payload)
 VALUES($5,$2,$3,$6,'LEASE_RESERVED',jsonb_build_object('lease_id',$7::text))
)
 UPDATE admission_tickets SET state='ADMITTED' WHERE request_id=$4`, in.UserID, in.PrincipalID, selected.id, in.RequestID, uuid.NewString(), snap.Lease.Epoch, snap.Lease.ID)
	if err != nil {
		return rejected, err
	}

	var stillLive bool
	err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()<$1 AND ($2::timestamptz IS NULL OR clock_timestamp()<$2)`, in.Deadline, nullableAdmissionTime(ticketDeadline)).Scan(&stillLive)
	if err != nil {
		return rejected, err
	}
	if !stillLive {
		return admissionReject("ADMISSION_QUEUE_TIMEOUT"), nil
	}
	if err = tx.Commit(); err != nil {
		return rejected, service.ErrAdmissionStoreUnavailable
	}
	return service.AdmissionDecision{Code: service.AdmissionAdmitted, Snapshot: snap}, nil
}
func nullablePositive(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}
func admissionAudit(ctx context.Context, tx *sql.Tx, ref service.LeaseRef, event string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO credential_audit_outbox(event_id,principal_id,instance_id,version,event_type,safe_payload) VALUES($1,$2,$3,$4,$5,jsonb_build_object('lease_id',$6::text))`, uuid.NewString(), ref.PrincipalID, ref.InstanceID, ref.Epoch, event, ref.ID)
	return err
}

// lockLease validates all fencing fields after locking resources in one order.
func (s *principalAdmissionStore) lockLease(ctx context.Context, ref service.LeaseRef) (*sql.Tx, string, error) {
	tx, err := s.criticalDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, "", service.ErrAdmissionStoreUnavailable
	}
	fail := func(err error) (*sql.Tx, string, error) { tx.Rollback(); return nil, "", err }
	for _, q := range []struct {
		query string
		args  []any
	}{
		{`SELECT occupied FROM principal_user_capacity WHERE user_id=$1 AND principal_id=$2 FOR UPDATE`, []any{ref.UserID, ref.PrincipalID}},
		{`SELECT occupied FROM upstream_principals WHERE id=$1 FOR NO KEY UPDATE`, []any{ref.PrincipalID}},
		{`SELECT occupied FROM credential_instances WHERE id=$1 AND principal_id=$2 FOR UPDATE`, []any{ref.InstanceID, ref.PrincipalID}},
	} {
		var n int
		if err = tx.QueryRowContext(ctx, q.query, q.args...).Scan(&n); err != nil {
			return fail(err)
		}
	}
	var state string
	err = tx.QueryRowContext(ctx, `SELECT state FROM request_leases WHERE id=$1 AND request_id=$2 AND principal_id=$3 AND instance_id=$4 AND user_id=$5 AND owner_nonce=$6 AND epoch=$7 AND generation=$8 FOR UPDATE`, ref.ID, ref.RequestID, ref.PrincipalID, ref.InstanceID, ref.UserID, ref.OwnerNonce, ref.Epoch, ref.Generation).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return fail(service.ErrAdmissionOwnership)
	}
	if err != nil {
		return fail(err)
	}
	return tx, state, nil
}
func (s *principalAdmissionStore) BeginDispatch(ctx context.Context, ref service.LeaseRef) error {
	tx, state, err := s.lockLease(ctx, ref)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if state != "RESERVED" {
		return service.ErrAdmissionOwnership
	}
	var allowed bool
	var requestModel string
	var currentAllowlist []byte
	err = tx.QueryRowContext(ctx, `SELECT p.admission_epoch=l.epoch AND p.admin_state IN ('ACTIVE','DRAINING') AND p.routing_mode='GROUPED'
 AND i.admin_state IN ('ACTIVE','DRAINING') AND i.identity_generation=l.generation AND l.deadline>clock_timestamp()
 AND EXISTS(SELECT 1 FROM accounts a WHERE a.id=i.account_id AND a.updated_at=l.account_updated_at AND a.proxy_id IS NOT DISTINCT FROM l.proxy_id)
 AND (l.proxy_id IS NULL OR EXISTS(SELECT 1 FROM proxies x WHERE x.id=l.proxy_id AND x.updated_at=l.proxy_updated_at AND x.status='active' AND x.deleted_at IS NULL))
 AND u.status='active' AND u.deleted_at IS NULL AND ((r.api_key_id IS NULL AND u.role='admin') OR (k.status='active' AND k.deleted_at IS NULL
 AND (k.expires_at IS NULL OR k.expires_at>clock_timestamp())
 AND gr.status='active' AND gr.deleted_at IS NULL AND (gr.subscription_type='subscription' OR (NOT gr.is_exclusive AND NOT u.restrict_public_groups) OR EXISTS(SELECT 1 FROM user_allowed_groups ag WHERE ag.user_id=u.id AND ag.group_id=gr.id))
 AND EXISTS(SELECT 1 FROM account_groups g WHERE g.account_id=i.account_id AND g.group_id=k.group_id))),r.model,COALESCE(gr.model_allowlist,'{}'::jsonb)
 FROM request_leases l JOIN upstream_principals p ON p.id=l.principal_id JOIN credential_instances i ON i.id=l.instance_id
 JOIN logical_requests r ON r.id=l.request_id LEFT JOIN api_keys k ON k.id=r.api_key_id JOIN users u ON u.id=r.user_id LEFT JOIN groups gr ON gr.id=k.group_id WHERE l.id=$1`, ref.ID).Scan(&allowed, &requestModel, &currentAllowlist)
	if err != nil {
		return err
	}
	var allowlist service.GroupModelAllowlist
	if json.Unmarshal(currentAllowlist, &allowlist) != nil || !allowlist.Allows(requestModel) {
		return service.ErrAdmissionOwnership
	}
	if !allowed {
		return service.ErrAdmissionOwnership
	}
	_, err = tx.ExecContext(ctx, `UPDATE request_leases SET state='DISPATCHING',heartbeat_at=clock_timestamp() WHERE id=$1`, ref.ID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE session_bindings SET last_used_at=clock_timestamp(),idle_expires_at=clock_timestamp()+INTERVAL '24 hours' WHERE id=(SELECT binding_id FROM request_leases WHERE id=$1)`, ref.ID)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *principalAdmissionStore) Heartbeat(ctx context.Context, ref service.LeaseRef) error {
	tx, state, err := s.lockLease(ctx, ref)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if state == "RELEASED" || state == "ORPHANED" {
		return service.ErrAdmissionOwnership
	}
	result, err := tx.ExecContext(ctx, `UPDATE request_leases SET heartbeat_at=clock_timestamp() WHERE id=$1 AND epoch=(SELECT admission_epoch FROM upstream_principals WHERE id=$2)`, ref.ID, ref.PrincipalID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return service.ErrAdmissionOwnership
	}
	return tx.Commit()
}
func (s *principalAdmissionStore) Finish(ctx context.Context, in service.FinishAdmissionInput) error {
	defer s.signalQueue()
	tx, state, err := s.lockLease(ctx, in.Lease)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if state == "RELEASED" {
		return nil
	}
	if !in.Complete && state != "RESERVED" {
		_, err = tx.ExecContext(ctx, `UPDATE request_leases SET state='ORPHANED',outcome='UNKNOWN' WHERE id=$1`, in.Lease.ID)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE logical_requests SET status='UNKNOWN' WHERE id=$1`, in.Lease.RequestID)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO credential_usage_events(event_id,lease_id,outcome,usage_state) VALUES($1,$2,'UNKNOWN','UNKNOWN') ON CONFLICT(lease_id) DO NOTHING`, uuid.NewString(), in.Lease.ID)
		if err != nil {
			return err
		}
		if err = admissionAudit(ctx, tx, in.Lease, "LEASE_ORPHANED"); err != nil {
			return err
		}
		return tx.Commit()
	}
	outcome := in.Outcome
	if state == "RESERVED" {
		outcome = "NOT_SENT"
	}
	if outcome != "NOT_SENT" && outcome != "COMPLETED" && outcome != "FAILED" && outcome != "CANCELLED" {
		return fmt.Errorf("invalid terminal outcome")
	}
	if outcome == "COMPLETED" {
		_, err = tx.ExecContext(ctx, `UPDATE upstream_principals SET protected_until=NULL WHERE id=$1 AND protected_until<=clock_timestamp()`, in.Lease.PrincipalID)
		if err != nil {
			return err
		}
	}
	if err = releaseAdmission(ctx, tx, in.Lease, outcome); err != nil {
		return err
	}
	usageState := "UNKNOWN"
	if in.InputTokens != nil && in.OutputTokens != nil {
		usageState = "KNOWN"
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO credential_usage_events(event_id,lease_id,outcome,usage_state,input_tokens,output_tokens,upstream_request_id) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(lease_id) DO UPDATE SET outcome=EXCLUDED.outcome,usage_state=EXCLUDED.usage_state,input_tokens=EXCLUDED.input_tokens,output_tokens=EXCLUDED.output_tokens,upstream_request_id=EXCLUDED.upstream_request_id WHERE credential_usage_events.settled_at IS NULL`, uuid.NewString(), in.Lease.ID, outcome, usageState, in.InputTokens, in.OutputTokens, nullableString(in.UpstreamRequestID))
	if err != nil {
		return err
	}
	return tx.Commit()
}
func releaseAdmission(ctx context.Context, tx *sql.Tx, ref service.LeaseRef, outcome string) error {
	// lockLease holds user, principal, instance and lease locks before this
	// statement. A failed write rolls back all counters and the terminal state.
	_, err := tx.ExecContext(ctx, `WITH user_capacity AS (
 UPDATE principal_user_capacity SET occupied=occupied-1 WHERE user_id=$1 AND principal_id=$2
), principal_capacity AS (
 UPDATE upstream_principals SET occupied=occupied-1 WHERE id=$2
), instance_capacity AS (
 UPDATE credential_instances SET occupied=occupied-1 WHERE id=$3
), request_state AS (
 UPDATE logical_requests SET status=CASE WHEN $5='COMPLETED' THEN 'COMPLETED' ELSE 'FAILED' END WHERE id=$6
)
 UPDATE request_leases SET state='RELEASED',outcome=$5,released_at=CURRENT_TIMESTAMP WHERE id=$4`, ref.UserID, ref.PrincipalID, ref.InstanceID, ref.ID, outcome, ref.RequestID)
	if err != nil {
		return err
	}

	return admissionAudit(ctx, tx, ref, "LEASE_RELEASED")
}
func (s *principalAdmissionStore) Cancel(ctx context.Context, ref service.LeaseRef) error {
	defer s.signalQueue()
	tx, state, err := s.lockLease(ctx, ref)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if state == "RELEASED" {
		return nil
	}
	if state == "RESERVED" {
		err = releaseAdmission(ctx, tx, ref, "NOT_SENT")
	} else if state != "ORPHANED" {
		_, err = tx.ExecContext(ctx, `UPDATE request_leases SET state='CANCELLING' WHERE id=$1`, ref.ID)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

// RecoverReserved reads the durable result of an uncertain acquire commit with
// the same request identity and owner. It never creates a second attempt.
func (s *principalAdmissionStore) RecoverReserved(ctx context.Context, in service.AdmissionInput) (*service.CredentialExecutionSnapshot, error) {
	var snap service.CredentialExecutionSnapshot
	err := s.criticalDB().QueryRowContext(ctx, `SELECT l.id,l.request_id,l.principal_id,l.instance_id,l.generation,l.user_id,l.owner_nonce,l.epoch,
 i.account_id,l.credential_version,l.config_version,p.installation_id,p.source,s.secret_ciphertext,s.secret_aad,l.proxy_id,r.endpoint,r.caller_scope_hash,l.deadline,l.account_updated_at,l.proxy_updated_at
 FROM request_leases l JOIN logical_requests r ON r.id=l.request_id JOIN credential_instances i ON i.id=l.instance_id
 JOIN credential_identity_profiles p ON p.instance_id=i.id AND p.generation=l.generation
 JOIN credential_secrets s ON s.instance_id=i.id AND s.credential_version=l.credential_version JOIN accounts a ON a.id=i.account_id
 WHERE l.request_id=$1 AND l.owner_nonce=$2 AND r.owner_node=$3 AND r.payload_digest=$4 AND l.state='RESERVED'
 AND r.user_id=$5 AND r.api_key_id IS NOT DISTINCT FROM $6 AND l.principal_id=$7`, in.RequestID, in.OwnerNonce, in.Node, in.PayloadDigest, in.UserID, nullablePositive(in.APIKeyID), in.PrincipalID).Scan(
		&snap.Lease.ID, &snap.Lease.RequestID, &snap.Lease.PrincipalID, &snap.Lease.InstanceID, &snap.Lease.Generation, &snap.Lease.UserID, &snap.Lease.OwnerNonce, &snap.Lease.Epoch,
		&snap.AccountID, &snap.CredentialVersion, &snap.ConfigVersion, &snap.InstallationID, &snap.IdentitySource, &snap.SecretCiphertext, &snap.SecretAAD, &snap.ProxyID, &snap.Endpoint, &snap.CallerScope, &snap.Deadline, &snap.AccountUpdatedAt, &snap.ProxyUpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAdmissionOwnership
	}
	if err != nil {
		return nil, service.ErrAdmissionStoreUnavailable
	}
	if err = loadCredentialSnapshotProxy(ctx, s.criticalDB(), &snap); err != nil {
		return nil, service.ErrAdmissionOwnership
	}
	return &snap, nil
}

func loadCredentialSnapshotProxy(ctx context.Context, db interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, snap *service.CredentialExecutionSnapshot) error {
	if snap.ProxyID == nil {
		return nil
	}
	p := &service.Proxy{ID: *snap.ProxyID}
	err := db.QueryRowContext(ctx, `SELECT name,protocol,host,port,COALESCE(username,''),COALESCE(password,''),status,updated_at FROM proxies WHERE id=$1 AND deleted_at IS NULL AND status='active'`, p.ID).Scan(&p.Name, &p.Protocol, &p.Host, &p.Port, &p.Username, &p.Password, &p.Status, &p.UpdatedAt)
	if err != nil {
		return err
	}
	if snap.ProxyUpdatedAt != nil && !snap.ProxyUpdatedAt.Equal(p.UpdatedAt) {
		return service.ErrAdmissionOwnership
	}
	snap.ProxyUpdatedAt = &p.UpdatedAt
	snap.Proxy = p
	return nil
}

func nullableAdmissionTime(v time.Time) any {
	if v.IsZero() {
		return nil
	}
	return v
}
