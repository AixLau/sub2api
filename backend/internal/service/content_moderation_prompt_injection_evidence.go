package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/pkg/promptfilter"
)

const (
	contentModerationPromptInjectionEvidenceRevision = "prompt-injection-evidence-v2+" + promptfilter.DetectorRevision
	contentModerationPromptInjectionEvidenceMinRunes = 512
	contentModerationPromptInjectionEvidenceMaxCores = 512
)

// contentModerationPromptInjectionEvidenceInput is intentionally independent
// from contentModerationCandidateSelection. It lets the caller pass the active
// source from the complete extraction stream instead of the legacy 12K display
// projection when that source is available.
type contentModerationPromptInjectionEvidenceInput struct {
	SourceText     string
	Matches        []promptfilter.Match
	MaxRunes       int
	SourceComplete bool
}

type contentModerationPromptInjectionEvidence struct {
	Text           string
	Complete       bool
	Runes          int
	Revision       string
	Digest         string
	Windowed       bool
	WindowCount    int
	MatchesTotal   int
	MatchesCovered int
}

type contentModerationPromptInjectionEvidenceEnvelope struct {
	Kind           string                                           `json:"kind"`
	Revision       string                                           `json:"revision"`
	SourceRunes    int                                              `json:"source_runes"`
	Complete       bool                                             `json:"complete"`
	MatchesTotal   int                                              `json:"matches_total"`
	MatchesCovered int                                              `json:"matches_covered"`
	Windows        []contentModerationPromptInjectionEvidenceWindow `json:"windows"`
}

type contentModerationPromptInjectionEvidenceWindow struct {
	StartRune int      `json:"start_rune"`
	EndRune   int      `json:"end_rune"`
	Labels    []string `json:"labels"`
	Text      string   `json:"text"`
}

type contentModerationPromptInjectionEvidenceCore struct {
	Start       int
	End         int
	Count       int
	Weight      int
	Strict      bool
	Operational bool
}

type contentModerationPromptInjectionEvidenceInterval struct {
	Start   int
	End     int
	Head    bool
	Hit     bool
	Tail    bool
	Covered int
}

// buildContentModerationPromptInjectionEvidence returns the exact redacted
// active source while it fits. Larger sources are represented as one valid JSON
// value containing head, every coverable prompt-injection occurrence, and tail
// windows. The serialized JSON is measured after redaction and escaping; it is
// never cut with trimRunes.
func buildContentModerationPromptInjectionEvidence(input contentModerationPromptInjectionEvidenceInput) (contentModerationPromptInjectionEvidence, error) {
	maxRunes := input.MaxRunes
	if maxRunes <= 0 || maxRunes > maxModerationInputRunes {
		maxRunes = maxModerationInputRunes
	}
	if maxRunes < contentModerationPromptInjectionEvidenceMinRunes {
		return contentModerationPromptInjectionEvidence{}, errors.New("prompt-injection evidence budget is below the minimum valid envelope size")
	}

	sourceText := input.SourceText
	sourceComplete := input.SourceComplete
	if !utf8.ValidString(sourceText) {
		sourceText = strings.ToValidUTF8(sourceText, "�")
		sourceComplete = false
	}
	if strings.TrimSpace(sourceText) == "" {
		return contentModerationPromptInjectionEvidence{}, errors.New("prompt-injection evidence source is empty")
	}

	sourceRunes := []rune(sourceText)
	cores, matchesTotal, invalidMatches, omittedByCoreLimit := contentModerationPromptInjectionEvidenceCores(sourceText, input.Matches)
	validMatches := matchesTotal - invalidMatches
	rawSpansComplete := invalidMatches == 0

	redactedFullSource := redactContentModerationSecrets(sourceText)
	if len([]rune(redactedFullSource)) <= maxRunes {
		complete := sourceComplete && rawSpansComplete
		return newContentModerationPromptInjectionEvidence(
			redactedFullSource,
			complete,
			false,
			1,
			matchesTotal,
			validMatches,
		), nil
	}

	// Context and edge sizes are reduced as a unit. Every attempt includes all
	// located occurrences; only the final overflow fallback is allowed to omit a
	// hit, and that fallback is always marked incomplete.
	attempts := []struct {
		context int
		edge    int
	}{
		{context: 320, edge: 768},
		{context: 256, edge: 512},
		{context: 160, edge: 384},
		{context: 96, edge: 256},
		{context: 48, edge: 160},
		{context: 16, edge: 96},
		{context: 0, edge: 64},
	}
	for _, attempt := range attempts {
		intervals := contentModerationPromptInjectionEvidenceIntervals(len(sourceRunes), cores, attempt.context, attempt.edge)
		complete := sourceComplete && rawSpansComplete && !omittedByCoreLimit && matchesTotal > 0
		text, windowCount, covered, err := marshalContentModerationPromptInjectionEvidenceEnvelope(
			sourceRunes,
			intervals,
			complete,
			matchesTotal,
			contentModerationPromptInjectionEvidenceCovered(cores),
		)
		if err != nil {
			return contentModerationPromptInjectionEvidence{}, err
		}
		if len([]rune(text)) <= maxRunes {
			return newContentModerationPromptInjectionEvidence(text, complete, true, windowCount, matchesTotal, covered), nil
		}
	}

	// Keyword flooding can make the exact hit spans themselves exceed 12K.
	// Preserve both source edges, then admit strict/operational/high-weight cores
	// in priority order while keeping one valid JSON value. Any omitted core makes
	// the result incomplete, so a semantic allow cannot become a final allow.
	prioritized := append([]contentModerationPromptInjectionEvidenceCore(nil), cores...)
	sort.SliceStable(prioritized, func(i, j int) bool {
		if prioritized[i].Operational != prioritized[j].Operational {
			return prioritized[i].Operational
		}
		if prioritized[i].Strict != prioritized[j].Strict {
			return prioritized[i].Strict
		}
		if prioritized[i].Weight != prioritized[j].Weight {
			return prioritized[i].Weight > prioritized[j].Weight
		}
		return prioritized[i].Start < prioritized[j].Start
	})
	selected := make([]contentModerationPromptInjectionEvidenceCore, 0, len(prioritized))
	for _, core := range prioritized {
		candidate := append(selected, core)
		intervals := contentModerationPromptInjectionEvidenceIntervals(len(sourceRunes), candidate, 0, 64)
		text, _, _, err := marshalContentModerationPromptInjectionEvidenceEnvelope(sourceRunes, intervals, false, matchesTotal, contentModerationPromptInjectionEvidenceCovered(candidate))
		if err != nil {
			return contentModerationPromptInjectionEvidence{}, err
		}
		if len([]rune(text)) <= maxRunes {
			selected = candidate
		}
	}
	intervals := contentModerationPromptInjectionEvidenceIntervals(len(sourceRunes), selected, 0, 64)
	text, windowCount, covered, err := marshalContentModerationPromptInjectionEvidenceEnvelope(
		sourceRunes,
		intervals,
		false,
		matchesTotal,
		contentModerationPromptInjectionEvidenceCovered(selected),
	)
	if err != nil {
		return contentModerationPromptInjectionEvidence{}, err
	}
	if len([]rune(text)) > maxRunes {
		return contentModerationPromptInjectionEvidence{}, errors.New("prompt-injection evidence metadata exceeds the configured budget")
	}
	return newContentModerationPromptInjectionEvidence(text, false, true, windowCount, matchesTotal, covered), nil
}

func newContentModerationPromptInjectionEvidence(text string, complete, windowed bool, windowCount, matchesTotal, matchesCovered int) contentModerationPromptInjectionEvidence {
	digest := sha256.Sum256([]byte(text))
	return contentModerationPromptInjectionEvidence{
		Text:           text,
		Complete:       complete,
		Runes:          len([]rune(text)),
		Revision:       contentModerationPromptInjectionEvidenceRevision,
		Digest:         hex.EncodeToString(digest[:]),
		Windowed:       windowed,
		WindowCount:    windowCount,
		MatchesTotal:   matchesTotal,
		MatchesCovered: matchesCovered,
	}
}

func contentModerationPromptInjectionEvidenceCores(text string, matches []promptfilter.Match) ([]contentModerationPromptInjectionEvidenceCore, int, int, bool) {
	byteOffsets := make([]int, 0, utf8.RuneCountInString(text)+1)
	for byteOffset := range text {
		byteOffsets = append(byteOffsets, byteOffset)
	}
	byteOffsets = append(byteOffsets, len(text))

	type spanKey struct {
		Start int
		End   int
	}
	coreBySpan := make(map[spanKey]int)
	cores := make([]contentModerationPromptInjectionEvidenceCore, 0)
	matchesTotal := 0
	invalidMatches := 0
	omittedByCoreLimit := false
	for _, match := range matches {
		if !promptfilter.IsPromptInjectionMatch(match) {
			continue
		}
		matchesTotal++
		startRune, startOK := exactPromptInjectionEvidenceRuneOffset(byteOffsets, match.StartByte)
		endRune, endOK := exactPromptInjectionEvidenceRuneOffset(byteOffsets, match.EndByte)
		if !startOK || !endOK || endRune <= startRune {
			invalidMatches++
			continue
		}
		key := spanKey{Start: startRune, End: endRune}
		if idx, exists := coreBySpan[key]; exists {
			core := &cores[idx]
			core.Count++
			if match.Weight > core.Weight {
				core.Weight = match.Weight
			}
			core.Strict = core.Strict || match.Strict
			core.Operational = core.Operational || match.Operational
			continue
		}
		if len(cores) >= contentModerationPromptInjectionEvidenceMaxCores {
			omittedByCoreLimit = true
			continue
		}
		coreBySpan[key] = len(cores)
		cores = append(cores, contentModerationPromptInjectionEvidenceCore{
			Start:       startRune,
			End:         endRune,
			Count:       1,
			Weight:      match.Weight,
			Strict:      match.Strict,
			Operational: match.Operational,
		})
	}
	sort.Slice(cores, func(i, j int) bool {
		if cores[i].Start == cores[j].Start {
			return cores[i].End < cores[j].End
		}
		return cores[i].Start < cores[j].Start
	})
	return cores, matchesTotal, invalidMatches, omittedByCoreLimit
}

func exactPromptInjectionEvidenceRuneOffset(byteOffsets []int, byteOffset int) (int, bool) {
	idx := sort.SearchInts(byteOffsets, byteOffset)
	return idx, idx < len(byteOffsets) && byteOffsets[idx] == byteOffset
}

func contentModerationPromptInjectionEvidenceIntervals(sourceRunes int, cores []contentModerationPromptInjectionEvidenceCore, contextRunes, edgeRunes int) []contentModerationPromptInjectionEvidenceInterval {
	if sourceRunes <= 0 {
		return nil
	}
	if edgeRunes < 1 {
		edgeRunes = 1
	}
	intervals := []contentModerationPromptInjectionEvidenceInterval{
		{Start: 0, End: minInt(sourceRunes, edgeRunes), Head: true},
		{Start: maxInt(0, sourceRunes-edgeRunes), End: sourceRunes, Tail: true},
	}
	for _, core := range cores {
		intervals = append(intervals, contentModerationPromptInjectionEvidenceInterval{
			Start:   maxInt(0, core.Start-contextRunes),
			End:     minInt(sourceRunes, core.End+contextRunes),
			Hit:     true,
			Covered: core.Count,
		})
	}
	sort.Slice(intervals, func(i, j int) bool {
		if intervals[i].Start == intervals[j].Start {
			return intervals[i].End < intervals[j].End
		}
		return intervals[i].Start < intervals[j].Start
	})
	merged := make([]contentModerationPromptInjectionEvidenceInterval, 0, len(intervals))
	for _, interval := range intervals {
		if interval.End <= interval.Start {
			continue
		}
		if len(merged) == 0 || interval.Start > merged[len(merged)-1].End {
			merged = append(merged, interval)
			continue
		}
		current := &merged[len(merged)-1]
		if interval.End > current.End {
			current.End = interval.End
		}
		current.Head = current.Head || interval.Head
		current.Hit = current.Hit || interval.Hit
		current.Tail = current.Tail || interval.Tail
		current.Covered += interval.Covered
	}
	return merged
}

func marshalContentModerationPromptInjectionEvidenceEnvelope(source []rune, intervals []contentModerationPromptInjectionEvidenceInterval, complete bool, matchesTotal, matchesCovered int) (string, int, int, error) {
	windows := make([]contentModerationPromptInjectionEvidenceWindow, 0, len(intervals))
	covered := 0
	for _, interval := range intervals {
		if interval.Start < 0 || interval.End > len(source) || interval.End <= interval.Start {
			continue
		}
		labels := make([]string, 0, 3)
		if interval.Head {
			labels = append(labels, "head")
		}
		if interval.Hit {
			labels = append(labels, "hit")
		}
		if interval.Tail {
			labels = append(labels, "tail")
		}
		covered += interval.Covered
		windows = append(windows, contentModerationPromptInjectionEvidenceWindow{
			StartRune: interval.Start,
			EndRune:   interval.End,
			Labels:    labels,
			Text:      redactContentModerationSecrets(string(source[interval.Start:interval.End])),
		})
	}
	if covered < matchesCovered {
		complete = false
	}
	envelope := contentModerationPromptInjectionEvidenceEnvelope{
		Kind:           contentModerationReviewKindPromptInjection,
		Revision:       contentModerationPromptInjectionEvidenceRevision,
		SourceRunes:    len(source),
		Complete:       complete,
		MatchesTotal:   matchesTotal,
		MatchesCovered: covered,
		Windows:        windows,
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return "", 0, 0, err
	}
	return string(payload), len(windows), covered, nil
}

func contentModerationPromptInjectionEvidenceCovered(cores []contentModerationPromptInjectionEvidenceCore) int {
	covered := 0
	for _, core := range cores {
		covered += core.Count
	}
	return covered
}
