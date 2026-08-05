package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSortedConfiguredModelNames_NumericVersionDescending(t *testing.T) {
	models := sortedConfiguredModelNames(map[string]struct{}{
		"claude-opus-4-6":        {},
		"gpt-5.5":                {},
		"codex-auto-review":      {},
		"claude-opus-5":          {},
		"gpt-5.6-sol":            {},
		"claude-opus-4-8":        {},
		"claude-opus-4-20250514": {},
	})

	require.Equal(t, []string{
		"gpt-5.6-sol",
		"gpt-5.5",
		"claude-opus-5",
		"claude-opus-4-8",
		"claude-opus-4-6",
		"claude-opus-4-20250514",
		"codex-auto-review",
	}, models)
}
