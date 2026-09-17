package service

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type CredentialRouteCandidate struct{ PrincipalID, InstanceID, AccountID int64 }
type CredentialRouteStore interface {
	CredentialRoutes(context.Context, int64) ([]CredentialRouteCandidate, bool, error)
	BoundCredentialPrincipal(context.Context, string, string) (int64, error)
	IsControlledCredentialAccount(context.Context, int64) (bool, error)
	CheckCredentialRuntime(context.Context, bool) error
	CredentialProbeRoute(context.Context, int64) (CredentialRouteCandidate, int64, error)
}
type CredentialHTTPRuntime struct {
	QueueBudget CredentialQueueBudget
	Store       PrincipalAdmissionStore
	Routes      CredentialRouteStore
	Vault       *CredentialVault
	Enabled     bool
}

func NewCredentialHTTPRuntime(store PrincipalAdmissionStore, routes CredentialRouteStore, cfg *config.Config) (*CredentialHTTPRuntime, error) {
	RegisterCredentialMetrics()
	vault, _ := NewCredentialVault(cfg.Gateway.CredentialVaultKey)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := routes.CheckCredentialRuntime(ctx, cfg.Gateway.MultiCredentialHTTPEnabled); err != nil {
		return nil, err
	}
	return &CredentialHTTPRuntime{Store: store, Routes: routes, Vault: vault, Enabled: cfg.Gateway.MultiCredentialHTTPEnabled}, nil
}

// BuildCredentialRoute preserves existing model, Codex-client, channel and
// profit filters. One representative per principal enters account ordering.
// Controlled accounts stay unschedulable in every legacy scheduler.
func (s *OpenAIGatewayService) BuildCredentialRoute(ctx context.Context, group int64, model string, compact bool, candidates []CredentialRouteCandidate, bound int64) (int64, []int64, map[int64]*Account, error) {
	accounts := map[int64]*Account{}
	byPrincipal := map[int64][]int64{}
	representatives := map[int64]*Account{}
	for _, candidate := range candidates {
		if bound != 0 && candidate.PrincipalID != bound {
			continue
		}
		account, err := s.accountRepo.GetByID(ctx, candidate.AccountID)
		if err != nil {
			return 0, nil, nil, err
		}
		copyAccount := *account
		copyAccount.Status = StatusActive
		copyAccount.Schedulable = true
		if !s.openAIAccountMatchesSchedulingGroup(&copyAccount, &group) || !isOpenAICompatibleAccountEligibleForRequestBeforeProfit(ctx, &copyAccount, PlatformOpenAI, model, compact, "") {
			continue
		}
		if allowed, _ := s.codexAccountAllowedForScheduling(ctx, &copyAccount); !allowed {
			continue
		}
		if s.isUpstreamModelRestrictedByChannel(ctx, group, &copyAccount, model, compact) {
			continue
		}
		if vetoed, _ := openAIProfitControlVetoReason(ctx, &copyAccount); vetoed {
			continue
		}
		accounts[account.ID] = account
		byPrincipal[candidate.PrincipalID] = append(byPrincipal[candidate.PrincipalID], candidate.InstanceID)
		if old := representatives[candidate.PrincipalID]; old == nil || s.isBetterAccount(&copyAccount, old) {
			representatives[candidate.PrincipalID] = &copyAccount
		}
	}
	ids := make([]int64, 0, len(representatives))
	for id := range representatives {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := representatives[ids[i]], representatives[ids[j]]
		if s.isBetterAccount(a, b) {
			return true
		}
		if s.isBetterAccount(b, a) {
			return false
		}
		return ids[i] < ids[j]
	})
	if len(ids) == 0 {
		return 0, nil, nil, errors.New("NO_AUTHORIZED_CREDENTIAL_INSTANCE")
	}
	return ids[0], byPrincipal[ids[0]], accounts, nil
}

func CredentialHTTPAccountAuthorized(ctx context.Context, accountID int64) bool {
	e := credentialExecutionFromContext(ctx)
	return e != nil && e.snapshot.AccountID == accountID
}
