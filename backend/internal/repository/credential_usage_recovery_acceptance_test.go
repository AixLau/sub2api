//go:build integration

package repository

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type acceptanceUsageCrash struct {
	service.PrincipalAdmissionStore
	service.CredentialUsageReceiptStore
	window string
}

func (s acceptanceUsageCrash) SaveCredentialUsageReceipt(ctx context.Context, in service.CredentialUsageReceipt) error {
	if s.window == "before_receipt_commit" {
		panic("acceptance crash before receipt commit")
	}
	if err := s.CredentialUsageReceiptStore.SaveCredentialUsageReceipt(ctx, in); err != nil {
		return err
	}
	if s.window == "after_receipt_commit" {
		panic("acceptance crash after receipt commit")
	}
	return nil
}
func (s acceptanceUsageCrash) Finish(ctx context.Context, in service.FinishAdmissionInput) error {
	if err := s.PrincipalAdmissionStore.Finish(ctx, in); err != nil {
		return err
	}
	if s.window == "after_finish_commit" {
		panic("acceptance crash after finish commit")
	}
	return nil
}

func TestCredentialUsageCrashWindowsRecoverWithoutReplay(t *testing.T) {
	for _, window := range []string{"before_receipt_commit", "after_receipt_commit", "after_finish_commit", "after_priced_command", "after_settlement_before_usage_log", "after_usage_log_before_ack"} {
		t.Run(window, func(t *testing.T) {
			f := newAdmissionFixture(t, 10)
			prepareAcceptanceIdentity(t, f, f.user)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var starts atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { starts.Add(1); acceptanceTerminal(w) }))
			defer upstream.Close()
			real := &principalAdmissionStore{db: integrationDB}
			store := acceptanceUsageCrash{PrincipalAdmissionStore: real, CredentialUsageReceiptStore: real, window: window}
			// PostgreSQL failpoints are restricted to synthetic rows of this test and
			// emulate rollback at the selected transaction boundary, not upstream replay.
			trigger := ""
			var target string
			switch window {
			case "after_priced_command":
				target = "users"
				trigger = fmt.Sprintf(`IF NEW.id=%d AND NEW.balance<OLD.balance THEN RAISE EXCEPTION 'acceptance settlement interrupted'; END IF;`, f.user)
			case "after_settlement_before_usage_log":
				target = "usage_logs"
				trigger = fmt.Sprintf(`IF NEW.user_id=%d THEN RAISE EXCEPTION 'acceptance usage row interrupted'; END IF;`, f.user)
			case "after_usage_log_before_ack":
				target = "credential_usage_receipts"
				trigger = fmt.Sprintf(`IF (NEW.receipt->>'UserID')::bigint=%d AND NEW.state='RECORDED' THEN RAISE EXCEPTION 'acceptance ack interrupted'; END IF;`, f.user)
			}
			if trigger != "" {
				_, err := integrationDB.Exec(`CREATE OR REPLACE FUNCTION acceptance_usage_crash() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN ` + trigger + ` RETURN NEW; END $$; CREATE TRIGGER acceptance_usage_crash BEFORE INSERT OR UPDATE ON ` + target + ` FOR EACH ROW EXECUTE FUNCTION acceptance_usage_crash()`)
				require.NoError(t, err)
				t.Cleanup(func() { _, _ = integrationDB.Exec(`DROP TRIGGER IF EXISTS acceptance_usage_crash ON ` + target) })
			}
			gateway := acceptanceGateway(t, &acceptanceUpstream{url: upstream.URL}, store)
			_, _, err := acceptanceRequest(ctx, gateway.URL+"/v1/responses", acceptanceKey(t, f.key), "crash-window")
			require.NoError(t, err)
			require.Equal(t, int64(1), starts.Load())
			if trigger != "" {
				_, err = integrationDB.Exec(`DROP TRIGGER acceptance_usage_crash ON ` + target)
				require.NoError(t, err)
			}
			var receiptCount int
			require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM credential_usage_receipts c JOIN request_leases l ON l.id=c.lease_id WHERE l.principal_id=$1`, f.principal).Scan(&receiptCount))
			if window == "before_receipt_commit" {
				require.Zero(t, receiptCount)
				_, err = integrationDB.Exec(`UPDATE request_leases SET heartbeat_at=CURRENT_TIMESTAMP-INTERVAL '40 seconds' WHERE principal_id=$1`, f.principal)
				require.NoError(t, err)
				_, err = (&credentialOperations{db: integrationDB}).ReconcileCredentialLeases(ctx)
				require.NoError(t, err)
				assertAdmissionLedger(t, f, 1)
				var state string
				require.NoError(t, integrationDB.QueryRow(`SELECT outcome FROM request_leases WHERE principal_id=$1`, f.principal).Scan(&state))
				require.Equal(t, "UNKNOWN", state)
			} else {
				require.Equal(t, 1, receiptCount)
				// New dependencies simulate restart; persisted facts, not the HTTP request,
				// are replayed. Repeat recovery verifies both balance and usage-row dedup.
				restarted := acceptanceGateway(t, &acceptanceUpstream{url: upstream.URL}, real)
				for range 2 {
					require.NoError(t, restarted.gateway.RecoverCredentialUsage(ctx, real, restarted.keys, restarted.keyService))
				}
				assertAdmissionLedger(t, f, 0)
				var receipts, logs, bills int
				require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM credential_usage_receipts c JOIN request_leases l ON l.id=c.lease_id WHERE l.principal_id=$1 AND c.state='RECORDED'`, f.principal).Scan(&receipts))
				require.Equal(t, 1, receipts)
				require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM usage_logs WHERE user_id=$1 AND request_id LIKE 'credential-lease:%'`, f.user).Scan(&logs))
				require.Equal(t, 1, logs)
				require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM usage_billing_dedup WHERE api_key_id=$1 AND request_id LIKE 'credential-lease:%'`, f.key).Scan(&bills))
				require.Equal(t, 1, bills)
				var balance float64
				require.NoError(t, integrationDB.QueryRow(`SELECT balance FROM users WHERE id=$1`, f.user).Scan(&balance))
				require.Less(t, balance, 100.0)
			}
			require.Equal(t, int64(1), starts.Load(), "recovery must never execute the upstream again")
		})
	}
}
