//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func collectFramedSSELines(lines ...string) []string {
	out := make([]string, 0, len(lines)*2)
	framer := newOpenAISSEDataFramer(func(line string) {
		out = append(out, line)
	})
	for _, line := range lines {
		framer.Add(line)
	}
	return out
}

func TestOpenAISSEDataFramer_FramesSingleNewlineStream(t *testing.T) {
	t.Parallel()

	got := collectFramedSSELines(
		`data: {"choices":[{"delta":{"content":"hello"}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	)
	require.Equal(t, []string{
		`data: {"choices":[{"delta":{"content":"hello"}}]}`,
		"",
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		"",
		"data: [DONE]",
		"",
	}, got)
}

func TestOpenAISSEDataFramer_LeavesCompliantStreamUnchanged(t *testing.T) {
	t.Parallel()

	input := []string{
		`: heartbeat`,
		`data: {"choices":[{"delta":{"content":"hello"}}]}`,
		"",
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		"",
		`data: [DONE]`,
		"",
	}
	got := collectFramedSSELines(input...)
	require.Equal(t, input, got)
}

func TestOpenAISSEDataFramer_PreservesEventFieldFraming(t *testing.T) {
	t.Parallel()

	got := collectFramedSSELines(
		`event: message_start`,
		`data: {"type":"message_start"}`,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta"}`,
	)
	require.Equal(t, []string{
		`event: message_start`,
		`data: {"type":"message_start"}`,
		"",
		`event: content_block_delta`,
		`data: {"type":"content_block_delta"}`,
		"",
	}, got)
}

func TestOpenAISSEDataFramer_PassesThroughNonSSEBody(t *testing.T) {
	t.Parallel()

	body := `{"id":"chatcmpl_1","object":"chat.completion","choices":[{"message":{"content":"hello"}}]}`
	got := collectFramedSSELines(body)
	require.Equal(t, []string{body}, got)
}

func TestOpenAISSEDataFramer_SkipsOnlyOneUpstreamBlankAfterData(t *testing.T) {
	t.Parallel()

	got := collectFramedSSELines(
		`data: one`,
		"",
		"",
		`data: two`,
	)
	require.Equal(t, []string{
		`data: one`,
		"",
		"",
		`data: two`,
		"",
	}, got)
}
