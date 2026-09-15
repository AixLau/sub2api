package handler

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOpenAICompatibleRequestPlatformUsesGroupPlatform(t *testing.T) {
	for _, platform := range []string{
		service.PlatformGrok,
		service.PlatformKimi,
		service.PlatformZhipu,
		service.PlatformDeepseek,
		service.PlatformMiniMax,
	} {
		t.Run(platform, func(t *testing.T) {
			apiKey := &service.APIKey{Group: &service.Group{Platform: platform}}
			require.Equal(t, platform, openAICompatibleRequestPlatform(context.Background(), apiKey))
		})
	}
}

func TestOpenAICompatibleRequestPlatformUsesResolvedCNPlatform(t *testing.T) {
	for _, platform := range []string{
		service.PlatformKimi,
		service.PlatformZhipu,
		service.PlatformDeepseek,
		service.PlatformMiniMax,
	} {
		t.Run(platform, func(t *testing.T) {
			ctx := service.WithResolvedTargetPlatform(context.Background(), platform)
			require.Equal(t, platform, openAICompatibleRequestPlatform(ctx, nil))
		})
	}
}

func TestOpenAICompatibleRequestPlatformDefaultsToOpenAI(t *testing.T) {
	require.Equal(t, service.PlatformOpenAI, openAICompatibleRequestPlatform(context.Background(), nil))
	require.Equal(t, service.PlatformOpenAI, openAICompatibleRequestPlatform(context.Background(), &service.APIKey{
		Group: &service.Group{Platform: service.PlatformOpenAI},
	}))

	// Non-OpenAI targets are handled by their native gateway before this helper.
	ctx := service.WithResolvedTargetPlatform(context.Background(), service.PlatformAnthropic)
	require.Equal(t, service.PlatformOpenAI, openAICompatibleRequestPlatform(ctx, nil))
}
