package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
)

var (
	contentModerationBenchmarkInputSink ContentModerationInput
	contentModerationBenchmarkRuleSink  ContentModerationKeywordRule
	contentModerationBenchmarkBoolSink  bool
)

func BenchmarkContentModerationNoHit(b *testing.B) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.EngineMode = ContentModerationEngineModeCandidateOnly
	cfg.PromptFilterMode = "observe"
	cfg.SemanticReview.PromptInjectionReviewerEnabled = true
	cfg.SemanticReview.PromptInjectionMaxInputRunes = maxModerationInputRunes
	body, err := json.Marshal(map[string]any{
		"messages": []map[string]string{{
			"role":    "user",
			"content": strings.Repeat("ordinary product documentation paragraph. ", 40),
		}},
	})
	if err != nil {
		b.Fatal(err)
	}
	content := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)
	if _, hit := contentModerationCandidateSelectionForInput(cfg, content); hit {
		b.Fatal("unexpected prompt-injection candidate in no-hit fixture")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, contentModerationBenchmarkBoolSink = contentModerationCandidateSelectionForInput(cfg, content)
	}
}

func TestValidateContentModerationBenchmarkExtraction(t *testing.T) {
	tests := []struct {
		name         string
		input        ContentModerationInput
		expectedText string
		wantErr      bool
	}{
		{name: "expected text", input: ContentModerationInput{Text: "ordinary expected text"}, expectedText: "expected text"},
		{name: "empty", input: ContentModerationInput{}, expectedText: "expected text", wantErr: true},
		{name: "wrong text", input: ContentModerationInput{Text: "different text"}, expectedText: "expected text", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateContentModerationBenchmarkExtraction(tt.input, tt.expectedText)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validate extraction error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateContentModerationBenchmarkDecision(t *testing.T) {
	allow := &ContentModerationDecision{Allowed: true, Action: ContentModerationActionAllow}
	tests := []struct {
		name     string
		decision *ContentModerationDecision
		err      error
		wantErr  bool
	}{
		{name: "allow", decision: allow},
		{name: "check error", decision: allow, err: errors.New("check failed"), wantErr: true},
		{name: "nil", wantErr: true},
		{name: "not allowed", decision: &ContentModerationDecision{Action: ContentModerationActionAllow}, wantErr: true},
		{name: "blocked", decision: &ContentModerationDecision{Allowed: true, Blocked: true, Action: ContentModerationActionAllow}, wantErr: true},
		{name: "flagged", decision: &ContentModerationDecision{Allowed: true, Flagged: true, Action: ContentModerationActionAllow}, wantErr: true},
		{name: "non-allow action", decision: &ContentModerationDecision{Allowed: true, Action: ContentModerationActionBlock}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateContentModerationBenchmarkDecision(tt.decision, tt.err)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validate decision error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func validateContentModerationBenchmarkExtraction(input ContentModerationInput, expectedText string) error {
	if input.IsEmpty() {
		return errors.New("extraction returned empty moderation input")
	}
	if !strings.Contains(input.Text, expectedText) {
		return fmt.Errorf("extraction omitted expected text %q", expectedText)
	}
	return nil
}

func validateContentModerationBenchmarkDecision(decision *ContentModerationDecision, err error) error {
	if err != nil {
		return fmt.Errorf("moderation check failed: %w", err)
	}
	if decision == nil {
		return errors.New("moderation check returned nil decision")
	}
	if !decision.Allowed || decision.Blocked || decision.Flagged || decision.Action != ContentModerationActionAllow {
		return fmt.Errorf("unexpected moderation decision: allowed=%t blocked=%t flagged=%t action=%q", decision.Allowed, decision.Blocked, decision.Flagged, decision.Action)
	}
	return nil
}

func BenchmarkContentModerationExtraction(b *testing.B) {
	fixtures := []struct {
		name         string
		protocol     string
		body         []byte
		expectedText string
	}{
		{
			name:         "SmallText",
			protocol:     ContentModerationProtocolOpenAIChat,
			body:         []byte(`{"messages":[{"role":"user","content":"summarize this release note"}]}`),
			expectedText: "summarize this release note",
		},
		{
			name:         "MultiMessage1MiB",
			protocol:     ContentModerationProtocolOpenAIChat,
			body:         contentModerationBenchmarkMultiMessageBody(1 << 20),
			expectedText: "ordinary benchmark message content",
		},
	}
	for _, fixture := range fixtures {
		fixture := fixture
		b.Run(fixture.name, func(b *testing.B) {
			input := ExtractContentModerationInput(fixture.protocol, fixture.body)
			if err := validateContentModerationBenchmarkExtraction(input, fixture.expectedText); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(fixture.body)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				contentModerationBenchmarkInputSink = ExtractContentModerationInput(fixture.protocol, fixture.body)
			}
			b.ReportMetric(float64(len(fixture.body)), "input-bytes/op")
		})
	}
}

func BenchmarkContentModerationRuleMatching(b *testing.B) {
	text := strings.Repeat("safe ", maxModerationInputRunes/5)
	rules := make([]ContentModerationKeywordRule, maxContentModerationBlockedKeywords)
	for i := range rules {
		rules[i] = ContentModerationKeywordRule{
			Keyword:  fmt.Sprintf("forbidden-%05d", i),
			Category: ContentModerationKeywordCategoryCustom,
			Severity: ContentModerationKeywordSeverityHigh,
			Action:   ContentModerationKeywordActionBlock,
			Enabled:  true,
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var hit bool
		contentModerationBenchmarkRuleSink, hit = matchContentModerationKeyword(text, rules)
		if hit {
			b.Fatal("unexpected keyword match")
		}
	}
}

func BenchmarkContentModerationRequestSizeRejection(b *testing.B) {
	const configuredLimit = int64(1 << 20)
	body := bytes.Repeat([]byte{'x'}, int(configuredLimit+1))
	b.ReportAllocs()
	b.SetBytes(int64(len(body)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := pkghttputil.NormalizeLenientJSONRequestBody(body, configuredLimit); err == nil {
			b.Fatal("expected configured request-size rejection")
		}
	}
}

func BenchmarkContentModerationConcurrentChecks(b *testing.B) {
	originalLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError})))
	b.Cleanup(func() { slog.SetDefault(originalLogger) })

	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.EngineMode = ContentModerationEngineModeRuleOnly
	cfg.KeywordBlockingMode = ContentModerationKeywordModeKeywordOnly
	cfg.KeywordRules = []ContentModerationKeywordRule{{
		Keyword:  "blocked benchmark phrase",
		Category: ContentModerationKeywordCategoryCustom,
		Severity: ContentModerationKeywordSeverityHigh,
		Action:   ContentModerationKeywordActionBlock,
		Enabled:  true,
	}}
	rawConfig, err := json.Marshal(cfg)
	if err != nil {
		b.Fatal(err)
	}
	svc := &ContentModerationService{
		settingRepo: &contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawConfig),
		}},
		repo:       &contentModerationTestRepo{},
		httpClient: &http.Client{},
		asyncQueue: make(chan contentModerationTask, 1),
		keyHealth:  make(map[string]*contentModerationKeyHealth),
	}
	input := ContentModerationCheckInput{
		Endpoint: "/v1/responses",
		Protocol: ContentModerationProtocolOpenAIResponses,
		Body:     []byte(`{"input":"summarize this ordinary benchmark request"}`),
	}
	decision, checkErr := svc.Check(context.Background(), input)
	if err := validateContentModerationBenchmarkDecision(decision, checkErr); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	var failed atomic.Bool
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			decision, err := svc.Check(context.Background(), input)
			if err != nil || decision == nil {
				failed.Store(true)
			}
		}
	})
	if failed.Load() {
		b.Fatal("concurrent moderation check failed")
	}
}

func contentModerationBenchmarkMultiMessageBody(minimumBytes int) []byte {
	const messageText = "ordinary benchmark message content "
	var body strings.Builder
	body.Grow(minimumBytes + 4096)
	body.WriteString(`{"messages":[`)
	for i := 0; body.Len() < minimumBytes; i++ {
		if i > 0 {
			body.WriteByte(',')
		}
		body.WriteString(`{"role":"user","content":"`)
		body.WriteString(strings.Repeat(messageText, 128))
		body.WriteString(`"}`)
	}
	body.WriteString(`]}`)
	return []byte(body.String())
}
