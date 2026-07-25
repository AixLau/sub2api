package service

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

type responsesExtractionSourceView struct {
	Source string
	Role   string
	Text   string
}

func TestContentModerationResponsesExtractionMixedShapeGolden(t *testing.T) {
	const ambient = `<in-app-browser-context source="ambient-ui-state">browser state</in-app-browser-context>`
	body := []byte(`{"input":[
		{"type":"message","role":"user","content":[
			{"type":"input_text","text":"alpha"},
			{"type":"future_text","text":"beta"},
			{"type":"input_image","image_url":"https://example.test/a.png"},
			{"type":"input_file","filename":"notes.txt","mime_type":"text/plain","file_id":"file_nested","file_data":"PAYLOAD_MUST_NOT_APPEAR","data":"DATA_MUST_NOT_APPEAR","content":"CONTENT_MUST_NOT_APPEAR"}
		]},
		{"type":"message","role":"assistant","content":[{"type":"refusal","refusal":"declined"}]},
		{"type":"message","role":"user","content":"alpha beta notes.txt text/plain file_nested"},
		{"type":"tool_result","content":"alpha beta notes.txt text/plain file_nested"},
		{"type":"message","role":"user","content":` + strconv.Quote(ambient) + `}
	]}`)

	got := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body, ContentModerationAuditScopeAllContext)

	require.False(t, got.Truncated, got.TruncateReasons)
	require.True(t, got.Extraction.Complete)
	require.Equal(t, []string{"https://example.test/a.png"}, got.Images)
	require.Equal(t, strings.Join([]string{
		"alpha beta notes.txt text/plain file_nested",
		"declined",
		"alpha beta notes.txt text/plain file_nested",
		ambient,
	}, " "), got.Text)
	require.Equal(t, []responsesExtractionSourceView{
		{Source: "responses.input[0].role=user.content", Role: "user", Text: "alpha beta notes.txt text/plain file_nested"},
		{Source: "responses.input[1].role=assistant.content", Role: "assistant", Text: "declined"},
		{Source: "responses.input[3].tool_result", Role: "tool", Text: "alpha beta notes.txt text/plain file_nested"},
		{Source: "responses.input[4].ambient_ui_state", Role: "context", Text: ambient},
	}, responsesLegacySourceViews(got.Sources))
	require.Equal(t, []responsesExtractionSourceView{
		{Source: "responses.input[0].role=user.content", Role: "user", Text: "alpha\nbeta\nnotes.txt\ntext/plain\nfile_nested"},
		{Source: "responses.input[1].role=assistant.content", Role: "assistant", Text: "declined"},
		{Source: "responses.input[2].role=user.content", Role: "user", Text: "alpha beta notes.txt text/plain file_nested"},
		{Source: "responses.input[3].tool_result", Role: "tool", Text: "alpha beta notes.txt text/plain file_nested"},
		{Source: "responses.input[4].ambient_ui_state", Role: "context", Text: ambient},
	}, responsesExtractionSourceViews(got.Extraction.Sources))
	for _, excluded := range []string{"PAYLOAD_MUST_NOT_APPEAR", "DATA_MUST_NOT_APPEAR", "CONTENT_MUST_NOT_APPEAR"} {
		require.NotContains(t, got.Text, excluded)
	}
}

func TestContentModerationResponsesExtractionFileMetadataWhitelistGolden(t *testing.T) {
	body := []byte(`{"input":[{
		"type":"input_file",
		"filename":"first.pdf",
		"file_name":"second.pdf",
		"mime_type":"application/pdf",
		"mimeType":"application/x-pdf",
		"file_id":"file_123",
		"file_data":"FILE_DATA_MUST_NOT_APPEAR",
		"data":"DATA_MUST_NOT_APPEAR",
		"content":"CONTENT_MUST_NOT_APPEAR"
	}]}`)

	got := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body, ContentModerationAuditScopeUserOnly)

	require.False(t, got.Truncated, got.TruncateReasons)
	require.Equal(t, "first.pdf second.pdf application/pdf application/x-pdf file_123", got.Text)
	require.Empty(t, got.Images)
	require.Equal(t, []responsesExtractionSourceView{{
		Source: "responses.input[0]",
		Role:   "user",
		Text:   "first.pdf second.pdf application/pdf application/x-pdf file_123",
	}}, responsesLegacySourceViews(got.Sources))
	require.Len(t, got.Extraction.Sources, 1)
	require.Equal(t, "first.pdf\nsecond.pdf\napplication/pdf\napplication/x-pdf\nfile_123", got.Extraction.Sources[0].Text)
	for _, excluded := range []string{"FILE_DATA_MUST_NOT_APPEAR", "DATA_MUST_NOT_APPEAR", "CONTENT_MUST_NOT_APPEAR"} {
		require.NotContains(t, got.Text, excluded)
		require.NotContains(t, got.Extraction.Sources[0].Text, excluded)
	}
}

func TestContentModerationResponsesExtractionToolFieldOrderGolden(t *testing.T) {
	body := []byte(`{"input":[
		{"type":"tool_result","content":{"content_key":"content-value"},"output":{"output_key":"output-value"}},
		{"type":"tool_call","arguments":"{\"argument_key\":\"argument-value\"}","input":{"input_key":"input-value"},"parameters":{"parameter_key":"parameter-value"}}
	]}`)

	got := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body, ContentModerationAuditScopeAllContext)

	require.False(t, got.Truncated, got.TruncateReasons)
	require.Equal(t, "content_key content-value output_key output-value argument_key argument-value input_key input-value parameter_key parameter-value", got.Text)
	require.Equal(t, []responsesExtractionSourceView{
		{Source: "responses.input[0].tool_result", Role: "tool", Text: "content_key content-value output_key output-value"},
		{Source: "responses.input[1]", Role: "assistant", Text: "argument_key argument-value input_key input-value parameter_key parameter-value"},
	}, responsesLegacySourceViews(got.Sources))
	require.Equal(t, []responsesExtractionSourceView{
		{Source: "responses.input[0].tool_result", Role: "tool", Text: "content_key\ncontent-value\noutput_key\noutput-value"},
		{Source: "responses.input[1]", Role: "assistant", Text: "argument_key\nargument-value\ninput_key\ninput-value\nparameter_key\nparameter-value"},
	}, responsesExtractionSourceViews(got.Extraction.Sources))
}

func TestContentModerationResponsesExtractionAuditScopesGolden(t *testing.T) {
	const ambient = `<in-app-browser-context source="ambient-ui-state">browser state</in-app-browser-context>`
	body := []byte(`{
		"instructions":"top instructions",
		"input":[
			{"type":"message","role":"system","content":"system context"},
			{"type":"message","role":"developer","content":"developer context"},
			{"type":"message","role":"assistant","content":"assistant context"},
			{"type":"function_call","arguments":"assistant-call"},
			{"type":"function_call_output","output":"tool output"},
			{"type":"message","role":"user","content":"user request"},
			{"type":"message","role":"user","content":` + strconv.Quote(ambient) + `}
		]
	}`)

	tests := []struct {
		name        string
		scope       string
		wantText    string
		wantSources []responsesExtractionSourceView
	}{
		{
			name:     "all context",
			scope:    ContentModerationAuditScopeAllContext,
			wantText: "top instructions system context developer context assistant context assistant-call tool output user request " + ambient,
			wantSources: []responsesExtractionSourceView{
				{Source: "responses.instructions", Role: "developer", Text: "top instructions"},
				{Source: "responses.input[0].role=system.content", Role: "system", Text: "system context"},
				{Source: "responses.input[1].role=developer.content", Role: "developer", Text: "developer context"},
				{Source: "responses.input[2].role=assistant.content", Role: "assistant", Text: "assistant context"},
				{Source: "responses.input[3]", Role: "assistant", Text: "assistant-call"},
				{Source: "responses.input[4].function_call_output", Role: "tool", Text: "tool output"},
				{Source: "responses.input[5].role=user.content", Role: "user", Text: "user request"},
				{Source: "responses.input[6].ambient_ui_state", Role: "context", Text: ambient},
			},
		},
		{
			name:     "user and tool",
			scope:    ContentModerationAuditScopeUserAndTool,
			wantText: "assistant-call tool output user request " + ambient,
			wantSources: []responsesExtractionSourceView{
				{Source: "responses.input[3]", Role: "assistant", Text: "assistant-call"},
				{Source: "responses.input[4].function_call_output", Role: "tool", Text: "tool output"},
				{Source: "responses.input[5].role=user.content", Role: "user", Text: "user request"},
				{Source: "responses.input[6].ambient_ui_state", Role: "context", Text: ambient},
			},
		},
		{
			name:     "user only",
			scope:    ContentModerationAuditScopeUserOnly,
			wantText: "user request " + ambient,
			wantSources: []responsesExtractionSourceView{
				{Source: "responses.input[5].role=user.content", Role: "user", Text: "user request"},
				{Source: "responses.input[6].ambient_ui_state", Role: "context", Text: ambient},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body, tt.scope)

			require.False(t, got.Truncated, got.TruncateReasons)
			require.True(t, got.Extraction.Complete)
			require.Equal(t, tt.wantText, got.Text)
			require.Equal(t, tt.wantSources, responsesLegacySourceViews(got.Sources))
			require.Equal(t, tt.wantSources, responsesExtractionSourceViews(got.Extraction.Sources))
		})
	}
}

func TestContentModerationResponsesExtractionLegacyDedupKeepsCompleteExtraction(t *testing.T) {
	longText := strings.Repeat("界", maxModerationInputRunes+5)
	body := []byte(`{"input":[
		{"type":"message","role":"user","content":"Repeat   Text"},
		{"type":"message","role":"user","content":"repeat text"},
		{"type":"function_call_output","output":"repeat text"},
		{"type":"message","role":"user","content":"` + longText + `"}
	]}`)

	got := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body, ContentModerationAuditScopeAllContext)

	require.False(t, got.Truncated, got.TruncateReasons)
	require.True(t, got.Extraction.Complete)
	require.Empty(t, got.Extraction.TruncateReasons)
	require.Len(t, got.Sources, 3)
	require.Equal(t, []responsesExtractionSourceView{
		{Source: "responses.input[0].role=user.content", Role: "user", Text: "Repeat Text"},
		{Source: "responses.input[2].function_call_output", Role: "tool", Text: "repeat text"},
		{Source: "responses.input[3].role=user.content", Role: "user", Text: strings.Repeat("界", maxModerationInputRunes)},
	}, responsesLegacySourceViews(got.Sources))
	require.True(t, got.Sources[2].Truncated)
	require.Equal(t, []string{"source_max_runes"}, got.Sources[2].TruncateReasons)
	require.Equal(t, maxModerationInputRunes, utf8.RuneCountInString(got.Text))

	require.Len(t, got.Extraction.Sources, 4)
	require.Equal(t, []responsesExtractionSourceView{
		{Source: "responses.input[0].role=user.content", Role: "user", Text: "Repeat   Text"},
		{Source: "responses.input[1].role=user.content", Role: "user", Text: "repeat text"},
		{Source: "responses.input[2].function_call_output", Role: "tool", Text: "repeat text"},
		{Source: "responses.input[3].role=user.content", Role: "user", Text: longText},
	}, responsesExtractionSourceViews(got.Extraction.Sources))
	require.False(t, got.Extraction.Sources[3].Truncated)
	require.Empty(t, got.Extraction.Sources[3].TruncateReasons)
	require.Equal(t,
		utf8.RuneCountInString("Repeat   Text")+
			utf8.RuneCountInString("repeat text")*2+
			utf8.RuneCountInString(longText),
		got.Extraction.TotalRunes,
	)
}

func responsesLegacySourceViews(sources []ContentModerationInputSource) []responsesExtractionSourceView {
	out := make([]responsesExtractionSourceView, 0, len(sources))
	for _, source := range sources {
		out = append(out, responsesExtractionSourceView{
			Source: source.Source,
			Role:   source.Role,
			Text:   source.Text,
		})
	}
	return out
}

func responsesExtractionSourceViews(sources []ModerationTextSource) []responsesExtractionSourceView {
	out := make([]responsesExtractionSourceView, 0, len(sources))
	for _, source := range sources {
		out = append(out, responsesExtractionSourceView{
			Source: source.Source,
			Role:   source.Role,
			Text:   source.Text,
		})
	}
	return out
}
