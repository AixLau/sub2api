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
	contentModerationBenchmarkRulesSink []ContentModerationKeywordRule
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
	prepared := newContentModerationPreparedRuleSet(rules)
	b.ReportAllocs()
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var hit bool
		contentModerationBenchmarkRuleSink, hit = prepared.Match(text)
		if hit {
			b.Fatal("unexpected keyword match")
		}
	}
}

func TestContentModerationPreparedRuleSetMatchesAllocations(t *testing.T) {
	tests := []struct {
		name      string
		prepare   func() (*contentModerationPreparedRuleSet, string)
		wantCount int
		maxAllocs float64
		allocRuns int
	}{
		{
			name: "miss 10k",
			prepare: func() (*contentModerationPreparedRuleSet, string) {
				return newContentModerationPreparedRuleSet(contentModerationBenchmarkUnrelatedRules(10_000)),
					strings.Repeat("ordinary content ", maxModerationInputRunes/17)
			},
			maxAllocs: 0,
			allocRuns: 100,
		},
		{
			name: "single hit 10k",
			prepare: func() (*contentModerationPreparedRuleSet, string) {
				rules := contentModerationBenchmarkUnrelatedRules(10_000)
				rules[0].Keyword = "needle"
				return newContentModerationPreparedRuleSet(rules),
					"needle " + strings.Repeat("ordinary content ", maxModerationInputRunes/17)
			},
			wantCount: 1,
			maxAllocs: 1,
			allocRuns: 100,
		},
		{
			name: "multiple hits",
			prepare: func() (*contentModerationPreparedRuleSet, string) {
				rules := []ContentModerationKeywordRule{
					{Keyword: "gamma", Enabled: true},
					{Keyword: "alpha", Enabled: true},
					{Keyword: "beta", Enabled: true},
				}
				return newContentModerationPreparedRuleSet(rules), "alpha beta gamma"
			},
			wantCount: 3,
			maxAllocs: 1,
			allocRuns: 100,
		},
		{
			name: "compact collision 10k",
			prepare: func() (*contentModerationPreparedRuleSet, string) {
				return newContentModerationPreparedRuleSet(contentModerationCompactCollisionRules(10_000)),
					"abcdefghijklmno"
			},
			wantCount: 10_000,
			maxAllocs: 2,
			allocRuns: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prepared, text := tt.prepare()
			if matches := prepared.Matches(text); len(matches) != tt.wantCount {
				t.Fatalf("Matches() count = %d, want %d", len(matches), tt.wantCount)
			}
			allocs := testing.AllocsPerRun(tt.allocRuns, func() {
				contentModerationBenchmarkRulesSink = prepared.Matches(text)
			})
			if allocs > tt.maxAllocs {
				t.Fatalf("Matches() allocations = %.0f, want <= %.0f", allocs, tt.maxAllocs)
			}
		})
	}
}

func BenchmarkContentModerationPreparedRuleSetMatches(b *testing.B) {
	benchmarks := []struct {
		name      string
		prepare   func() (*contentModerationPreparedRuleSet, string)
		wantCount int
	}{
		{
			name: "Miss10K",
			prepare: func() (*contentModerationPreparedRuleSet, string) {
				return newContentModerationPreparedRuleSet(contentModerationBenchmarkUnrelatedRules(10_000)),
					strings.Repeat("ordinary content ", maxModerationInputRunes/17)
			},
		},
		{
			name: "SingleHit10K",
			prepare: func() (*contentModerationPreparedRuleSet, string) {
				rules := contentModerationBenchmarkUnrelatedRules(10_000)
				rules[0].Keyword = "needle"
				return newContentModerationPreparedRuleSet(rules),
					"needle " + strings.Repeat("ordinary content ", maxModerationInputRunes/17)
			},
			wantCount: 1,
		},
		{
			name: "MultipleHits",
			prepare: func() (*contentModerationPreparedRuleSet, string) {
				rules := []ContentModerationKeywordRule{
					{Keyword: "gamma", Enabled: true},
					{Keyword: "alpha", Enabled: true},
					{Keyword: "beta", Enabled: true},
				}
				return newContentModerationPreparedRuleSet(rules), "alpha beta gamma"
			},
			wantCount: 3,
		},
		{
			name: "CompactCollision10K",
			prepare: func() (*contentModerationPreparedRuleSet, string) {
				return newContentModerationPreparedRuleSet(contentModerationCompactCollisionRules(10_000)),
					"abcdefghijklmno"
			},
			wantCount: 10_000,
		},
	}

	for _, benchmark := range benchmarks {
		b.Run(benchmark.name, func(b *testing.B) {
			prepared, text := benchmark.prepare()
			if matches := prepared.Matches(text); len(matches) != benchmark.wantCount {
				b.Fatalf("Matches() count = %d, want %d", len(matches), benchmark.wantCount)
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(text)))
			b.ResetTimer()
			for range b.N {
				contentModerationBenchmarkRulesSink = prepared.Matches(text)
			}
		})
	}
}

func contentModerationBenchmarkUnrelatedRules(count int) []ContentModerationKeywordRule {
	rules := make([]ContentModerationKeywordRule, count)
	for index := range rules {
		rules[index] = ContentModerationKeywordRule{
			Keyword: fmt.Sprintf("unrelated%05d", index), Enabled: true,
		}
	}
	return rules
}

func BenchmarkContentModerationKeywordFalseCandidate10K(b *testing.B) {
	rules := make([]ContentModerationKeywordRule, maxContentModerationBlockedKeywords)
	rules[0] = ContentModerationKeywordRule{Keyword: "apikey", Enabled: true}
	for index := 1; index < len(rules); index++ {
		rules[index] = ContentModerationKeywordRule{
			Keyword: fmt.Sprintf("unrelated%05d", index), Enabled: true,
		}
	}
	prepared := newContentModerationPreparedRuleSet(rules)
	text := strings.Repeat("xapi key ", maxModerationInputRunes/9)
	if _, hit := prepared.Match(text); hit {
		b.Fatal("unexpected keyword match")
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for range b.N {
		contentModerationBenchmarkRuleSink, contentModerationBenchmarkBoolSink = prepared.Match(text)
	}
}

func BenchmarkContentModerationKeywordCompactCollision10K(b *testing.B) {
	prepared := newContentModerationPreparedRuleSet(contentModerationCompactCollisionRules(10_000))
	text := strings.Repeat("xabcdefghijklmno ", maxModerationInputRunes/17)
	if _, hit := prepared.Match(text); hit {
		b.Fatal("unexpected keyword match")
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for range b.N {
		contentModerationBenchmarkRuleSink, contentModerationBenchmarkBoolSink = prepared.Match(text)
	}
}

func BenchmarkContentModerationKeywordNestedSuffix200(b *testing.B) {
	rules := make([]ContentModerationKeywordRule, maxContentModerationBlockedKeywordRunes)
	for index := range rules {
		rules[index] = ContentModerationKeywordRule{
			Keyword: strings.Repeat("a", index+1), Enabled: true,
		}
	}
	prepared := newContentModerationPreparedRuleSet(rules)
	text := "x" + strings.Repeat("a", maxModerationInputRunes-1)
	if _, hit := prepared.Match(text); hit {
		b.Fatal("unexpected keyword match")
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for range b.N {
		contentModerationBenchmarkRuleSink, contentModerationBenchmarkBoolSink = prepared.Match(text)
	}
}

func BenchmarkFindCompactKeywordComparableSpanBoundaryReject(b *testing.B) {
	for _, size := range []int{1_000, 2_000, 4_000, 8_000} {
		b.Run(fmt.Sprintf("Compact/%d", size), func(b *testing.B) {
			normalized := strings.Repeat("xapikey", size)
			compact := compactKeywordComparable(normalized)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				_, _, contentModerationBenchmarkBoolSink = findCompactKeywordComparableSpanWithBoundary(normalized, compact, "apikey")
			}
		})
		b.Run(fmt.Sprintf("Spaced/%d", size), func(b *testing.B) {
			normalized := strings.Repeat("xapi key ", size)
			compact := compactKeywordComparable(normalized)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				_, _, contentModerationBenchmarkBoolSink = findCompactKeywordComparableSpanWithBoundary(normalized, compact, "apikey")
			}
		})
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
