package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIBusinessFastImportDefaults(t *testing.T) {
	expires := time.Date(2027, time.January, 1, 8, 0, 0, 0, time.UTC)
	before := expires.Add(-time.Nanosecond)
	for _, tc := range []struct {
		plan string
		at   time.Time
		want bool
	}{
		{"team", before, true},
		{" self_serve_business_usage_based ", before, true},
		{"SELF_SERVE_BUSINESS_PROLITE", before, true},
		{"business", before, false},
		{"enterprise", before, false},
		{"plus", before, false},
		{"", before, false},
		{"team", expires, false},
	} {
		input := &CreateAccountInput{
			Platform: PlatformOpenAI, Type: AccountTypeOAuth,
			Credentials: map[string]any{"plan_type": tc.plan},
			Extra:       map[string]any{openaiAccountFreeFastKey: false},
		}
		input.ApplyOpenAIBusinessFastImportDefaults(tc.at)
		_, imported := input.Extra[openaiBusinessFastUntilKey]
		require.Equal(t, tc.want, imported, tc.plan)

		account := &Account{
			Platform: input.Platform, Type: input.Type,
			Credentials: input.Credentials, Extra: input.Extra,
		}
		require.Equal(t, tc.want, account.openAIFastEnabled(openaiAccountForceFastKey, tc.at))
		require.False(t, billsOpenAIFastAtStandard(nil, account, "priority", tc.at))
		require.False(t, account.openAIFastEnabled(openaiAccountForceFastKey, expires))
		account.Extra[openaiAccountForceFastKey] = true
		require.True(t, account.openAIFastEnabled(openaiAccountForceFastKey, expires))
	}
}

func TestOpenAIAccountForceFastAppliesBeforeGlobalPolicy(t *testing.T) {
	ctx := context.Background()
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{
		openaiAccountForceFastKey: true,
	}}
	model := "gpt-6.1-sol"
	body := []byte(`{"model":"gpt-6.1-sol","service_tier":"flex"}`)
	frame := []byte(`{"type":"response.create","model":"gpt-6.1-sol"}`)
	service := newOpenAIGatewayServiceWithSettings(t, &OpenAIFastPolicySettings{})

	updated, err := service.applyOpenAIFastPolicyToBody(ctx, account, model, body)
	require.NoError(t, err)
	require.Equal(t, OpenAIFastTierPriority, gjson.GetBytes(updated, "service_tier").String())
	updated, blocked, err := service.applyOpenAIFastPolicyToWSResponseCreate(ctx, account, model, frame)
	require.NoError(t, err)
	require.Nil(t, blocked)
	require.Equal(t, OpenAIFastTierPriority, gjson.GetBytes(updated, "service_tier").String())
	updated, err = service.applyOpenAIFastPolicyToBody(openAIUltrafastGroupContext(), account, model, body)
	require.NoError(t, err)
	require.Equal(t, OpenAIFastTierUltrafast, gjson.GetBytes(updated, "service_tier").String())

	ctx = withOpenAIFastPolicyContext(ctx, &OpenAIFastPolicySettings{Rules: []OpenAIFastPolicyRule{{
		ServiceTier: OpenAIFastTierPriority, Scope: BetaPolicyScopeAll, Action: BetaPolicyActionFilter,
	}}})
	updated, err = service.applyOpenAIFastPolicyToBody(ctx, account, model, body)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(updated, "service_tier").Exists())
	updated, blocked, err = service.applyOpenAIFastPolicyToWSResponseCreate(ctx, account, model, frame)
	require.NoError(t, err)
	require.Nil(t, blocked)
	require.False(t, gjson.GetBytes(updated, "service_tier").Exists())
}
