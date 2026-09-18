//go:build integration

package repository

import (
	"context"
	"database/sql"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func reservedAdmissionStore(t *testing.T) *principalAdmissionStore {
	t.Helper()
	open := func(limit int) *sql.DB {
		db, err := openSQLWithRetry(context.Background(), integrationDSN, 5*time.Second)
		require.NoError(t, err)
		db.SetMaxOpenConns(limit)
		db.SetMaxIdleConns(limit)
		t.Cleanup(func() { _ = db.Close() })
		return db
	}
	return &principalAdmissionStore{db: open(6), lifecycleDB: open(2)}
}

func TestCredentialLifecycleResourcesSurviveAdmissionPoolExhaustion(t *testing.T) {
	f := newAdmissionFixture(t, 4)
	s := reservedAdmissionStore(t)
	ctx := context.Background()
	d, err := s.TryAdmit(ctx, f.input())
	require.NoError(t, err)
	require.NoError(t, s.BeginDispatch(ctx, d.Snapshot.Lease))
	var held []*sql.Conn
	for range 6 {
		c, err := s.db.Conn(ctx)
		require.NoError(t, err)
		held = append(held, c)
	}
	defer func() {
		for _, c := range held {
			_ = c.Close()
		}
	}()
	deadline, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	require.NoError(t, s.Heartbeat(deadline, d.Snapshot.Lease))
	receipt := service.CredentialUsageReceipt{Lease: d.Snapshot.Lease, LeaseID: d.Snapshot.Lease.ID, UserID: f.user, APIKeyID: f.key, AccountID: d.Snapshot.AccountID, Complete: true, Outcome: "COMPLETED", Result: service.CredentialUsageResult{Usage: service.OpenAIUsage{InputTokens: 1, OutputTokens: 1}}}
	require.NoError(t, s.SaveCredentialUsageReceipt(deadline, receipt))
	finish, end := context.WithTimeout(ctx, 5*time.Second)
	defer end()
	require.NoError(t, s.Finish(finish, service.FinishAdmissionInput{Lease: d.Snapshot.Lease, Complete: true, Outcome: "COMPLETED"}))
	assertAdmissionLedger(t, f, 0)
	require.Equal(t, 8, s.db.Stats().MaxOpenConnections+s.lifecycleDB.Stats().MaxOpenConnections)
}

func TestCredentialLifecycleResourcesThreeNodeAdmissionPressure(t *testing.T) {
	f := newAdmissionFixture(t, 100)
	ctx := context.Background()
	nodes := []*principalAdmissionStore{reservedAdmissionStore(t), reservedAdmissionStore(t), reservedAdmissionStore(t)}
	occupied, err := nodes[0].TryAdmit(ctx, f.input())
	require.NoError(t, err)
	require.NoError(t, nodes[0].BeginDispatch(ctx, occupied.Snapshot.Lease))
	pressure, stop := context.WithCancel(ctx)
	defer stop()
	var wg sync.WaitGroup
	for i := range 90 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := nodes[i%3]
			for pressure.Err() == nil {
				in := f.input()
				d, err := s.TryAdmit(pressure, in)
				if err != nil {
					return
				}
				if d.Code == service.AdmissionAdmitted {
					_ = s.Cancel(context.Background(), d.Snapshot.Lease)
				} else if d.Code == service.AdmissionWait {
					_ = s.CancelQueued(context.Background(), in)
				}
			}
		}(i)
	}
	var maxHeartbeat time.Duration
	for range 12 {
		hb, end := context.WithTimeout(ctx, 3*time.Second)
		started := time.Now()
		err := nodes[0].Heartbeat(hb, occupied.Snapshot.Lease)
		end()
		require.NoError(t, err)
		maxHeartbeat = max(maxHeartbeat, time.Since(started))
	}
	finish, end := context.WithTimeout(ctx, 5*time.Second)
	started := time.Now()
	require.NoError(t, nodes[0].Finish(finish, service.FinishAdmissionInput{Lease: occupied.Snapshot.Lease, Complete: true, Outcome: "COMPLETED"}))
	end()
	t.Logf("max_heartbeat=%s finish=%s admission_nodes=3 contenders=90 total_connection_budget=24", maxHeartbeat, time.Since(started))
	stop()
	wg.Wait()
	assertAdmissionLedger(t, f, 0)
}

func TestCredentialLifecyclePoolBudgetAndDisabledGate(t *testing.T) {
	db, err := openSQLWithRetry(context.Background(), integrationDSN, time.Second)
	require.NoError(t, err)
	defer db.Close()
	db.SetMaxOpenConns(8)
	cfg := &config.Config{Timezone: "UTC"}
	cfg.Database.MaxOpenConns = 8
	cfg.Database.MaxIdleConns = 8
	disabled, err := ProvidePrincipalAdmissionStore(db, cfg)
	require.NoError(t, err)
	require.Nil(t, disabled.(*principalAdmissionStore).lifecycleDB)
	require.Equal(t, 8, db.Stats().MaxOpenConnections)
	cfg.Gateway.MultiCredentialHTTPEnabled = true
	cfg.Database.MaxOpenConns = 3
	_, err = ProvidePrincipalAdmissionStore(db, cfg)
	require.ErrorContains(t, err, "at least four")
	parsed, err := url.Parse(integrationDSN)
	require.NoError(t, err)
	cfg.Database.MaxOpenConns = 8
	cfg.Database.Host = parsed.Hostname()
	cfg.Database.Port, err = strconv.Atoi(parsed.Port())
	require.NoError(t, err)
	cfg.Database.User = parsed.User.Username()
	cfg.Database.Password, _ = parsed.User.Password()
	cfg.Database.DBName = strings.TrimPrefix(parsed.Path, "/")
	cfg.Database.SSLMode = "disable"
	enabled, err := ProvidePrincipalAdmissionStore(db, cfg)
	require.NoError(t, err)
	store := enabled.(*principalAdmissionStore)
	defer store.Close()
	require.Equal(t, 6, db.Stats().MaxOpenConnections)
	require.Equal(t, 2, store.criticalDB().Stats().MaxOpenConnections)
}
