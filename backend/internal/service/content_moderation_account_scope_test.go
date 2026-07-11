package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type contentModerationAccountScopeRepoStub struct {
	accounts []*Account
}

func (s contentModerationAccountScopeRepoStub) GetByIDs(context.Context, []int64) ([]*Account, error) {
	return s.accounts, nil
}

func TestContentModerationAccountScopeNormalizeLegacyDefaultsToAll(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.AccountScope = ""
	cfg.AccountIDs = []int64{3, 3, 0, -1, 2}

	cfg.normalize()

	require.Equal(t, ContentModerationAccountScopeAll, cfg.AccountScope)
	require.Empty(t, cfg.AccountIDs)
}

func TestContentModerationAccountScopeNormalizeSelectedIDs(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.AccountScope = ContentModerationAccountScopeSelected
	cfg.AccountIDs = []int64{3, 3, 0, -1, 2}

	cfg.normalize()

	require.Equal(t, []int64{2, 3}, cfg.AccountIDs)
}

func TestContentModerationAccountScopeMatching(t *testing.T) {
	tests := []struct {
		name        string
		scope       string
		accountIDs  []int64
		accountID   int64
		accountType string
		want        bool
	}{
		{name: "all matches oauth", scope: ContentModerationAccountScopeAll, accountID: 1, accountType: AccountTypeOAuth, want: true},
		{name: "all requires selected account", scope: ContentModerationAccountScopeAll, accountID: 0, accountType: AccountTypeOAuth, want: false},
		{name: "oauth matches oauth", scope: ContentModerationAccountScopeOAuth, accountID: 1, accountType: AccountTypeOAuth, want: true},
		{name: "oauth matches setup token", scope: ContentModerationAccountScopeOAuth, accountID: 2, accountType: AccountTypeSetupToken, want: true},
		{name: "oauth excludes api key", scope: ContentModerationAccountScopeOAuth, accountID: 3, accountType: AccountTypeAPIKey, want: false},
		{name: "oauth excludes upstream", scope: ContentModerationAccountScopeOAuth, accountID: 3, accountType: AccountTypeUpstream, want: false},
		{name: "oauth excludes bedrock", scope: ContentModerationAccountScopeOAuth, accountID: 3, accountType: AccountTypeBedrock, want: false},
		{name: "oauth excludes service account", scope: ContentModerationAccountScopeOAuth, accountID: 3, accountType: AccountTypeServiceAccount, want: false},
		{name: "selected matches configured id", scope: ContentModerationAccountScopeSelected, accountIDs: []int64{4, 5}, accountID: 5, accountType: AccountTypeAPIKey, want: true},
		{name: "selected excludes other id", scope: ContentModerationAccountScopeSelected, accountIDs: []int64{4, 5}, accountID: 6, accountType: AccountTypeOAuth, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := defaultContentModerationConfig()
			cfg.AccountScope = tt.scope
			cfg.AccountIDs = tt.accountIDs
			cfg.normalize()

			require.Equal(t, tt.want, cfg.includesAccount(tt.accountID, tt.accountType))
		})
	}
}

func TestContentModerationScopeIntersection(t *testing.T) {
	groupID := int64(7)
	cfg := defaultContentModerationConfig()
	cfg.AllGroups = false
	cfg.GroupIDs = []int64{groupID}
	cfg.AccountScope = ContentModerationAccountScopeSelected
	cfg.AccountIDs = []int64{11}
	cfg.ModelFilter = ContentModerationModelFilter{Type: ContentModerationModelFilterInclude, Models: []string{"gpt-5"}}
	cfg.normalize()

	require.True(t, cfg.includesGroup(&groupID) && cfg.includesAccount(11, AccountTypeOAuth) && cfg.includesModel("gpt-5"))
	require.False(t, cfg.includesGroup(&groupID) && cfg.includesAccount(12, AccountTypeOAuth) && cfg.includesModel("gpt-5"))
	require.False(t, cfg.includesGroup(&groupID) && cfg.includesAccount(11, AccountTypeOAuth) && cfg.includesModel("claude-sonnet"))
}

func TestContentModerationValidateConfigRequiresSelectedAccounts(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.AccountScope = ContentModerationAccountScopeSelected
	cfg.AccountIDs = nil
	svc := &ContentModerationService{}

	err := svc.validateConfig(context.Background(), cfg)

	require.ErrorContains(t, err, "至少需要配置 1 个账号")
}

func TestContentModerationValidateConfigRejectsMissingSelectedAccount(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.AccountScope = ContentModerationAccountScopeSelected
	cfg.AccountIDs = []int64{11, 12}
	svc := &ContentModerationService{
		accountScopeRepo: contentModerationAccountScopeRepoStub{accounts: []*Account{{ID: 11}}},
	}

	err := svc.validateConfig(context.Background(), cfg)

	require.ErrorContains(t, err, "审计账号不存在: 12")
}

func TestContentModerationBuildLogIncludesSelectedAccountSnapshot(t *testing.T) {
	log := (&ContentModerationService{}).buildLog(ContentModerationCheckInput{
		AccountID:   42,
		AccountName: "oauth-primary",
		AccountType: AccountTypeOAuth,
	}, defaultContentModerationConfig(), ContentModerationActionAllow, false, "", 0, nil, "", nil, nil, "")

	require.NotNil(t, log.AccountID)
	require.Equal(t, int64(42), *log.AccountID)
	require.Equal(t, "oauth-primary", log.AccountName)
	require.Equal(t, AccountTypeOAuth, log.AccountType)
}
