package service

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestContentModerationResponsesViewCompatDuplicateAndEscapedKeysUseFirstMatch(t *testing.T) {
	tests := []struct {
		name  string
		raw   string
		path  string
		field responsesObjectField
		want  string
	}{
		{
			name:  "plain duplicate",
			raw:   `{"text":"first","text":"second"}`,
			path:  "text",
			field: responsesFieldText,
			want:  "first",
		},
		{
			name:  "escaped key before plain duplicate",
			raw:   `{"te\u0078t":"escaped first","text":"plain second"}`,
			path:  "text",
			field: responsesFieldText,
			want:  "escaped first",
		},
		{
			name:  "plain key before escaped duplicate",
			raw:   `{"type":"message","ty\u0070e":"input_text"}`,
			path:  "type",
			field: responsesFieldType,
			want:  "message",
		},
		{
			name:  "escaped nested keys",
			raw:   `{"image_\u0075rl":{"u\u0072l":"https://example.com/escaped.png"}}`,
			path:  "image_url.url",
			field: responsesFieldImageURLURL,
			want:  "https://example.com/escaped.png",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			object := gjson.Parse(tt.raw)
			wantResult := object.Get(tt.path)
			view := newResponsesObjectView(object)
			gotResult := view.get(tt.field)

			require.True(t, wantResult.Exists())
			require.Equal(t, tt.want, wantResult.String())
			require.Equal(t, wantResult.Exists(), gotResult.Exists())
			require.Equal(t, wantResult.Type, gotResult.Type)
			require.Equal(t, wantResult.Raw, gotResult.Raw)
			require.Equal(t, wantResult.String(), gotResult.String())
		})
	}
}

func TestContentModerationResponsesViewCompatNestedPathsAcrossDuplicateContainers(t *testing.T) {
	tests := []struct {
		name  string
		raw   string
		path  string
		field responsesObjectField
		want  string
	}{
		{
			name:  "image url skips non-object container",
			raw:   `{"image_url":null,"image_url":{"url":"https://example.com/later.png"}}`,
			path:  "image_url.url",
			field: responsesFieldImageURLURL,
			want:  "https://example.com/later.png",
		},
		{
			name:  "image url nested null wins",
			raw:   `{"image_url":{"url":null},"image_url":{"url":"https://example.com/ignored.png"}}`,
			path:  "image_url.url",
			field: responsesFieldImageURLURL,
			want:  "",
		},
		{
			name:  "source url skips null container",
			raw:   `{"source":null,"source":{"url":"https://example.com/source.png"}}`,
			path:  "source.url",
			field: responsesFieldSourceURL,
			want:  "https://example.com/source.png",
		},
		{
			name:  "source fields come from separate containers",
			raw:   `{"source":{"media_type":"image/png"},"source":{"data":"aGVsbG8="}}`,
			path:  "source.data",
			field: responsesFieldSourceData,
			want:  "aGVsbG8=",
		},
		{
			name:  "source nested null wins",
			raw:   `{"source":{"data":null},"source":{"data":"ignored"}}`,
			path:  "source.data",
			field: responsesFieldSourceData,
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			object := gjson.Parse(tt.raw)
			wantResult := object.Get(tt.path)
			view := newResponsesObjectView(object)
			gotResult := view.get(tt.field)

			require.True(t, wantResult.Exists())
			require.Equal(t, tt.want, wantResult.String())
			require.Equal(t, wantResult.Exists(), gotResult.Exists())
			require.Equal(t, wantResult.Type, gotResult.Type)
			require.Equal(t, wantResult.Raw, gotResult.Raw)
			require.Equal(t, wantResult.String(), gotResult.String())
		})
	}
}

func TestContentModerationResponsesViewCompatNullAndFalseContentAreIncomplete(t *testing.T) {
	for _, value := range []string{"null", "false"} {
		t.Run(value, func(t *testing.T) {
			body := []byte(`{"input":[{"type":"message","role":"user","content":` + value + `}]}`)

			got := ExtractContentModerationInput(
				ContentModerationProtocolOpenAIResponses,
				body,
				ContentModerationAuditScopeUserOnly,
			)

			require.True(t, got.Truncated)
			require.Contains(t, got.TruncateReasons, "unsupported_required_value")
			require.False(t, got.Extraction.Complete)
			require.Empty(t, got.Text)
			require.Empty(t, got.Images)
			require.Empty(t, got.Sources)
		})
	}
}

func TestContentModerationResponsesViewCompatKeepsTypeSpecificValidation(t *testing.T) {
	tests := []struct {
		name  string
		block string
	}{
		{name: "refusal requires refusal", block: `{"type":"refusal","text":"fallback"}`},
		{name: "tool use requires object input", block: `{"type":"tool_use","name":"lookup","input":[]}`},
		{name: "input image requires image url", block: `{"type":"input_image","url":"https://example.com/image.png"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(`{"input":[{"type":"message","role":"user","content":[` + tt.block + `]}]}`)

			got := ExtractContentModerationInput(
				ContentModerationProtocolOpenAIResponses,
				body,
				ContentModerationAuditScopeAllContext,
			)

			require.True(t, got.Truncated)
			require.Contains(t, got.TruncateReasons, "unsupported_required_value")
			require.False(t, got.Extraction.Complete)
		})
	}
}

func TestContentModerationResponsesViewCompatInvalidMediaKeepsBestEffortImages(t *testing.T) {
	body := []byte(`{"input":[{"type":"message","role":"user","content":[
		{"type":"input_image","image_url":123},
		{"type":"image","url":"https://example.com/fallback.png","media_type":123,"data":false},
		{"type":"image","source":{"media_type":true,"data":123}}
	]}]}`)

	got := ExtractContentModerationInput(
		ContentModerationProtocolOpenAIResponses,
		body,
		ContentModerationAuditScopeUserOnly,
	)

	require.True(t, got.Truncated)
	require.Contains(t, got.TruncateReasons, "unsupported_required_value")
	require.False(t, got.Extraction.Complete)
	require.Equal(t, []string{
		"https://example.com/fallback.png",
		"data:123;base64,false",
		"data:true;base64,123",
	}, got.Images)
}

func TestContentModerationResponsesViewCompatReverseJSONOrderKeepsProjectionOrder(t *testing.T) {
	body := []byte(`{"input":[{
		"content":"nested content",
		"refusal":"refusal text",
		"text":"direct text",
		"file_id":"file_123",
		"mimeType":"application/x-alt",
		"mime_type":"application/pdf",
		"file_name":"second.pdf",
		"filename":"first.pdf",
		"role":"user",
		"type":"message"
	}]}`)

	got := ExtractContentModerationInput(
		ContentModerationProtocolOpenAIResponses,
		body,
		ContentModerationAuditScopeUserOnly,
	)

	require.False(t, got.Truncated, got.TruncateReasons)
	require.Equal(t,
		"nested content first.pdf second.pdf application/pdf application/x-alt file_123 first.pdf second.pdf application/pdf application/x-alt file_123 direct text refusal text nested content",
		got.Text,
	)
	require.Len(t, got.Extraction.Sources, 1)
	require.Equal(t,
		"nested content\nfirst.pdf\nsecond.pdf\napplication/pdf\napplication/x-alt\nfile_123\nfirst.pdf\nsecond.pdf\napplication/pdf\napplication/x-alt\nfile_123\ndirect text\nrefusal text\nnested content",
		got.Extraction.Sources[0].Text,
	)
}

func TestContentModerationResponsesViewCompatEscapedTextIsDecodedOnce(t *testing.T) {
	body := []byte(`{"input":[{"type":"message","role":"user","content":[{
		"type":"input_text",
		"te\u0078t":"line\nquote\" slash\\ unicode\u4e2d emoji\ud83d\ude00"
	}]}]}`)

	got := ExtractContentModerationInput(
		ContentModerationProtocolOpenAIResponses,
		body,
		ContentModerationAuditScopeUserOnly,
	)

	require.False(t, got.Truncated, got.TruncateReasons)
	require.Equal(t, `line quote" slash\ unicode中 emoji😀`, got.Text)
	require.Len(t, got.Extraction.Sources, 1)
	require.Equal(t, "line\nquote\" slash\\ unicode中 emoji😀", got.Extraction.Sources[0].Text)
}

func TestContentModerationResponsesViewCompatLargeEscapedFilePayloadIsExcluded(t *testing.T) {
	escapedPayload := strings.Repeat(`\u4e2d`, 64*1024)
	body := []byte(`{"input":[{
		"file_data":"` + escapedPayload + `",
		"data":"` + escapedPayload + `",
		"content":"` + escapedPayload + `",
		"filename":"report.pdf",
		"file_name":"report-copy.pdf",
		"mime_type":"application/pdf",
		"mimeType":"application/x-pdf",
		"file_id":"file_large",
		"type":"input_file"
	}]}`)

	assertExtraction := func() {
		got := ExtractContentModerationInput(
			ContentModerationProtocolOpenAIResponses,
			body,
			ContentModerationAuditScopeUserOnly,
		)
		require.False(t, got.Truncated, got.TruncateReasons)
		require.Equal(t, "report.pdf report-copy.pdf application/pdf application/x-pdf file_large", got.Text)
		require.NotContains(t, got.Text, "中")
		require.Empty(t, got.Images)
		require.Len(t, got.Extraction.Sources, 1)
		require.NotContains(t, got.Extraction.Sources[0].Text, "中")
	}

	assertExtraction()
	allocs := testing.AllocsPerRun(5, assertExtraction)
	require.LessOrEqual(t, allocs, float64(128), "large excluded file payload allocated %.0f objects per extraction", allocs)
}

func TestContentModerationResponsesViewCompatValidationReasonsUseExtractionSource(t *testing.T) {
	const ambient = `<in-app-browser-context source="ambient-ui-state">browser state</in-app-browser-context>`
	tests := []struct {
		name       string
		body       string
		wantSource string
		wantRole   string
	}{
		{
			name:       "role tool ambient wrapper",
			body:       `{"input":[{"role":"tool","content":` + strconv.Quote(ambient) + `,"output":42}]}`,
			wantSource: "responses.input[0].ambient_ui_state",
			wantRole:   "context",
		},
		{
			name:       "file metadata with malformed excluded content",
			body:       `{"input":[{"type":"input_file","filename":"report.pdf","content":[` + strconv.Quote(ambient) + `,false]}]}`,
			wantSource: "responses.input[0]",
			wantRole:   "user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractContentModerationInput(
				ContentModerationProtocolOpenAIResponses,
				[]byte(tt.body),
				ContentModerationAuditScopeAllContext,
			)

			require.True(t, got.Truncated)
			require.Contains(t, got.TruncateReasons, "unsupported_required_value")
			require.Len(t, got.Sources, 1)
			require.Equal(t, tt.wantSource, got.Sources[0].Source)
			require.Equal(t, tt.wantRole, got.Sources[0].Role)
			require.True(t, got.Sources[0].Truncated)
			require.Contains(t, got.Sources[0].TruncateReasons, "unsupported_required_value")
		})
	}
}

func TestContentModerationResponsesViewCompatFileWithToolRoleScansToolPayload(t *testing.T) {
	body := []byte(`{"input":[{
		"type":"input_file",
		"role":"tool",
		"filename":"must-not-replace-tool-payload.txt",
		"content":"tool content",
		"output":"tool output"
	}]}`)

	for _, scope := range []string{
		ContentModerationAuditScopeAllContext,
		ContentModerationAuditScopeUserAndTool,
	} {
		t.Run(scope, func(t *testing.T) {
			got := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body, scope)

			require.False(t, got.Truncated, got.TruncateReasons)
			require.Equal(t, "tool content tool output", got.Text)
			require.Len(t, got.Extraction.Sources, 1)
			require.Equal(t, "responses.input[0].role=tool.content", got.Extraction.Sources[0].Source)
			require.Equal(t, "tool", got.Extraction.Sources[0].Role)
			require.Equal(t, "tool content\ntool output", got.Extraction.Sources[0].Text)
		})
	}

	userOnly := ExtractContentModerationInput(
		ContentModerationProtocolOpenAIResponses,
		body,
		ContentModerationAuditScopeUserOnly,
	)
	require.False(t, userOnly.Truncated, userOnly.TruncateReasons)
	require.Empty(t, userOnly.Text)
	require.Empty(t, userOnly.Sources)
}

func TestContentModerationResponsesViewCompatFileToolAmbientIgnoresDirectText(t *testing.T) {
	const ambient = `<in-app-browser-context source="ambient-ui-state">browser state</in-app-browser-context>`
	body := []byte(`{"input":[{
		"type":"input_file",
		"role":"tool",
		"content":` + strconv.Quote(ambient) + `,
		"text":"must not change file ambient attribution"
	}]}`)

	got := ExtractContentModerationInput(
		ContentModerationProtocolOpenAIResponses,
		body,
		ContentModerationAuditScopeAllContext,
	)

	require.False(t, got.Truncated, got.TruncateReasons)
	require.Equal(t, ambient, got.Text)
	require.Len(t, got.Extraction.Sources, 1)
	require.Equal(t, "responses.input[0].ambient_ui_state", got.Extraction.Sources[0].Source)
	require.Equal(t, "context", got.Extraction.Sources[0].Role)
	require.Equal(t, ambient, got.Extraction.Sources[0].Text)
}

func TestContentModerationResponsesViewCompatEncodedToolArgumentsFallbackToRawTextPastDepthLimit(t *testing.T) {
	arguments := strings.Repeat("[", maxResponsesJSONDepth+1) + `"deep leaf"` + strings.Repeat("]", maxResponsesJSONDepth+1)
	body := []byte(`{"input":[{"type":"function_call","arguments":` + strconv.Quote(arguments) + `}]}`)

	got := ExtractContentModerationInput(
		ContentModerationProtocolOpenAIResponses,
		body,
		ContentModerationAuditScopeAllContext,
	)

	require.True(t, got.Truncated)
	require.Contains(t, got.TruncateReasons, "max_depth")
	require.False(t, got.Extraction.Complete)
	require.Contains(t, got.Text, "deep leaf")
	require.Len(t, got.Sources, 1)
	require.Contains(t, got.Sources[0].Text, "deep leaf")
}

func TestContentModerationToolArgumentsPastDepthLimitRetainStructuredBestEffort(t *testing.T) {
	const deepPadDepth = 10_001
	deepPad := strings.Repeat("[", deepPadDepth) + "0" + strings.Repeat("]", deepPadDepth)
	arguments := `{"base64":"YmxvY2tlZCBwaHJhc2U=","image_url":"https://example.test/unsafe.png","pad":` + deepPad + `}`
	tests := []struct {
		name     string
		protocol string
		body     []byte
	}{
		{
			name:     "responses",
			protocol: ContentModerationProtocolOpenAIResponses,
			body:     []byte(`{"input":[{"type":"function_call","arguments":` + strconv.Quote(arguments) + `}]}`),
		},
		{
			name:     "chat completions",
			protocol: ContentModerationProtocolOpenAIChat,
			body: []byte(`{"messages":[{"role":"assistant","tool_calls":[{"type":"function","function":{"name":"lookup","arguments":` +
				strconv.Quote(arguments) + `}}]}]}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractContentModerationInput(tt.protocol, tt.body, ContentModerationAuditScopeAllContext)

			require.True(t, got.Truncated)
			require.Contains(t, got.TruncateReasons, "max_depth")
			require.False(t, got.Extraction.Complete)
			require.Contains(t, got.Text, "blocked phrase")
			require.Equal(t, []string{"https://example.test/unsafe.png"}, got.Images)
		})
	}
}

func TestContentModerationToolArgumentsDeepMediaArrayReportsMaxDepth(t *testing.T) {
	images := `"https://example.test/deep.png"`
	for range maxToolResultTextDepth + 2 {
		images = `[` + images + `]`
	}
	arguments := `{"images":` + images + `}`
	body := []byte(`{"input":[{"type":"function_call","arguments":` + strconv.Quote(arguments) + `}]}`)

	got := ExtractContentModerationInput(
		ContentModerationProtocolOpenAIResponses,
		body,
		ContentModerationAuditScopeAllContext,
	)

	require.True(t, got.Truncated)
	require.Contains(t, got.TruncateReasons, "max_depth")
	require.False(t, got.Extraction.Complete)
}

func TestContentModerationToolArgumentsRepeatedLargeSuffixStopsAtScanBudget(t *testing.T) {
	pad := strings.Repeat("[", 12) + strings.Repeat(" ", 1<<20) + "0" + strings.Repeat("]", 12)
	arguments := `{"base64":"YmxvY2tlZCBwaHJhc2U=","pad":` + pad + `}`
	body := []byte(`{"input":[{"type":"function_call","arguments":` + strconv.Quote(arguments) + `}]}`)

	got := ExtractContentModerationInput(
		ContentModerationProtocolOpenAIResponses,
		body,
		ContentModerationAuditScopeAllContext,
	)

	require.True(t, got.Truncated)
	require.Contains(t, got.TruncateReasons, "max_scan_work")
	require.NotContains(t, got.TruncateReasons, "max_depth")
	require.False(t, got.Extraction.Complete)
	require.Contains(t, got.Text, "blocked phrase")
}

func TestContentModerationToolArgumentsWithUnbalancedBracketsRemainAuditable(t *testing.T) {
	arguments := strings.Repeat("[", maxResponsesJSONDepth+1) + "blocked phrase"
	tests := []struct {
		name     string
		protocol string
		body     []byte
	}{
		{
			name:     "responses",
			protocol: ContentModerationProtocolOpenAIResponses,
			body:     []byte(`{"input":[{"type":"function_call","arguments":` + strconv.Quote(arguments) + `}]}`),
		},
		{
			name:     "chat completions",
			protocol: ContentModerationProtocolOpenAIChat,
			body: []byte(`{"messages":[{"role":"assistant","tool_calls":[{"type":"function","function":{"name":"lookup","arguments":` +
				strconv.Quote(arguments) + `}}]}]}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractContentModerationInput(tt.protocol, tt.body, ContentModerationAuditScopeAllContext)

			require.True(t, got.Truncated)
			require.Contains(t, got.TruncateReasons, "max_depth")
			require.False(t, got.Extraction.Complete)
			require.Contains(t, got.Text, "blocked phrase")
		})
	}
}

func TestContentModerationResponsesViewCompatDeepContentFailsClosedAndCapsExtraction(t *testing.T) {
	tests := []struct {
		name string
		wrap func(string) string
	}{
		{
			name: "objects",
			wrap: func(content string) string {
				return `{"type":"message","content":` + content + `}`
			},
		},
		{
			name: "arrays",
			wrap: func(content string) string {
				return `[` + content + `]`
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := `"deep leaf"`
			for range maxResponsesContentDepth + 2 {
				content = tt.wrap(content)
			}
			body := []byte(`{"input":[{"type":"message","role":"user","content":` + content + `}]}`)

			got := ExtractContentModerationInput(
				ContentModerationProtocolOpenAIResponses,
				body,
				ContentModerationAuditScopeAllContext,
			)

			require.True(t, got.Truncated)
			require.Contains(t, got.TruncateReasons, "max_depth")
			require.False(t, got.Extraction.Complete)
			require.Empty(t, got.Text)
			require.Empty(t, got.Images)
			require.Empty(t, got.Sources)
		})
	}
}

func TestContentModerationResponsesViewCompatContentAtDepthLimitRemainsComplete(t *testing.T) {
	content := `"deep leaf"`
	for range maxResponsesContentDepth {
		content = `{"type":"message","content":` + content + `}`
	}
	body := []byte(`{"input":[{"type":"message","role":"user","content":` + content + `}]}`)

	got := ExtractContentModerationInput(
		ContentModerationProtocolOpenAIResponses,
		body,
		ContentModerationAuditScopeAllContext,
	)

	require.False(t, got.Truncated, got.TruncateReasons)
	require.True(t, got.Extraction.Complete)
	require.Equal(t, "deep leaf", got.Text)
}

func TestContentModerationResponsesViewCompatNestedLargeSuffixKeepsPrefixAndReportsScanBudget(t *testing.T) {
	content := `{"type":"input_text","text":"deep tail","padding":"` + strings.Repeat("x", 1<<20) + `"}`
	for range 12 {
		content = `{"type":"message","content":` + content + `}`
	}
	body := []byte(`{"input":[{"type":"input_text","role":"user","text":"prefix marker","content":` + content + `}]}`)

	got := ExtractContentModerationInput(
		ContentModerationProtocolOpenAIResponses,
		body,
		ContentModerationAuditScopeAllContext,
	)

	require.True(t, got.Truncated)
	require.Contains(t, got.TruncateReasons, "max_scan_work")
	require.False(t, got.Extraction.Complete)
	require.Equal(t, "prefix marker", got.Text)
	require.Len(t, got.Extraction.Sources, 1)
	require.Equal(t, "responses.input[0].role=user.content", got.Extraction.Sources[0].Source)
	require.Equal(t, "user", got.Extraction.Sources[0].Role)
	require.True(t, got.Extraction.Sources[0].Truncated)
	require.Contains(t, got.Extraction.Sources[0].TruncateReasons, "max_scan_work")
}

func TestContentModerationResponsesViewCompatUserOnlySkippedTreeDoesNotConsumeScanBudget(t *testing.T) {
	content := `{"type":"input_text","text":"assistant tail","padding":"` + strings.Repeat("x", 1<<20) + `"}`
	for range 12 {
		content = `{"type":"message","content":` + content + `}`
	}
	body := []byte(`{"input":[` +
		`{"type":"message","role":"assistant","content":` + content + `},` +
		`{"type":"message","role":"user","content":"included user marker"}` +
		`]}`)

	got := ExtractContentModerationInput(
		ContentModerationProtocolOpenAIResponses,
		body,
		ContentModerationAuditScopeUserOnly,
	)

	require.False(t, got.Truncated, got.TruncateReasons)
	require.True(t, got.Extraction.Complete)
	require.Equal(t, "included user marker", got.Text)
}

func TestContentModerationResponsesViewCompatExtremeJSONDepthRejectedBeforeValidation(t *testing.T) {
	body := strings.Repeat("[", maxResponsesJSONDepth+1) + `"deep leaf"` + strings.Repeat("]", maxResponsesJSONDepth+1)

	got := ExtractContentModerationInput(
		ContentModerationProtocolOpenAIResponses,
		[]byte(body),
		ContentModerationAuditScopeAllContext,
	)

	require.True(t, got.Truncated)
	require.Equal(t, []string{"max_depth"}, got.TruncateReasons)
	require.False(t, got.Extraction.Complete)
	require.Empty(t, got.Text)
	require.Empty(t, got.Sources)
}

func TestContentModerationResponsesViewCompatDeepUnknownEnvelopeReportsMaxDepth(t *testing.T) {
	content := `"deep leaf"`
	for range maxResponsesContentDepth + 2 {
		content = `{"type":"message","content":` + content + `}`
	}
	body := []byte(`{"input":[{"type":"future_message","role":"user","content":` + content + `}]}`)

	got := ExtractContentModerationInput(
		ContentModerationProtocolOpenAIResponses,
		body,
		ContentModerationAuditScopeAllContext,
	)

	require.True(t, got.Truncated)
	require.Contains(t, got.TruncateReasons, "max_depth")
	require.NotContains(t, got.TruncateReasons, "unsupported_required_value")
	require.False(t, got.Extraction.Complete)
	require.Empty(t, got.Text)
	require.Empty(t, got.Sources)
}

func TestContentModerationResponsesValidationDeduplicatesRepeatedPrimitiveEvents(t *testing.T) {
	const invalidValues = 10_000
	body := []byte(`{"input":[{"type":"message","role":"user","content":[` +
		strings.Repeat("0,", invalidValues-1) + `0]}]}`)
	state := &toolResultTextState{}
	root := gjson.ParseBytes(body)
	rootView := newResponsesRootView(root, ContentModerationAuditScopeAllContext)
	scan := newModerationScanBudget(len(body), state)

	validateModerationProtocolShape(
		ContentModerationProtocolOpenAIResponses,
		root,
		&rootView,
		ContentModerationAuditScopeAllContext,
		state,
		scan,
	)

	require.True(t, state.truncated)
	require.Equal(t, []string{"unsupported_required_value"}, state.truncationEventReasons)
	require.Equal(t, 1, state.truncationEvents)
	require.Equal(t, invalidValues, state.truncationReasonCount("unsupported_required_value"))
	require.Equal(
		t,
		[]string{"unsupported_required_value"},
		state.validationReasons["responses.input[0].role=user.content"],
	)
}

func TestContentModerationResponsesValidationSourceResolverReasonsUseBothAliases(t *testing.T) {
	const ambient = `<in-app-browser-context source="ambient-ui-state">browser state</in-app-browser-context>`
	item := gjson.Parse(`{"type":"future_message","role":"user","content":` + strconv.Quote(ambient) + `}`)
	state := &toolResultTextState{}

	state.withLazyValidationSources(func() []string {
		sources := responsesValidationItemSources("0", item, "future_message", "user", nil)
		state.markTruncated("max_scan_work")
		return sources
	}, func() {
		state.markTruncated("unsupported_required_value")
	})

	for _, source := range []string{
		"responses.input[0].role=user.content",
		"responses.input[0].ambient_ui_state",
	} {
		require.Contains(t, state.validationReasons[source], "unsupported_required_value")
		require.Contains(t, state.validationReasons[source], "max_scan_work")
	}
}

func TestContentModerationResponsesValidationScanExhaustionAliasesAmbientSource(t *testing.T) {
	const ambient = `<in-app-browser-context source="ambient-ui-state">browser state</in-app-browser-context>`
	content := strconv.Quote(ambient)
	for range 3 {
		content = `{"type":"message","content":` + strings.Repeat(" ", 1<<20) + content + `}`
	}
	body := []byte(`{"input":[{"type":"future_message","role":"user","content":` + content + `}]}`)

	got := ExtractContentModerationInput(
		ContentModerationProtocolOpenAIResponses,
		body,
		ContentModerationAuditScopeAllContext,
	)

	require.True(t, got.Truncated)
	require.Contains(t, got.TruncateReasons, "max_scan_work")
	require.NotContains(t, got.TruncateReasons, "unsupported_required_value")
	require.Equal(t, ambient, got.Text)
	require.Len(t, got.Sources, 1)
	require.Equal(t, "responses.input[0].ambient_ui_state", got.Sources[0].Source)
	require.Equal(t, "context", got.Sources[0].Role)
	require.True(t, got.Sources[0].Truncated)
	require.Contains(t, got.Sources[0].TruncateReasons, "max_scan_work")
}

func TestContentModerationResponsesRepeatedDeepSiblingsDoNotAddUnsupportedReason(t *testing.T) {
	deep := `0`
	for range maxResponsesContentDepth + 2 {
		deep = `{"type":"future_block","content":` + deep + `}`
	}
	body := []byte(`{"input":[{"type":"future_message","role":"user","content":[` + deep + `,` + deep + `]}]}`)

	got := ExtractContentModerationInput(
		ContentModerationProtocolOpenAIResponses,
		body,
		ContentModerationAuditScopeAllContext,
	)

	require.True(t, got.Truncated)
	require.Contains(t, got.TruncateReasons, "max_depth")
	require.NotContains(t, got.TruncateReasons, "unsupported_required_value")
}

func TestContentModerationResponsesMalformedOverDepthToolArgumentsDoNotPanic(t *testing.T) {
	deepPadding := strings.Repeat("[", maxResponsesJSONDepth+1) + `0` + strings.Repeat("]", maxResponsesJSONDepth+1)
	for _, suffix := range []string{`,"text":"`, `,"`} {
		inner := `{"pad":` + deepPadding + suffix
		body := []byte(`{"input":[{"type":"function_call","arguments":` + strconv.Quote(inner) + `}]}`)
		var got ContentModerationInput

		require.NotPanics(t, func() {
			got = ExtractContentModerationInput(
				ContentModerationProtocolOpenAIResponses,
				body,
				ContentModerationAuditScopeAllContext,
			)
		})
		require.True(t, got.Truncated)
		require.Contains(t, got.TruncateReasons, "max_depth")
		require.Contains(t, got.Text, inner)
	}
}

func TestContentModerationResponsesCapsManyTinyContentStrings(t *testing.T) {
	const values = maxResponsesContentStrings * 2
	content := strings.Repeat(`"a",`, values-1) + `"a"`
	body := []byte(`{"input":[{"type":"message","role":"user","content":[` + content + `]}]}`)

	got := ExtractContentModerationInput(
		ContentModerationProtocolOpenAIResponses,
		body,
		ContentModerationAuditScopeAllContext,
	)

	require.True(t, got.Truncated)
	require.Contains(t, got.TruncateReasons, "max_strings")
	require.Len(t, got.Extraction.Sources, 1)
	require.True(t, got.Extraction.Sources[0].Truncated)
	require.Contains(t, got.Extraction.Sources[0].TruncateReasons, "max_strings")
	require.LessOrEqual(t, len(got.Extraction.Sources[0].Text), maxResponsesContentStrings*2)
}

func TestContentModerationResponsesCapsOverDepthProjectionImages(t *testing.T) {
	var inner strings.Builder
	inner.WriteByte('[')
	for index := 0; index < maxModerationCollectedImages+64; index++ {
		if index > 0 {
			inner.WriteByte(',')
		}
		inner.WriteString(`{"image_url":"https://example.com/`)
		inner.WriteString(strconv.Itoa(index))
		inner.WriteString(`.png"}`)
	}
	inner.WriteString(`,{"pad":`)
	inner.WriteString(strings.Repeat("[", maxResponsesJSONDepth+1))
	inner.WriteByte('0')
	inner.WriteString(strings.Repeat("]", maxResponsesJSONDepth+1))
	inner.WriteString(`}]`)
	body := []byte(`{"input":[{"type":"function_call","arguments":` + strconv.Quote(inner.String()) + `}]}`)

	got := ExtractContentModerationInput(
		ContentModerationProtocolOpenAIResponses,
		body,
		ContentModerationAuditScopeAllContext,
	)

	require.True(t, got.Truncated)
	require.Contains(t, got.TruncateReasons, "max_depth")
	require.Contains(t, got.TruncateReasons, "max_images")
	require.Len(t, got.Images, maxModerationCollectedImages)
}

func TestContentModerationResponsesCapsMalformedItemValidationState(t *testing.T) {
	const items = maxResponsesScanNodes + 1024
	item := `{"type":"message","role":"user","content":0},`
	body := []byte(`{"input":[` + strings.Repeat(item, items-1) + strings.TrimSuffix(item, ",") + `]}`)
	state := &toolResultTextState{}
	root := gjson.ParseBytes(body)
	rootView := newResponsesRootView(root, ContentModerationAuditScopeAllContext)
	scan := newModerationScanBudget(len(body), state)

	validateModerationProtocolShape(
		ContentModerationProtocolOpenAIResponses,
		root,
		&rootView,
		ContentModerationAuditScopeAllContext,
		state,
		scan,
	)

	require.True(t, scan.exhausted)
	require.Contains(t, state.truncateReasons, "max_scan_work")
	require.LessOrEqual(t, len(state.truncationEventReasons), maxResponsesScanNodes)
	require.LessOrEqual(t, len(state.validationReasons), maxResponsesScanNodes)
}

func TestContentModerationResponsesSharesImageLimitAcrossToolAndUserContent(t *testing.T) {
	var toolContent strings.Builder
	toolContent.WriteByte('[')
	for index := 0; index < maxModerationCollectedImages; index++ {
		if index > 0 {
			toolContent.WriteByte(',')
		}
		toolContent.WriteString(`{"image_url":"https://example.com/tool-`)
		toolContent.WriteString(strconv.Itoa(index))
		toolContent.WriteString(`.png"}`)
	}
	toolContent.WriteByte(']')
	body := []byte(`{"input":[` +
		`{"type":"tool_result","content":` + toolContent.String() + `},` +
		`{"type":"message","role":"user","content":[{"type":"input_image","image_url":"https://example.com/user.png"}]}` +
		`]}`)

	got := ExtractContentModerationInput(
		ContentModerationProtocolOpenAIResponses,
		body,
		ContentModerationAuditScopeAllContext,
	)

	require.True(t, got.Truncated)
	require.Contains(t, got.TruncateReasons, "max_images")
	require.Len(t, got.Images, maxModerationCollectedImages)
	require.NotContains(t, got.Images, "https://example.com/user.png")
}

func TestContentModerationResponsesImageLimitCountsUniqueValues(t *testing.T) {
	duplicate := `{"type":"input_image","image_url":"https://example.com/repeated.png"},`
	content := strings.Repeat(duplicate, maxModerationCollectedImages+64) +
		`{"type":"input_image","image_url":"https://example.com/unique.png"}`
	body := []byte(`{"input":[{"type":"message","role":"user","content":[` + content + `]}]}`)

	got := ExtractContentModerationInput(
		ContentModerationProtocolOpenAIResponses,
		body,
		ContentModerationAuditScopeAllContext,
	)

	require.False(t, got.Truncated, got.TruncateReasons)
	require.Equal(t, []string{
		"https://example.com/repeated.png",
		"https://example.com/unique.png",
	}, got.Images)
}

func TestContentModerationResponsesSharesTextLimitAcrossToolAndUserContent(t *testing.T) {
	toolContent := strings.Repeat(`"tool",`, maxResponsesContentStrings-1) + `"tool"`
	body := []byte(`{"input":[` +
		`{"type":"tool_result","content":[` + toolContent + `]},` +
		`{"type":"message","role":"user","content":"user marker beyond limit"}` +
		`]}`)

	got := ExtractContentModerationInput(
		ContentModerationProtocolOpenAIResponses,
		body,
		ContentModerationAuditScopeAllContext,
	)

	require.True(t, got.Truncated)
	require.Contains(t, got.TruncateReasons, "max_strings")
	require.NotContains(t, got.Text, "user marker beyond limit")
}

func TestContentModerationToolObjectOverflowStillProjectsAfterSkippedMedia(t *testing.T) {
	state := &toolResultTextState{objectKeys: maxToolResultObjectKeys - 1}
	value := gjson.Parse(`{"image_url":"https://example.com/skipped.png","risk":"danger marker"}`)
	var parts []string
	var images []string

	collectToolResultTextValue(value, &parts, &images, 0, state)

	require.Contains(t, state.truncateReasons, "max_object_keys")
	require.Contains(t, strings.Join(parts, "\n"), "risk")
	require.Contains(t, strings.Join(parts, "\n"), "danger marker")
	require.Equal(t, []string{"https://example.com/skipped.png"}, images)
}

func TestContentModerationImageDataChecksDuplicateAndLimitBeforeFormatting(t *testing.T) {
	duplicate := moderationImageDataKey{mediaType: "image/png", data: "QUFBQQ=="}
	state := &toolResultTextState{
		images:       maxModerationCollectedImages,
		imageDataSet: map[moderationImageDataKey]struct{}{duplicate: {}},
	}
	var images []string

	require.True(t, state.addImageData(&images, duplicate.mediaType, duplicate.data))
	require.False(t, state.truncated)
	require.Empty(t, images)

	require.False(t, state.addImageData(&images, "image/png", "QkJCQg=="))
	require.True(t, state.truncated)
	require.Contains(t, state.truncateReasons, "max_images")
	require.Empty(t, images)
}
