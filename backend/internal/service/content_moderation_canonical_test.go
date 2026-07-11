package service

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModerationChunkAppendAndEditIdentityStability(t *testing.T) {
	plan := func(sources ...ModerationTextSource) []ModerationChunk {
		stream, err := CanonicalizeModerationExtraction(ModerationExtraction{Complete: true, Sources: sources})
		require.NoError(t, err)
		chunks, err := PlanModerationChunks(stream)
		require.NoError(t, err)
		return chunks
	}
	identity := func(chunk ModerationChunk) []byte {
		_, digest, err := BuildModerationChunkIdentity(make([]byte, 32), ModerationIdentityInput{KeyVersion: 1, Provider: "p", Model: "m", AuditScope: "all_context", PolicyScope: "scope", ChunkerVersion: ModerationChunkerVersion, ContextFrame: chunk.ContextFrame, NormalizedText: chunk.NormalizedText})
		require.NoError(t, err)
		return digest
	}
	before := plan(ModerationTextSource{Source: "message", Role: "user", Text: strings.Repeat("a", 1801)})
	after := plan(ModerationTextSource{Source: "message", Role: "user", Text: strings.Repeat("a", 1801) + strings.Repeat("b", 1600)})
	require.Equal(t, before[0].Text, after[0].Text)
	require.True(t, bytes.Equal(before[0].ContextFrame, after[0].ContextFrame))
	require.Equal(t, identity(before[0]), identity(after[0]))
	require.NotEqual(t, before[1].Text, after[1].Text)
	require.NotEqual(t, identity(before[1]), identity(after[1]))

	edited := plan(ModerationTextSource{Source: "message", Role: "user", Text: "x" + strings.Repeat("a", 1801)})
	require.NotEqual(t, before[0].Text, edited[0].Text)
	require.NotEqual(t, identity(before[1]), identity(edited[1]))
	prefixed := plan(ModerationTextSource{Source: "message", Role: "user", Text: "x" + strings.Repeat("a", 1800)})
	deleted := plan(ModerationTextSource{Source: "message", Role: "user", Text: strings.Repeat("a", 1800)})
	require.NotEqual(t, identity(prefixed[0]), identity(deleted[0]))

	roleChanged := plan(ModerationTextSource{Source: "message", Role: "tool", Text: strings.Repeat("a", 1801)})
	sourceChanged := plan(ModerationTextSource{Source: "other", Role: "user", Text: strings.Repeat("a", 1801)})
	require.NotEqual(t, identity(before[0]), identity(roleChanged[0]))
	require.NotEqual(t, identity(before[0]), identity(sourceChanged[0]))

	ordered := plan(ModerationTextSource{Source: "first", Role: "user", Text: "one"}, ModerationTextSource{Source: "second", Role: "tool", Text: "two"})
	reordered := plan(ModerationTextSource{Source: "second", Role: "tool", Text: "two"}, ModerationTextSource{Source: "first", Role: "user", Text: "one"})
	require.NotEqual(t, identity(ordered[0]), identity(reordered[0]))
}

func TestCanonicalizeModerationExtractionGolden(t *testing.T) {
	in := ModerationExtraction{Complete: true, Sources: []ModerationTextSource{
		{Source: "chat\x00separator", Role: " USER ", Text: "  Ａe\u0301\u200b\u200c\u200d\u2060\ufeff\u200e\t中😀  "},
		{Source: "tool.result", Role: " ToOl ", Text: "x\r\n\u00a0y"},
	}}
	got, err := CanonicalizeModerationExtraction(in)
	require.NoError(t, err)
	require.Equal(t, "Aé\u200e 中😀\nx y", got.Text)
	require.Equal(t, []CanonicalModerationSource{
		{Source: "chat\x00separator", Role: "user", Text: "Aé\u200e 中😀", StartRune: 0, EndRune: 6},
		{Source: "tool.result", Role: "tool", Text: "x y", StartRune: 7, EndRune: 10},
	}, got.Sources)
}

func TestCanonicalizeModerationExtractionRejectsInvalidUTF8(t *testing.T) {
	_, err := CanonicalizeModerationExtraction(ModerationExtraction{Complete: true, Sources: []ModerationTextSource{{Text: string([]byte{0xff})}}})
	require.ErrorIs(t, err, ErrInvalidModerationUTF8)
}

func TestModerationChunkGoldenContextAndStride(t *testing.T) {
	stream, err := CanonicalizeModerationExtraction(ModerationExtraction{Complete: true, Sources: []ModerationTextSource{
		{Source: "m", Role: "user", Text: strings.Repeat("界", 1700)},
		{Source: "t", Role: "tool", Text: strings.Repeat("😀", 1800)},
	}})
	require.NoError(t, err)
	chunks, err := PlanModerationChunks(stream)
	require.NoError(t, err)
	require.Len(t, chunks, 3)
	require.Equal(t, [][2]int{{0, 1800}, {1600, 3400}, {3200, 3501}}, [][2]int{{chunks[0].StartRune, chunks[0].EndRune}, {chunks[1].StartRune, chunks[1].EndRune}, {chunks[2].StartRune, chunks[2].EndRune}})
	require.LessOrEqual(t, chunks[1].RuneCount, ModerationChunkMaxRunes)
	require.Equal(t, "6368756e6b2d636f6e746578742d7631020475736572016d00a40d04746f6f6c01740063", hex.EncodeToString(chunks[0].ContextFrame))
	require.Equal(t, chunks[0].Text, chunks[0].NormalizedText)
}

func TestModerationChunkBoundariesAndBudget(t *testing.T) {
	for _, n := range []int{0, 1, 1800, 1801, 2000, 3400} {
		stream, err := CanonicalizeModerationExtraction(ModerationExtraction{Complete: true, Sources: []ModerationTextSource{{Source: "s", Role: "user", Text: strings.Repeat("e\u0301", n)}}})
		require.NoError(t, err)
		chunks, err := PlanModerationChunks(stream)
		require.NoError(t, err)
		expected := 0
		if n > 0 {
			expected = (n-1)/ModerationChunkStride + 1
		}
		require.Len(t, chunks, expected)
	}
	over := ModerationChunkStride*ModerationChunkMaxCount + 1
	stream, err := CanonicalizeModerationExtraction(ModerationExtraction{Complete: true, Sources: []ModerationTextSource{{Text: strings.Repeat("x", over)}}})
	require.NoError(t, err)
	_, err = PlanModerationChunks(stream)
	require.ErrorIs(t, err, ErrModerationChunkBudget)
}
