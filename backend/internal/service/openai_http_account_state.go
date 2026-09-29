package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/responsesstate"
)

// Provenance is separate from evictable scheduling affinity and plugin replay
// data. Only hashes and account IDs are stored, scoped by authenticated tenant.
// Expired/unknown provenance fails closed; it never licenses account migration.
const OpenAIHTTPAccountStateTTL = 90 * 24 * time.Hour

var ErrOpenAIHTTPStateOwnerConflict = errors.New("account-bound state has conflicting owners")

type OpenAIHTTPAccountStateCache interface {
	GetHTTPAccountStateOwner(context.Context, int64, int64, string) (int64, error)
	// Bind must atomically reject an existing different owner and renew only
	// that same owner's lease. It must never overwrite a competing claim.
	BindHTTPAccountStateOwner(context.Context, int64, int64, string, int64, time.Duration) error
}

type OpenAIHTTPAccountStateError struct {
	Status  int
	Code    string
	Message string
}

func (e *OpenAIHTTPAccountStateError) Error() string { return e.Message }

func HTTPAccountStateUnavailable() *OpenAIHTTPAccountStateError {
	return &OpenAIHTTPAccountStateError{http.StatusBadRequest, "account_bound_state_unavailable", "This history requires its original account, which is unavailable for this request. No other account was contacted. Restore access to the original account or start a new conversation with complete ordinary history."}
}

func httpAccountStateStoreError() *OpenAIHTTPAccountStateError {
	return &OpenAIHTTPAccountStateError{http.StatusServiceUnavailable, "account_state_store_unavailable", "Account-bound history ownership is temporarily unavailable; retry this request later."}
}

type httpAccountStateKey struct{}
type HTTPAccountStateBinding struct{ AccountID int64 }

func WithHTTPAccountStateBinding(ctx context.Context, binding HTTPAccountStateBinding) context.Context {
	return context.WithValue(ctx, httpAccountStateKey{}, binding)
}
func HTTPAccountStateAccountID(ctx context.Context) int64 {
	b, _ := ctx.Value(httpAccountStateKey{}).(HTTPAccountStateBinding)
	return b.AccountID
}

func (s *OpenAIGatewayService) ResolveHTTPAccountState(ctx context.Context, groupID *int64, userID int64, body []byte) (HTTPAccountStateBinding, error) {
	binding := HTTPAccountStateBinding{}
	keys := responsesstate.InputKeys(body)
	if len(keys) == 0 {
		return binding, nil
	}
	cache, ok := s.cache.(OpenAIHTTPAccountStateCache)
	if !ok || userID <= 0 {
		return binding, httpAccountStateStoreError()
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	for _, key := range keys {
		owner, err := cache.GetHTTPAccountStateOwner(ctx, derefGroupID(groupID), userID, key)
		if errors.Is(err, ErrStickySessionNotFound) || (err == nil && owner <= 0) {
			return HTTPAccountStateBinding{}, &OpenAIHTTPAccountStateError{http.StatusBadRequest, "account_bound_state_unknown", "The original account for this encrypted or tool history cannot be verified (missing or expired ownership). No account was contacted. Start a new conversation with complete ordinary history; opaque history cannot be migrated."}
		}
		if err != nil {
			return HTTPAccountStateBinding{}, httpAccountStateStoreError()
		}
		if binding.AccountID != 0 && binding.AccountID != owner {
			return HTTPAccountStateBinding{}, &OpenAIHTTPAccountStateError{http.StatusBadRequest, "account_bound_state_conflict", "This request mixes state from different accounts and cannot be replayed safely. Start a new conversation with complete ordinary history."}
		}
		binding.AccountID = owner
		if err := cache.BindHTTPAccountStateOwner(ctx, derefGroupID(groupID), userID, key, owner, OpenAIHTTPAccountStateTTL); err != nil {
			return HTTPAccountStateBinding{}, httpAccountStateStoreError()
		}
	}
	return binding, nil
}

func (s *OpenAIGatewayService) RecordHTTPAccountState(ctx context.Context, groupID *int64, userID, accountID int64, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	cache, ok := s.cache.(OpenAIHTTPAccountStateCache)
	if !ok || userID <= 0 || accountID <= 0 {
		return httpAccountStateStoreError()
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	for _, key := range keys {
		if err := cache.BindHTTPAccountStateOwner(ctx, derefGroupID(groupID), userID, key, accountID, OpenAIHTTPAccountStateTTL); err != nil {
			if errors.Is(err, ErrOpenAIHTTPStateOwnerConflict) {
				return &OpenAIHTTPAccountStateError{http.StatusBadRequest, "account_bound_state_conflict", "Upstream returned state already owned by a different account; it was not delivered."}
			}
			return httpAccountStateStoreError()
		}
	}
	return nil
}

// SelectHTTPAccountStateOwner uses existing account admission checks, but never
// executes the load-balancing layer (including sticky escape / spillover).
func (s *OpenAIGatewayService) SelectHTTPAccountStateOwner(ctx context.Context, req OpenAIAccountScheduleRequest, userID int64) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	id := HTTPAccountStateAccountID(ctx)
	decision := newOpenAIAccountScheduleDecision(req)
	decision.Layer = "http_account_bound_state"
	if id <= 0 {
		return nil, decision, HTTPAccountStateUnavailable()
	}
	req.ExcludedIDs, req.UserCooldownIDs = s.mergeUserAccountCooldownsWithDiagnostics(ctx, req.ExcludedIDs, userID)
	if _, excluded := req.ExcludedIDs[id]; excluded {
		return nil, decision, HTTPAccountStateUnavailable()
	}
	if s.checkChannelPricingRestriction(ctx, req.GroupID, req.RequestedModel) {
		return nil, decision, HTTPAccountStateUnavailable()
	}
	ctx = s.withOpenAIQuotaAutoPauseContext(ctx)
	ctx = s.withOpenAIGroupPrivacyRequirement(ctx, req.GroupID)
	ctx = s.withOpenAIProfitControlGate(ctx, req.GroupID)
	req.RequirePrivacySet = s.openAIGroupRequiresPrivacySet(ctx, req.GroupID)
	req.StickyAccountID = id
	req.SessionHash = fmt.Sprintf("http-state-owner:%d", id)
	req.PreserveStickyBinding = true
	req.DisableStickyEscape = true
	scheduler := &defaultOpenAIAccountScheduler{service: s}
	selection, _, err := scheduler.selectBySessionHash(ctx, req)
	if err != nil {
		return nil, decision, httpAccountStateStoreError()
	}
	if selection == nil || selection.Account == nil {
		return nil, decision, HTTPAccountStateUnavailable()
	}
	decision.SelectedAccountID = selection.Account.ID
	decision.SelectedAccountType = selection.Account.Type
	return selection, decision, nil
}
