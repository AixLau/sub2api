package service

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

// ListSemanticReviewModels returns concrete text-capable model names exposed
// by active, schedulable OpenAI accounts. Explicit account mappings are used
// verbatim; accounts without a mapping use the platform defaults.
func (s *OpenAIGatewayService) ListSemanticReviewModels(ctx context.Context) ([]string, error) {
	if s == nil || s.accountRepo == nil {
		return []string{}, nil
	}
	accounts, err := s.accountRepo.ListModelAvailabilityCandidates(ctx, nil, []string{PlatformOpenAI}, true)
	if err != nil {
		return nil, err
	}
	models := make(map[string]struct{})
	for i := range accounts {
		account := &accounts[i]
		if account.IsShadow() || !account.IsOpenAI() {
			continue
		}
		mapping := account.GetModelMapping()
		if account.IsOpenAIPassthroughEnabled() || len(mapping) == 0 {
			for _, model := range openai.DefaultModels {
				if strings.Contains(strings.ToLower(model.ID), "image") {
					continue
				}
				models[model.ID] = struct{}{}
			}
			continue
		}
		addConfiguredModelNames(models, account)
	}
	return sortedConfiguredModelNames(models), nil
}

// DiagnoseModelAvailabilityForPlatform reports whether the requested model
// is configured to be served by any persistently eligible OpenAI-compatible
// account in the group for the given platform (e.g. PlatformOpenAI,
// PlatformGrok). The platform scopes the candidate pool so distinct
// OpenAI-compatible platforms do not cross-contaminate diagnosis results.
// The query bypasses scheduler snapshots and ignores transient runtime state.
//
// Safe to call on the error path: returns {true,true} on any internal
// failure or when the inputs preclude meaningful diagnosis (empty model,
// nil service), so callers stay on the 503 fallback branch.
func (s *OpenAIGatewayService) DiagnoseModelAvailabilityForPlatform(
	ctx context.Context,
	groupID *int64,
	requestedModel string,
	platform string,
) ModelAvailabilityDiagnosis {
	if s == nil {
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	}
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	}
	if s.accountRepo == nil {
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	}

	platform = NormalizeOpenAICompatiblePlatform(platform)
	queryGroupID := groupID
	includeGrouped := false
	if s.cfg != nil && s.cfg.RunMode == config.RunModeSimple {
		queryGroupID = nil
		includeGrouped = true
	}
	accounts, err := s.accountRepo.ListModelAvailabilityCandidates(
		ctx,
		queryGroupID,
		[]string{platform},
		includeGrouped,
	)
	if err != nil {
		// Conservative fallback so the caller keeps returning 503; we do not
		// want a transient lookup failure to flip into 404 model_not_found.
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	}

	diag := ModelAvailabilityDiagnosis{}
	configuredModels := make(map[string]struct{})
	for i := range accounts {
		diag.HasAccountsInPool = true
		addConfiguredModelNames(configuredModels, &accounts[i])
		// Mirrors the per-candidate filter used during account selection
		// (openai_account_scheduler.isAccountRequestCompatible): empty
		// model_mapping accepts everything; otherwise the explicit / wildcard
		// mapping must match.
		if accounts[i].IsModelSupported(requestedModel) {
			diag.HasModelSupport = true
			return diag
		}
	}
	diag.SupportedModels = sortedConfiguredModelNames(configuredModels)
	return diag
}
