package service

import (
	"context"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// ModelAvailabilityDiagnosis describes whether the requested model can be
// served by any persistently eligible account in the group (active with its
// schedulable setting enabled), ignoring transient state such as rate limits,
// overload, temporary unschedulability, and runtime blocks. Handlers use this
// on the "no available accounts" error path to distinguish 404
// model_not_found from 503 service_unavailable.
type ModelAvailabilityDiagnosis struct {
	// HasAccountsInPool is true if the group has at least one persistently
	// eligible account on the queried platform (or, for Anthropic/Gemini, on
	// the platform plus mixed-scheduled Antigravity accounts).
	HasAccountsInPool bool
	// HasModelSupport is true if at least one account's model mapping admits
	// the requested model.
	HasModelSupport bool
	// SupportedModels contains concrete client-facing model names configured
	// on the inspected accounts. It is populated only when model support is
	// missing, sorted by numeric model version from newest to oldest.
	SupportedModels []string
}

func addConfiguredModelNames(dst map[string]struct{}, account *Account) {
	if account == nil {
		return
	}
	for model := range account.GetModelMapping() {
		model = strings.TrimSpace(model)
		if model == "" || strings.Contains(model, "*") {
			continue
		}
		dst[model] = struct{}{}
	}
}

func sortedConfiguredModelNames(modelSet map[string]struct{}) []string {
	models := make([]string, 0, len(modelSet))
	for model := range modelSet {
		models = append(models, model)
	}
	sort.Slice(models, func(i, j int) bool {
		leftVersion := modelVersionParts(models[i])
		rightVersion := modelVersionParts(models[j])
		if cmp := compareModelVersions(leftVersion, rightVersion); cmp != 0 {
			return cmp > 0
		}
		return strings.ToLower(models[i]) < strings.ToLower(models[j])
	})
	return models
}

// modelVersionParts extracts the leading model version while ignoring date
// suffixes. For example: gpt-5.6-sol -> [5,6], claude-opus-4-8 -> [4,8],
// and claude-opus-4-20250514 -> [4].
func modelVersionParts(model string) []int {
	start := -1
	for i := 0; i < len(model); i++ {
		if model[i] >= '0' && model[i] <= '9' {
			start = i
			break
		}
	}
	if start < 0 {
		return nil
	}

	parts := make([]int, 0, 3)
	for pos := start; pos < len(model); {
		end := pos
		value := 0
		for end < len(model) && model[end] >= '0' && model[end] <= '9' {
			value = value*10 + int(model[end]-'0')
			end++
		}
		digits := end - pos
		if digits == 0 || (len(parts) == 0 && digits >= 4) || (len(parts) > 0 && digits > 2) {
			break
		}
		parts = append(parts, value)
		if end >= len(model) || (model[end] != '.' && model[end] != '-') {
			break
		}
		pos = end + 1
		if pos >= len(model) || model[pos] < '0' || model[pos] > '9' {
			break
		}
	}
	return parts
}

func compareModelVersions(left, right []int) int {
	maxLen := len(left)
	if len(right) > maxLen {
		maxLen = len(right)
	}
	for i := 0; i < maxLen; i++ {
		leftPart, rightPart := 0, 0
		if i < len(left) {
			leftPart = left[i]
		}
		if i < len(right) {
			rightPart = right[i]
		}
		if leftPart > rightPart {
			return 1
		}
		if leftPart < rightPart {
			return -1
		}
	}
	if len(left) > 0 && len(right) == 0 {
		return 1
	}
	if len(left) == 0 && len(right) > 0 {
		return -1
	}
	return 0
}

// ModelAvailabilityDiagnoser is implemented by gateway services that can
// report whether the requested model is configured to be served by any
// account. Both *GatewayService and *OpenAIGatewayService implement this so
// handlers in either package can share a single classifier.
type ModelAvailabilityDiagnoser interface {
	DiagnoseModelAvailabilityForPlatform(
		ctx context.Context,
		groupID *int64,
		requestedModel string,
		platform string,
	) ModelAvailabilityDiagnosis
}

// DiagnoseModelAvailabilityForPlatform inspects accounts enabled for scheduling
// by persistent configuration and returns whether the requested model is
// configured to be served by any of them. The dedicated repository query
// bypasses scheduler snapshots and deliberately ignores transient rate-limit,
// overload, temporary-unschedulable, expiry, quota, and runtime-block state.
//
// Safe to call on the error path: returns {true,true} on any internal failure
// or when the inputs preclude meaningful diagnosis (empty model, etc.), so
// callers stay on the 503 fallback branch.
func (s *GatewayService) DiagnoseModelAvailabilityForPlatform(
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
		// No model specified — cannot decide model_not_found. Caller falls back to 503.
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	}
	if strings.TrimSpace(platform) == "" {
		// Without a platform we cannot scope the lookup; bail out to the
		// 503 branch rather than make an unscoped scan.
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	}

	if s.accountRepo == nil {
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	}

	useMixed := platform == PlatformAnthropic || platform == PlatformGemini
	platforms := []string{platform}
	if useMixed {
		platforms = append(platforms, PlatformAntigravity)
	}

	queryGroupID := groupID
	includeGrouped := false
	if useMixed {
		// Preserve the generic scheduler's scope rules: an explicit group wins
		// for mixed scheduling, while group-less simple mode scans all accounts.
		if groupID == nil && s.cfg != nil && s.cfg.RunMode == config.RunModeSimple {
			includeGrouped = true
		}
	} else if s.cfg != nil && s.cfg.RunMode == config.RunModeSimple {
		queryGroupID = nil
		includeGrouped = true
	}

	accounts, err := s.accountRepo.ListModelAvailabilityCandidates(ctx, queryGroupID, platforms, includeGrouped)
	if err != nil {
		// Conservative fallback: pretend everything is fine so the caller
		// returns 503 (we don't want to flip to 404 just because a lookup
		// hiccup'd).
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	}

	diag := ModelAvailabilityDiagnosis{}
	configuredModels := make(map[string]struct{})
	for i := range accounts {
		if useMixed && accounts[i].Platform == PlatformAntigravity && !accounts[i].IsMixedSchedulingEnabled() {
			continue
		}
		diag.HasAccountsInPool = true
		addConfiguredModelNames(configuredModels, &accounts[i])
		if s.isModelSupportedByAccountWithContext(ctx, &accounts[i], requestedModel) {
			diag.HasModelSupport = true
			return diag
		}
	}
	diag.SupportedModels = sortedConfiguredModelNames(configuredModels)
	return diag
}
