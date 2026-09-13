package service

import "strings"

// narrowContentModerationInputToLatestTurn keeps the current user turn and the
// nearest preceding assistant/model output. The extractor retains message
// sources as role boundaries and this pass removes every unselected role.
// Images remain on the input because ContentModerationInput does not
// attribute media to individual sources; dropping them would silently skip
// image moderation for the current request.
func narrowContentModerationInputToLatestTurn(input ContentModerationInput) ContentModerationInput {
	sources := make([]ContentModerationInputSource, 0, len(input.Extraction.Sources))
	if len(input.Extraction.Sources) > 0 {
		for _, source := range input.Extraction.Sources {
			sources = append(sources, ContentModerationInputSource{
				Source:          source.Source,
				Role:            source.Role,
				Text:            source.Text,
				Truncated:       source.Truncated,
				TruncateReasons: append([]string(nil), source.TruncateReasons...),
			})
		}
	} else {
		sources = append(sources, input.Sources...)
	}
	if len(sources) == 0 {
		return input
	}

	latestUser := -1
	for index := len(sources) - 1; index >= 0; index-- {
		if contentModerationLatestTurnUserRole(sources[index].Role) {
			latestUser = index
			break
		}
	}
	if latestUser < 0 {
		// Preserve the established behavior for unusual payloads without a user
		// turn instead of manufacturing an empty moderation input.
		return input
	}

	userIndexes := latestContentModerationUserTurnIndexes(sources, latestUser)
	firstUser := userIndexes[0]
	selected := make([]ContentModerationInputSource, 0, len(userIndexes)+2)
	for _, index := range userIndexes {
		selected = append(selected, sources[index])
	}

	for index := firstUser - 1; index >= 0; index-- {
		if !contentModerationLatestTurnAssistantRole(sources[index].Role) {
			continue
		}
		start := index
		for start > 0 && contentModerationLatestTurnAssistantRole(sources[start-1].Role) {
			start--
		}
		selected = append(selected, sources[start:index+1]...)
		break
	}

	input.Sources = append([]ContentModerationInputSource(nil), selected...)
	input.Text = legacyModerationTextFromParts(contentModerationSourceTexts(selected))
	input.Normalize()
	input.Extraction.Sources = make([]ModerationTextSource, 0, len(input.Sources))
	input.Extraction.TotalRunes = 0
	for _, source := range input.Sources {
		input.Extraction.Sources = append(input.Extraction.Sources, ModerationTextSource{
			Source:          source.Source,
			Role:            source.Role,
			Text:            source.Text,
			Truncated:       source.Truncated,
			TruncateReasons: append([]string(nil), source.TruncateReasons...),
		})
		input.Extraction.TotalRunes += len([]rune(source.Text))
	}
	return input
}

func latestContentModerationUserTurnIndexes(sources []ContentModerationInputSource, latest int) []int {
	turn := semanticReviewSplitUserTurn(sources[latest].Source)
	if turn == "" {
		start := latest
		for start > 0 && contentModerationLatestTurnUserRole(sources[start-1].Role) {
			start--
		}
		indexes := make([]int, 0, latest-start+1)
		for index := start; index <= latest; index++ {
			indexes = append(indexes, index)
		}
		return indexes
	}
	indexes := make([]int, 0, 2)
	for index, source := range sources {
		if !contentModerationLatestTurnUserRole(source.Role) || semanticReviewSplitUserTurn(source.Source) != turn {
			continue
		}
		indexes = append(indexes, index)
	}
	if len(indexes) == 0 {
		return []int{latest}
	}
	return indexes
}

func contentModerationLatestTurnUserRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "", "user":
		return true
	default:
		return false
	}
}

func contentModerationLatestTurnAssistantRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "assistant", "model":
		return true
	default:
		return false
	}
}

func contentModerationSourceTexts(sources []ContentModerationInputSource) []string {
	texts := make([]string, 0, len(sources))
	for _, source := range sources {
		texts = append(texts, source.Text)
	}
	return texts
}
