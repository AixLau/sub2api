package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestSelectSemanticReviewAccountRequiresSparkProPlan(t *testing.T) {
	const spark = ContentModerationSemanticReviewPrimaryModel
	for _, plan := range []string{"plus", "free", "team", "enterprise", "", "unknown"} {
		t.Run(plan, func(t *testing.T) {
			accounts := make([]Account, 0, 5)
			for id := int64(1); id <= 4; id++ {
				accounts = append(accounts, semanticReviewSchedulingAccount(id, plan, spark))
			}
			accounts = append(accounts, semanticReviewSchedulingAccount(5, "pro", spark))
			svc := &OpenAIGatewayService{accountRepo: groupAwareStubOpenAIAccountRepo{stubOpenAIAccountRepo{accounts: accounts}}}
			excluded := map[int64]struct{}{99: {}}

			selection, err := svc.SelectSemanticReviewAccount(context.Background(), nil, spark, excluded)

			require.NoError(t, err)
			require.NotNil(t, selection)
			require.Equal(t, int64(5), selection.Account.ID)
			require.Equal(t, map[int64]struct{}{99: {}}, excluded, "selection must not modify caller exclusions")
			releaseAccountSelection(selection)
		})
	}
}

func TestSelectSemanticReviewAccountSparkPlanAcrossGroupsAndGlobalRateLimit(t *testing.T) {
	const spark = ContentModerationSemanticReviewPrimaryModel
	for _, rateLimited := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "global_429"}[rateLimited], func(t *testing.T) {
			businessGroup, auditGroup := int64(9), int64(17)
			plus := semanticReviewSchedulingAccount(1, "plus", spark)
			plus.GroupIDs = []int64{businessGroup}
			pro := semanticReviewSchedulingAccount(2, "pro", spark)
			pro.GroupIDs = []int64{auditGroup}
			if rateLimited {
				resetAt := time.Now().Add(time.Hour)
				plus.RateLimitResetAt = &resetAt
				pro.RateLimitResetAt = &resetAt
			}
			svc := &OpenAIGatewayService{accountRepo: groupAwareStubOpenAIAccountRepo{stubOpenAIAccountRepo{accounts: []Account{plus, pro}}}}

			selection, err := svc.SelectSemanticReviewAccount(context.Background(), &businessGroup, spark, nil)

			require.NoError(t, err)
			require.NotNil(t, selection)
			require.Equal(t, pro.ID, selection.Account.ID)
			releaseAccountSelection(selection)
		})
	}
}

func TestSelectSemanticReviewAccountSparkProWithAdvancedScheduler(t *testing.T) {
	accounts := make([]Account, 0, 101)
	for id := int64(1); id <= 100; id++ {
		accounts = append(accounts, semanticReviewSchedulingAccount(id, "plus", ContentModerationSemanticReviewPrimaryModel))
	}
	accounts = append(accounts, semanticReviewSchedulingAccount(101, "pro", ContentModerationSemanticReviewPrimaryModel))
	svc := &OpenAIGatewayService{
		accountRepo:      groupAwareStubOpenAIAccountRepo{stubOpenAIAccountRepo{accounts: accounts}},
		rateLimitService: newOpenAIAdvancedSchedulerRateLimitService("true"),
		cfg:              &config.Config{},
	}
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
	require.NotNil(t, svc.getOpenAIAccountScheduler(context.Background()))

	selection, err := svc.SelectSemanticReviewAccount(context.Background(), nil, ContentModerationSemanticReviewPrimaryModel, nil)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.Equal(t, int64(101), selection.Account.ID)
	releaseAccountSelection(selection)
}

func TestSelectSemanticReviewAccountSparkShadowUsesParentPlan(t *testing.T) {
	const spark = ContentModerationSemanticReviewPrimaryModel
	plusParent := semanticReviewSchedulingAccount(1, "plus", spark)
	plusParent.Schedulable = false
	plusShadow := semanticReviewSchedulingAccount(2, "pro", spark)
	plusShadow.ParentAccountID = &plusParent.ID
	plusShadow.QuotaDimension = QuotaDimensionSpark
	proParent := semanticReviewSchedulingAccount(3, "pro", spark)
	proParent.Schedulable = false
	proShadow := semanticReviewSchedulingAccount(4, "", spark)
	proShadow.ParentAccountID = &proParent.ID
	proShadow.QuotaDimension = QuotaDimensionSpark
	svc := &OpenAIGatewayService{accountRepo: groupAwareStubOpenAIAccountRepo{stubOpenAIAccountRepo{
		accounts: []Account{plusParent, plusShadow, proParent, proShadow},
	}}}

	selection, err := svc.SelectSemanticReviewAccount(context.Background(), nil, spark, nil)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.Equal(t, proShadow.ID, selection.Account.ID)
	releaseAccountSelection(selection)
}

func TestSelectSemanticReviewAccountSparkPlanUsesMappedModel(t *testing.T) {
	account := semanticReviewSchedulingAccount(1, "plus", "audit-model")
	account.Credentials["model_mapping"] = map[string]any{"audit-model": ContentModerationSemanticReviewPrimaryModel}
	svc := &OpenAIGatewayService{accountRepo: groupAwareStubOpenAIAccountRepo{stubOpenAIAccountRepo{accounts: []Account{account}}}}

	selection, err := svc.SelectSemanticReviewAccount(context.Background(), nil, "audit-model", nil)

	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	require.Nil(t, selection)
}

func TestSelectSemanticReviewAccountSparkRejectsPoolWithoutPro(t *testing.T) {
	for _, rateLimited := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "global_429"}[rateLimited], func(t *testing.T) {
			account := semanticReviewSchedulingAccount(1, "plus", ContentModerationSemanticReviewPrimaryModel)
			if rateLimited {
				resetAt := time.Now().Add(time.Hour)
				account.RateLimitResetAt = &resetAt
			}
			svc := &OpenAIGatewayService{accountRepo: groupAwareStubOpenAIAccountRepo{stubOpenAIAccountRepo{accounts: []Account{account}}}}

			selection, err := svc.SelectSemanticReviewAccount(context.Background(), nil, ContentModerationSemanticReviewPrimaryModel, nil)

			require.ErrorIs(t, err, ErrNoAvailableAccounts)
			require.Nil(t, selection)
		})
	}
}

func TestSelectSemanticReviewAccountSparkPlanPreservesEligibleAccounts(t *testing.T) {
	for _, tt := range []struct {
		name, plan, accountType, model string
	}{
		{"pro", "pro", AccountTypeOAuth, ContentModerationSemanticReviewPrimaryModel},
		{"pro_alias", " CHATGPTPRO ", AccountTypeOAuth, ContentModerationSemanticReviewPrimaryModel},
		{"api_key", "", AccountTypeAPIKey, ContentModerationSemanticReviewPrimaryModel},
		{"plus_fallback_model", "plus", AccountTypeOAuth, ContentModerationSemanticReviewFallbackModel},
	} {
		t.Run(tt.name, func(t *testing.T) {
			account := semanticReviewSchedulingAccount(1, tt.plan, tt.model)
			account.Type = tt.accountType
			svc := &OpenAIGatewayService{accountRepo: groupAwareStubOpenAIAccountRepo{stubOpenAIAccountRepo{accounts: []Account{account}}}}

			selection, err := svc.SelectSemanticReviewAccount(context.Background(), nil, tt.model, nil)

			require.NoError(t, err)
			require.NotNil(t, selection)
			require.Equal(t, account.ID, selection.Account.ID)
			releaseAccountSelection(selection)
		})
	}
}

func TestSemanticReviewRouterQuotaSkipsDoNotConsumeModelAttempts(t *testing.T) {
	backend := &semanticReviewBackendStub{accountsByModel: map[string][]*Account{
		ContentModerationSemanticReviewPrimaryModel: {
			exhaustedSemanticReviewAccount(1), exhaustedSemanticReviewAccount(2),
			exhaustedSemanticReviewAccount(3), freshSemanticReviewAccount(4),
		},
		ContentModerationSemanticReviewFallbackModel: {freshSemanticReviewAccount(5)},
	}}
	router := NewOpenAIContentModerationSemanticReviewRouter(backend, nil, nil)

	result, err := router.Review(context.Background(), semanticReviewTestConfig(), ContentModerationSemanticReviewInput{Text: "test"})

	require.NoError(t, err)
	require.Equal(t, ContentModerationSemanticReviewPrimaryModel, result.Model)
	require.Equal(t, int64(4), result.AccountID)
	require.Equal(t, 1, result.AttemptCount)
	require.Equal(t, []string{ContentModerationSemanticReviewPrimaryModel}, backend.reviewCalls)
}

func TestSemanticReviewRouterNeverCallsSparkWithPlus(t *testing.T) {
	for _, hasPro := range []bool{false, true} {
		t.Run(map[bool]string{false: "fallback_without_pro", true: "spark_with_pro"}[hasPro], func(t *testing.T) {
			plus := semanticReviewSchedulingAccount(1, "plus", ContentModerationSemanticReviewPrimaryModel)
			plus.Credentials["model_mapping"].(map[string]any)[ContentModerationSemanticReviewFallbackModel] = ContentModerationSemanticReviewFallbackModel
			accounts := []Account{plus}
			if hasPro {
				accounts = append(accounts, semanticReviewSchedulingAccount(2, "pro", ContentModerationSemanticReviewPrimaryModel))
			}
			backend := &semanticReviewGatewaySelectionBackend{OpenAIGatewayService: &OpenAIGatewayService{
				accountRepo: groupAwareStubOpenAIAccountRepo{stubOpenAIAccountRepo{accounts: accounts}},
			}}
			router := NewOpenAIContentModerationSemanticReviewRouter(backend, nil, nil)

			result, err := router.Review(context.Background(), semanticReviewTestConfig(), ContentModerationSemanticReviewInput{Text: "test"})

			require.NoError(t, err)
			require.Equal(t, 1, result.AttemptCount)
			if hasPro {
				require.Equal(t, ContentModerationSemanticReviewPrimaryModel, result.Model)
				require.Equal(t, []int64{2}, backend.calledAccounts)
				require.Empty(t, result.FallbackReason)
			} else {
				require.Equal(t, ContentModerationSemanticReviewFallbackModel, result.Model)
				require.Equal(t, []int64{1}, backend.calledAccounts)
				require.Equal(t, "no_account", result.FallbackReason)
			}
			require.Equal(t, []string{result.Model}, backend.calledModels)
		})
	}
}

type semanticReviewGatewaySelectionBackend struct {
	*OpenAIGatewayService
	calledAccounts []int64
	calledModels   []string
}

func (b *semanticReviewGatewaySelectionBackend) ReviewSemanticContent(_ context.Context, account *Account, model string, _ ContentModerationSemanticReviewInput) (ContentModerationSemanticReviewResult, error) {
	b.calledAccounts = append(b.calledAccounts, account.ID)
	b.calledModels = append(b.calledModels, model)
	return ContentModerationSemanticReviewResult{Verdict: "allow", Confidence: 0.99}, nil
}

func semanticReviewSchedulingAccount(id int64, plan, model string) Account {
	return Account{
		ID: id, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: int(id),
		Credentials: map[string]any{
			"plan_type": plan, "model_mapping": map[string]any{model: model},
		},
	}
}
