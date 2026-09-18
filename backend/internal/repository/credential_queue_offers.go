package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

const credentialOfferBatch = 8

// Called under the principal row lock (a subset of the global lock order).
// Hints are serialized but not capacity reservations. Ready windows expire,
// retain original ticket age, and put an unresponsive owner behind one bounded
// retry delay. The request body never leaves its original owner.
func advanceCredentialOffers(ctx context.Context, tx *sql.Tx, principal int64) error {
	_, err := tx.ExecContext(ctx, `UPDATE admission_tickets SET offer_until=NULL,offer_instance_id=NULL,ready_at=clock_timestamp()+INTERVAL '1 second'
 WHERE principal_id=$1 AND state='QUEUED' AND offer_until<=clock_timestamp()`, principal)
	if err != nil {
		return err
	}
	var capacity, occupied int
	var userLimit sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT requested_limit,occupied,user_concurrency_limit FROM upstream_principals
 WHERE id=$1 AND routing_mode='GROUPED' AND verification_state='VERIFIED' AND admin_state IN ('ACTIVE','DRAINING')
 AND (protected_until IS NULL OR (protected_until<=clock_timestamp() AND occupied=0))
 AND NOT EXISTS(SELECT 1 FROM upstream_principal_quota_domains p JOIN upstream_quota_domains q ON q.id=p.quota_domain_id WHERE p.principal_id=$1 AND (q.requires_admin_reset OR q.blocked_until>clock_timestamp()))`, principal).Scan(&capacity, &occupied, &userLimit)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if occupied >= capacity {
		return nil
	}
	type instance struct {
		id                                int64
		weight                            float64
		hard, occupied, projected, demand int
	}
	instances := map[int64]*instance{}
	rows, err := tx.QueryContext(ctx, `SELECT id,weight,LEAST($2,COALESCE(hard_max,$2),COALESCE(health_capacity,$2)),occupied FROM credential_instances
 WHERE principal_id=$1 AND admin_state IN ('ACTIVE','DRAINING') AND credential_state='VALID'
 AND transport_state IN ('HEALTHY','DEGRADED','HALF_OPEN') AND (cooldown_until IS NULL OR cooldown_until<=clock_timestamp())`, principal, capacity)
	if err != nil {
		return err
	}
	for rows.Next() {
		i := &instance{}
		if err = rows.Scan(&i.id, &i.weight, &i.hard, &i.occupied); err != nil {
			rows.Close()
			return err
		}
		i.projected = i.occupied
		i.demand = i.occupied
		instances[i.id] = i
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	type ticket struct {
		id              string
		user            int64
		bound           sql.NullInt64
		ids             []int64
		created         time.Time
		last            sql.NullTime
		occupied        int
		offered         sql.NullTime
		offeredInstance sql.NullInt64
		ready           bool
	}
	var tickets []*ticket
	// Recheck current scope and binding for hints too; final admission repeats all
	// checks and detects concurrent revocation/configuration changes.
	rows, err = tx.QueryContext(ctx, `SELECT t.request_id,t.user_id,t.instance_id,t.created_at,c.last_admitted_at,c.occupied,t.offer_until,t.offer_instance_id,t.ready_at<=clock_timestamp(),
 ARRAY(SELECT i.id FROM credential_instances i JOIN accounts a ON a.id=i.account_id JOIN credential_secrets cs ON cs.instance_id=i.id AND cs.credential_version=i.credential_version
 WHERE i.principal_id=t.principal_id AND i.id=ANY(t.candidate_ids) AND a.deleted_at IS NULL AND r.endpoint=ANY(i.capabilities)
 AND (t.instance_id IS NULL OR t.instance_id=i.id) AND (b.id IS NULL OR (b.state IN ('ACTIVE','DRAINING') AND b.generation=i.identity_generation))
 AND (p.admin_state='ACTIVE' OR (b.id IS NOT NULL AND p.drain_deadline>clock_timestamp()))
 AND (i.admin_state='ACTIVE' OR (i.admin_state='DRAINING' AND b.id IS NOT NULL AND i.drain_deadline>clock_timestamp()))
 AND i.credential_state='VALID' AND cs.expires_at>clock_timestamp()+INTERVAL '30 seconds'
 AND i.transport_state IN ('HEALTHY','DEGRADED','HALF_OPEN') AND (i.cooldown_until IS NULL OR i.cooldown_until<=clock_timestamp())
 AND (r.api_key_id IS NULL OR EXISTS(SELECT 1 FROM account_groups g WHERE g.account_id=i.account_id AND g.group_id=k.group_id)) ORDER BY i.id),r.model,COALESCE(g.model_allowlist,'{}'::jsonb)
 FROM admission_tickets t JOIN logical_requests r ON r.id=t.request_id JOIN upstream_principals p ON p.id=t.principal_id
 JOIN principal_user_capacity c ON c.principal_id=t.principal_id AND c.user_id=t.user_id JOIN users u ON u.id=t.user_id
 LEFT JOIN api_keys k ON k.id=r.api_key_id LEFT JOIN groups g ON g.id=k.group_id LEFT JOIN session_bindings b ON b.id=t.binding_id
 WHERE t.principal_id=$1 AND t.state='QUEUED' AND r.status='QUEUED' AND t.deadline>clock_timestamp()
 AND u.status='active' AND u.deleted_at IS NULL
 AND ((r.api_key_id IS NULL AND u.role='admin') OR (k.user_id=u.id AND k.status='active' AND k.deleted_at IS NULL AND (k.expires_at IS NULL OR k.expires_at>clock_timestamp())
 AND g.status='active' AND g.deleted_at IS NULL AND (g.subscription_type='subscription' OR (NOT g.is_exclusive AND NOT u.restrict_public_groups) OR EXISTS(SELECT 1 FROM user_allowed_groups ag WHERE ag.user_id=u.id AND ag.group_id=g.id))))
 ORDER BY c.last_admitted_at NULLS FIRST,t.created_at,t.id`, principal)
	if err != nil {
		return err
	}
	for rows.Next() {
		t := &ticket{}
		var model string
		var allow []byte
		if err = rows.Scan(&t.id, &t.user, &t.bound, &t.created, &t.last, &t.occupied, &t.offered, &t.offeredInstance, &t.ready, pq.Array(&t.ids), &model, &allow); err != nil {
			rows.Close()
			return err
		}
		var models service.GroupModelAllowlist
		if json.Unmarshal(allow, &models) != nil || !models.Allows(model) {
			continue
		}
		tickets = append(tickets, t)
		if t.bound.Valid && t.ready {
			if i := instances[t.bound.Int64]; i != nil {
				i.demand++
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	demands := make([]service.CredentialDemand, 0, len(instances))
	for _, i := range instances {
		demands = append(demands, service.CredentialDemand{ID: i.id, Weight: i.weight, Demand: float64(i.demand), HardCapacity: float64(i.hard)})
	}
	targets := service.CredentialTargets(float64(capacity), demands)
	under := func(t *ticket) bool {
		i := instances[t.bound.Int64]
		return t.bound.Valid && i != nil && float64(i.projected) < targets[i.id]
	}
	sort.SliceStable(tickets, func(a, b int) bool { return under(tickets[a]) && !under(tickets[b]) })
	pending := 0
	userPending := map[int64]int{}
	for _, t := range tickets {
		if t.offered.Valid {
			pending++
			userPending[t.user]++
			if i := instances[t.offeredInstance.Int64]; i != nil {
				i.projected++
			}
		}
	}
	available := min(capacity-occupied-pending, credentialOfferBatch-pending)
	if available <= 0 {
		return nil
	}
	var ids []string
	var chosen []int64
	selectedUsers := map[int64]bool{}
	// Round-robin within a batch before granting a second hint to the same user.
	for available > 0 {
		progressed := false
		for _, t := range tickets {
			if t.offered.Valid || !t.ready || selectedUsers[t.user] || (userLimit.Valid && t.occupied+userPending[t.user] >= int(userLimit.Int64)) {
				continue
			}
			var best *instance
			for _, id := range t.ids {
				i := instances[id]
				if i == nil || i.projected >= i.hard {
					continue
				}
				if best == nil || float64(i.projected+1)/i.weight < float64(best.projected+1)/best.weight {
					best = i
				}
			}
			if best == nil {
				continue
			}
			ids = append(ids, t.id)
			chosen = append(chosen, best.id)
			best.projected++
			userPending[t.user]++
			selectedUsers[t.user] = true
			t.offered.Valid = true
			available--
			progressed = true
			if available == 0 {
				break
			}
		}
		if !progressed {
			if len(selectedUsers) == 0 {
				break
			}
			selectedUsers = map[int64]bool{}
			continue
		}
		selectedUsers = map[int64]bool{}
	}
	if len(ids) == 0 {
		return nil
	}
	_, err = tx.ExecContext(ctx, `UPDATE admission_tickets t SET offer_until=LEAST(t.deadline,clock_timestamp()+INTERVAL '1 second'),offer_instance_id=o.instance
 FROM unnest($1::uuid[],$2::bigint[]) AS o(request,instance) WHERE t.request_id=o.request AND t.state='QUEUED'`, pq.Array(ids), pq.Array(chosen))
	return err
}
