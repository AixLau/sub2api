package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const credentialRedisEpochKey = "concurrency:credential:epoch"

var errCredentialRedisEpoch = errors.New("CREDENTIAL_REDIS_STATE_LOST")

type credentialGlobalUserSlots struct {
	epochTTL time.Duration
	db       *sql.DB
	redis    *redis.Client
	cache    service.ConcurrencyCache
	epoch    string
}

func newCredentialGlobalUserSlots(db *sql.DB, rdb *redis.Client, cache service.ConcurrencyCache) (*credentialGlobalUserSlots, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	lifetime := time.Minute
	if c, ok := cache.(*concurrencyCache); ok {
		lifetime = time.Duration(c.slotTTLSeconds/2) * time.Second
		if lifetime > 2*time.Minute {
			lifetime = 2 * time.Minute
		}
	}
	policy, err := rdb.ConfigGet(ctx, "maxmemory-policy").Result()
	if err != nil || policy["maxmemory-policy"] != "noeviction" {
		return nil, errors.New("grouped global user slots require Redis noeviction")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(9182027)`)
	if err != nil {
		return nil, err
	}
	var epoch string
	err = tx.QueryRowContext(ctx, `SELECT epoch FROM credential_redis_epoch WHERE singleton=true FOR UPDATE`).Scan(&epoch)
	if errors.Is(err, sql.ErrNoRows) {
		var occupied int
		err = tx.QueryRowContext(ctx, `SELECT count(*) FROM request_leases WHERE state<>'RELEASED'`).Scan(&occupied)
		if err != nil {
			return nil, err
		}
		if occupied != 0 {
			return nil, errCredentialRedisEpoch
		}
		epoch = uuid.NewString()
		_, err = tx.ExecContext(ctx, `INSERT INTO credential_redis_epoch(singleton,epoch) VALUES(true,$1)`, epoch)
		if err != nil {
			return nil, err
		}
		if err = rdb.Set(ctx, credentialRedisEpochKey, epoch, lifetime).Err(); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	actual, err := rdb.Get(ctx, credentialRedisEpochKey).Result()
	if err != nil || actual != epoch {
		return nil, errCredentialRedisEpoch
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &credentialGlobalUserSlots{db: db, redis: rdb, cache: cache, epoch: epoch, epochTTL: lifetime}, nil
}
func (g *credentialGlobalUserSlots) check(ctx context.Context) error {
	value, err := g.redis.Get(ctx, credentialRedisEpochKey).Result()
	if err != nil || value != g.epoch {
		return errCredentialRedisEpoch
	}
	return nil
}
func (g *credentialGlobalUserSlots) Acquire(ctx context.Context, ref service.LeaseRef, limit int) (bool, error) {
	if err := g.check(ctx); err != nil {
		return false, err
	}
	slot := "credential:" + ref.ID
	store := &principalAdmissionStore{db: g.db}
	tx, state, err := store.lockLease(ctx, ref)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if state != "RESERVED" {
		return false, service.ErrAdmissionOwnership
	}
	_, err = tx.ExecContext(ctx, `UPDATE request_leases SET global_user_slot=$2 WHERE id=$1`, ref.ID, slot)
	if err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	// Use the very same Redis counter and script as all legacy paths. No second
	// independent global capacity approval is introduced.
	cap := limit
	if cap <= 0 {
		cap = int(^uint(0) >> 1)
	}
	acquired, err := g.cache.AcquireUserSlot(ctx, ref.UserID, cap, slot)
	if err != nil || !acquired {
		return acquired, err
	}
	result, err := g.db.ExecContext(ctx, `UPDATE request_leases SET global_user_acquired=true WHERE id=$1 AND state='RESERVED' AND owner_nonce=$2`, ref.ID, ref.OwnerNonce)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if n != 1 {
		return false, service.ErrAdmissionOwnership
	}
	return true, nil
}
func (g *credentialGlobalUserSlots) Release(ctx context.Context, ref service.LeaseRef) error {
	var state string
	var slot sql.NullString
	if err := g.db.QueryRowContext(ctx, `SELECT state,global_user_slot FROM request_leases WHERE id=$1 AND user_id=$2 AND owner_nonce=$3`, ref.ID, ref.UserID, ref.OwnerNonce).Scan(&state, &slot); err != nil {
		return err
	}
	if state != "RELEASED" || !slot.Valid {
		return nil
	} // unknown execution still consumes global capacity
	if err := g.cache.ReleaseUserSlot(ctx, ref.UserID, slot.String); err != nil {
		return err
	}
	_, err := g.db.ExecContext(ctx, `UPDATE request_leases SET global_user_acquired=false WHERE id=$1 AND state='RELEASED'`, ref.ID)
	return err
}
func (g *credentialGlobalUserSlots) Reconcile(ctx context.Context) error {
	if err := g.check(ctx); err != nil {
		return err
	}
	rows, err := g.db.QueryContext(ctx, `SELECT id,user_id,owner_nonce,state,global_user_slot FROM request_leases WHERE global_user_acquired ORDER BY created_at`)
	if err != nil {
		return err
	}
	type hold struct {
		ref         service.LeaseRef
		state, slot string
	}
	var holds []hold
	for rows.Next() {
		var h hold
		if err = rows.Scan(&h.ref.ID, &h.ref.UserID, &h.ref.OwnerNonce, &h.state, &h.slot); err != nil {
			rows.Close()
			return err
		}
		holds = append(holds, h)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, h := range holds {
		if h.state == "RELEASED" {
			if err = g.Release(ctx, h.ref); err != nil {
				return err
			}
			continue
		}
		// Renew, never newly approve; these are durably acquired nonterminal holds.
		// A missing marker closes all acquisitions until fenced manual recovery.
		now, err := g.redis.Time(ctx).Result()
		if err != nil {
			return err
		}
		pipe := g.redis.TxPipeline()
		key := userSlotKeyPrefix + strconv.FormatInt(h.ref.UserID, 10)
		pipe.ZAdd(ctx, key, redis.Z{Score: float64(now.Unix()), Member: h.slot})
		pipe.Expire(ctx, key, 30*time.Minute)
		if _, err = pipe.Exec(ctx); err != nil {
			return fmt.Errorf("renew credential user hold: %w", err)
		}
		// Release can race the read above; never leave a renewed ghost member.
		var state string
		if err = g.db.QueryRowContext(ctx, `SELECT state FROM request_leases WHERE id=$1`, h.ref.ID).Scan(&state); err != nil {
			return err
		}
		if state == "RELEASED" {
			if err = g.Release(ctx, h.ref); err != nil {
				return err
			}
		}
	}
	return g.redis.Expire(ctx, credentialRedisEpochKey, g.epochTTL).Err()
}
