package service

import (
	"encoding/binary"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	ModerationChunkMaxRunes  = 1800
	ModerationChunkOverlap   = 200
	ModerationChunkStride    = ModerationChunkMaxRunes - ModerationChunkOverlap
	ModerationChunkMaxCount  = 64
	ModerationChunkerVersion = "zhipu-text-v1"
)

var (
	ErrInvalidModerationUTF8 = errors.New("invalid moderation UTF-8")
	ErrModerationChunkBudget = errors.New("moderation chunk budget exceeded")
)

type ModerationTextSource struct {
	Source          string
	Role            string
	Text            string
	Truncated       bool
	TruncateReasons []string
}

type ModerationExtraction struct {
	Sources         []ModerationTextSource
	Complete        bool
	TruncateReasons []string
	TotalRunes      int
}

type CanonicalModerationSource struct {
	Source    string
	Role      string
	Text      string
	StartRune int
	EndRune   int
}

type CanonicalModerationStream struct {
	Text     string
	Sources  []CanonicalModerationSource
	Complete bool
}

type ModerationChunk struct {
	Index          int
	Text           string
	NormalizedText string
	RuneCount      int
	StartRune      int
	EndRune        int
	SourceStart    int
	SourceEnd      int
	ContextFrame   []byte
}

func CanonicalizeModerationExtraction(in ModerationExtraction) (CanonicalModerationStream, error) {
	out := CanonicalModerationStream{Complete: in.Complete}
	parts := make([]string, 0, len(in.Sources))
	offset := 0
	for _, source := range in.Sources {
		if !utf8.ValidString(source.Text) || !utf8.ValidString(source.Role) || !utf8.ValidString(source.Source) {
			return CanonicalModerationStream{}, ErrInvalidModerationUTF8
		}
		text := canonicalizeModerationText(source.Text)
		if len(parts) > 0 {
			offset++
		}
		start := offset
		offset += utf8.RuneCountInString(text)
		parts = append(parts, text)
		out.Sources = append(out.Sources, CanonicalModerationSource{
			Source: source.Source, Role: strings.ToLower(strings.TrimSpace(source.Role)),
			Text: text, StartRune: start, EndRune: offset,
		})
	}
	out.Text = strings.Join(parts, "\n")
	return out, nil
}

func canonicalizeModerationText(text string) string {
	text = norm.NFKC.String(text)
	var b strings.Builder
	space := false
	for _, r := range text {
		switch r {
		case '\u200b', '\u200c', '\u200d', '\u2060', '\ufeff':
			continue
		}
		if unicode.IsSpace(r) {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return strings.Trim(b.String(), " ")
}

func PlanModerationChunks(stream CanonicalModerationStream) ([]ModerationChunk, error) {
	maxCovered := ModerationChunkMaxRunes + (ModerationChunkMaxCount-1)*ModerationChunkStride
	runes, err := boundedModerationRunes(stream.Text, maxCovered)
	if err != nil {
		return nil, err
	}
	if len(runes) == 0 {
		return nil, nil
	}
	count := 1
	if len(runes) > ModerationChunkMaxRunes {
		count += (len(runes) - ModerationChunkMaxRunes + ModerationChunkStride - 1) / ModerationChunkStride
	}
	if count > ModerationChunkMaxCount {
		return nil, ErrModerationChunkBudget
	}
	chunks := make([]ModerationChunk, 0, count)
	for index, start := 0, 0; start < len(runes); index, start = index+1, start+ModerationChunkStride {
		end := min(start+ModerationChunkMaxRunes, len(runes))
		spans, sourceStart, sourceEnd := overlappingModerationSourceSpans(stream.Sources, start, end)
		text := string(runes[start:end])
		chunks = append(chunks, ModerationChunk{Index: index, Text: text, NormalizedText: text, RuneCount: end - start, StartRune: start, EndRune: end, SourceStart: sourceStart, SourceEnd: sourceEnd, ContextFrame: encodeModerationChunkContext(spans)})
		if end == len(runes) {
			break
		}
	}
	return chunks, nil
}

func boundedModerationRunes(text string, limit int) ([]rune, error) {
	runes := make([]rune, 0, min(len(text), limit))
	for len(text) > 0 {
		r, size := utf8.DecodeRuneInString(text)
		if r == utf8.RuneError && size == 1 {
			return nil, ErrInvalidModerationUTF8
		}
		if len(runes) == limit {
			return nil, ErrModerationChunkBudget
		}
		runes = append(runes, r)
		text = text[size:]
	}
	return runes, nil
}

type moderationSourceSpan struct {
	role, source string
	start, end   int
}

func overlappingModerationSourceSpans(sources []CanonicalModerationSource, start, end int) ([]moderationSourceSpan, int, int) {
	spans := make([]moderationSourceSpan, 0, 2)
	first, last := -1, -1
	for index, source := range sources {
		spanStart := max(start, source.StartRune)
		spanEnd := min(end, source.EndRune)
		if spanStart >= spanEnd {
			continue
		}
		if first < 0 {
			first = index
		}
		last = index + 1
		spans = append(spans, moderationSourceSpan{source.Role, source.Source, spanStart - source.StartRune, spanEnd - source.StartRune})
	}
	return spans, first, last
}

func encodeModerationChunkContext(spans []moderationSourceSpan) []byte {
	frame := append([]byte(nil), "chunk-context-v1"...)
	frame = appendUvarint(frame, uint64(len(spans)))
	for _, span := range spans {
		frame = appendUvarint(frame, uint64(len(span.role)))
		frame = append(frame, span.role...)
		frame = appendUvarint(frame, uint64(len(span.source)))
		frame = append(frame, span.source...)
		frame = appendUvarint(frame, uint64(span.start))
		frame = appendUvarint(frame, uint64(span.end))
	}
	return frame
}

func appendUvarint(dst []byte, value uint64) []byte {
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(buf[:], value)
	return append(dst, buf[:n]...)
}
