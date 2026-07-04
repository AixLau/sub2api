package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOpenAIResponsesRequiredImageCapability(t *testing.T) {
	t.Run("image generation tool requires responses image tool capability", func(t *testing.T) {
		body := []byte(`{"model":"gpt-5.5","input":"draw","tools":[{"type":"image_generation"}]}`)

		require.Equal(t, service.OpenAIImagesCapabilityResponsesImageTool, openAIResponsesRequiredImageCapability("gpt-5.5", body))
	})

	t.Run("plain responses request does not require image capability", func(t *testing.T) {
		body := []byte(`{"model":"gpt-5.5","input":"hello"}`)

		require.Empty(t, openAIResponsesRequiredImageCapability("gpt-5.5", body))
	})
}
