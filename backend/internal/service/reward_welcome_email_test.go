package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type welcomeEmailRewardRepo struct {
	legacyRewardReplayRepoStub
	evaluated []string
	pending   []RewardGrant
}

func (r *welcomeEmailRewardRepo) ListRuntimeCampaigns(context.Context, string, time.Time) ([]RewardCampaign, error) {
	return []RewardCampaign{
		{ID: 1, CampaignKey: RewardSystemCampaignWelcome},
		{ID: 2, CampaignKey: RewardSystemCampaignSurprise},
	}, nil
}

func (r *welcomeEmailRewardRepo) EvaluateAndMaybeGrant(_ context.Context, campaign RewardCampaign, _ int64, _ time.Time, _, _ bool, _ string, _ *int64) (*RewardGrant, bool, error) {
	r.evaluated = append(r.evaluated, campaign.CampaignKey)
	return nil, false, nil
}

func (r *welcomeEmailRewardRepo) ListPending(context.Context, int64, time.Time) ([]RewardGrant, error) {
	return append([]RewardGrant(nil), r.pending...), nil
}

func TestPendingWelcomeRewardEmailPolicy(t *testing.T) {
	for _, tc := range []struct {
		name  string
		email string
		want  []string
	}{
		{"Gmail dot", "first.last@gmail.com", []string{RewardSystemCampaignSurprise}},
		{"custom domain dot", "first.last@example.org", []string{RewardSystemCampaignSurprise}},
		{"domain dots only", "firstlast@mail.example.org", []string{RewardSystemCampaignWelcome, RewardSystemCampaignSurprise}},
		{"existing plus alias policy", "firstlast+tag@gmail.com", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &welcomeEmailRewardRepo{pending: []RewardGrant{
				{CampaignKey: RewardSystemCampaignWelcome}, {CampaignKey: RewardSystemCampaignSurprise},
			}}
			repo.profile.Email = tc.email
			grants, err := NewRewardService(repo, nil, nil).Pending(context.Background(), 1)
			require.NoError(t, err)
			require.Equal(t, tc.want, repo.evaluated)
			var keys []string
			for _, grant := range grants {
				keys = append(keys, grant.CampaignKey)
			}
			require.Equal(t, tc.want, keys)
		})
	}
}

func TestLegacyPendingSkipsDottedWelcomeEmail(t *testing.T) {
	repo := &welcomeEmailRewardRepo{}
	repo.profile.Email = "first.last@gmail.com"
	pending, err := NewRewardService(repo, nil, nil).LegacyPending(context.Background(), 1, RewardSystemCampaignWelcome)
	require.NoError(t, err)
	require.False(t, pending)
	require.Empty(t, repo.evaluated)
}
