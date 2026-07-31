package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRewardCampaignMigrationKeepsFinancialAndVersionInvariants(t *testing.T) {
	content, err := FS.ReadFile("193_reward_campaigns.sql")
	require.NoError(t, err)
	sql := string(content)

	require.Contains(t, sql, "reserved_budget + spent_budget <= total_budget")
	require.Contains(t, sql, "UNIQUE (campaign_id, user_id, cycle_key)")
	require.Contains(t, sql, "UNIQUE INDEX IF NOT EXISTS idx_user_reward_grants_claim_record")
	require.Contains(t, sql, "reward campaign versions are immutable")
	require.Contains(t, sql, "terminal reward grant status cannot be changed")
	require.Contains(t, sql, "reward grants are financial records and cannot be deleted")
	require.Contains(t, sql, "FOREIGN KEY (campaign_id, campaign_version_id)")
	require.Contains(t, sql, "REFERENCES reward_campaign_versions(campaign_id, id)")
}

func TestRewardCampaignMigrationSeedsCompatibleSystemCampaigns(t *testing.T) {
	content, err := FS.ReadFile("193_reward_campaigns.sql")
	require.NoError(t, err)
	sql := string(content)

	require.Contains(t, sql, "'system_welcome'")
	require.Contains(t, sql, "'system_surprise'")
	require.Contains(t, sql, `"copy_i18n"`)
	require.Contains(t, sql, `"relative_days": -7`)
	require.Contains(t, sql, "ENCODE(SHA256(CONVERT_TO(seeded.config::TEXT, 'UTF8')), 'hex')")
	require.Contains(t, sql, "JSONB_INSERT(")
	require.Contains(t, sql, "'legacy:welcome'")
	require.Contains(t, sql, "'legacy:surprise'")
	require.Contains(t, sql, "RETURNING campaign_id, amount")
	require.Contains(t, sql, "reserved_budget = c.reserved_budget + totals.amount")
	require.Contains(t, sql, "SET welcome_reward_amount = 0")
	require.Contains(t, sql, "SET surprise_reward_amount = 0")
}

func TestRewardCampaignMigrationUsesStableUTCHourBuckets(t *testing.T) {
	content, err := FS.ReadFile("193_reward_campaigns.sql")
	require.NoError(t, err)
	sql := string(content)

	require.Contains(t, sql, "bucket_start AT TIME ZONE 'UTC'")
	require.Contains(t, sql, "UNIQUE (user_id, bucket_start)")
	require.Contains(t, sql, "idx_reward_campaign_jobs_due")
	require.Contains(t, sql, "lease_expires_at")
	require.Contains(t, sql, "AFTER INSERT ON usage_logs")
	require.Contains(t, sql, "NEW.source IS DISTINCT FROM 'gateway'")
	require.Contains(t, sql, "NEW.source = 'gateway'")
	require.Contains(t, sql, "NEW.api_key_id IS NULL")
	require.Contains(t, sql, "created_at >= NOW() - INTERVAL '30 days'")
	require.Contains(t, sql, "WITH latest_usage AS")
	require.Contains(t, sql, "ORDER BY user_id, created_at DESC")
	require.Contains(t, sql, "source = 'gateway'")
	require.Contains(t, sql, "recharge_amount = EXCLUDED.recharge_amount")
	require.Contains(t, sql, "ON CONFLICT (user_id, bucket_start) DO UPDATE")
	require.Contains(t, sql, "user_behavior_daily.request_count + EXCLUDED.request_count")
	require.Contains(t, sql, "user_behavior_daily.actual_cost + EXCLUDED.actual_cost")
	require.Contains(t, sql, "AFTER UPDATE OF status ON payment_orders")
	require.Contains(t, sql, "OLD.status IS DISTINCT FROM NEW.status")
	require.Contains(t, sql, "NEW.order_type <> 'balance'")
	require.Contains(t, sql, "user_behavior_daily.recharge_amount + EXCLUDED.recharge_amount")
}
