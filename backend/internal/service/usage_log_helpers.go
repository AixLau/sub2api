package service

import "strings"

func optionalTrimmedStringPtr(raw string) *string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func optionalStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

// coalesceRequestedReasoningEffort prefers the client-requested value and falls
// back to the effective effort for unmapped and historical call paths.
func coalesceRequestedReasoningEffort(requested, effective *string) *string {
	if trimmed := optionalStringValue(requested); trimmed != "" {
		return &trimmed
	}
	if trimmed := optionalStringValue(effective); trimmed != "" {
		return &trimmed
	}
	return nil
}

// usageBillingModelForSource selects the model name before pricing lookup.
// Channel-mapped billing uses the explicit channel target when one exists;
// otherwise any later account/provider mapping is represented by upstreamModel.
func usageBillingModelForSource(source, requestedModel, channelMappedModel, forwardedModel, upstreamModel, explicitBillingModel string) string {
	requestedModel = strings.TrimSpace(requestedModel)
	channelMappedModel = strings.TrimSpace(channelMappedModel)
	forwardedModel = strings.TrimSpace(forwardedModel)
	upstreamModel = strings.TrimSpace(upstreamModel)
	explicitBillingModel = strings.TrimSpace(explicitBillingModel)

	legacyModel := firstNonEmptyUsageModel(explicitBillingModel, forwardedModel, upstreamModel, channelMappedModel, requestedModel)
	switch source {
	case BillingModelSourceRequested:
		return firstNonEmptyUsageModel(requestedModel, legacyModel)
	case BillingModelSourceUpstream:
		return firstNonEmptyUsageModel(upstreamModel, explicitBillingModel, forwardedModel, channelMappedModel, requestedModel)
	case BillingModelSourceChannelMapped:
		if channelMappedModel != "" && (requestedModel == "" || !strings.EqualFold(channelMappedModel, requestedModel)) {
			return channelMappedModel
		}
		return firstNonEmptyUsageModel(upstreamModel, explicitBillingModel, forwardedModel, channelMappedModel, requestedModel)
	default:
		return legacyModel
	}
}

func firstNonEmptyUsageModel(models ...string) string {
	for _, model := range models {
		if trimmed := strings.TrimSpace(model); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func optionalInt64Ptr(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}

func forwardResultBillingModel(requestedModel, upstreamModel string) string {
	return firstNonEmptyUsageModel(requestedModel, upstreamModel)
}
