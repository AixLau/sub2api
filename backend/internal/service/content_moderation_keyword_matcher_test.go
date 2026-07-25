package service

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContentModerationKeywordMatcherMatchesLegacyBehavior(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		keywords []string
	}{
		{name: "miss", text: "clean prompt", keywords: []string{"blocked", "secret"}},
		{name: "case insensitive", text: "contains SECRET value", keywords: []string{"secret"}},
		{name: "configured order wins", text: "early appears before later", keywords: []string{"later", "early"}},
		{name: "overlap uses configured order", text: "abc", keywords: []string{"bc", "abc"}},
		{name: "boundary falls through to later keyword", text: "xabc", keywords: []string{"abc", "xabc"}},
		{name: "ascii start boundary", text: "notsecret secret", keywords: []string{"secret"}},
		{name: "ascii end boundary", text: "secretvalue secret", keywords: []string{"secret"}},
		{name: "compact punctuation", text: "s.e.c.r.e.t", keywords: []string{"secret"}},
		{name: "compact whitespace", text: "敏 感 词", keywords: []string{"敏感词"}},
		{name: "numeric compact disabled", text: "12 34", keywords: []string{"1234"}},
		{name: "url decoding", text: "s%65cret", keywords: []string{"secret"}},
		{name: "nfkc", text: "contains ＳＥＣＲＥＴ", keywords: []string{"secret"}},
		{name: "unicode", text: "这里包含敏感词和世界", keywords: []string{"世界", "敏感词"}},
		{name: "duplicates", text: "duplicate", keywords: []string{"duplicate", "DUPLICATE"}},
		{name: "empty entries", text: "blocked", keywords: []string{"", "blocked"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantKeyword, wantHit := matchBlockedKeyword(tt.text, tt.keywords)
			gotKeyword, gotHit := newContentModerationKeywordMatcher(tt.keywords).Match(tt.text)
			require.Equal(t, wantHit, gotHit)
			require.Equal(t, wantKeyword, gotKeyword)
		})
	}
}

func TestContentModerationKeywordMatcherRandomizedParity(t *testing.T) {
	rng := rand.New(rand.NewSource(20260714))
	const alphabet = "abcXYZ"
	for iteration := 0; iteration < 1000; iteration++ {
		keywords := make([]string, 1+rng.Intn(30))
		for index := range keywords {
			length := 1 + rng.Intn(8)
			var value strings.Builder
			for range length {
				_ = value.WriteByte(alphabet[rng.Intn(len(alphabet))])
			}
			keywords[index] = value.String()
		}
		var text strings.Builder
		for range 20 + rng.Intn(100) {
			_ = text.WriteByte(alphabet[rng.Intn(len(alphabet))])
		}

		wantKeyword, wantHit := matchBlockedKeyword(text.String(), keywords)
		gotKeyword, gotHit := newContentModerationKeywordMatcher(keywords).Match(text.String())
		require.Equal(t, wantHit, gotHit, "iteration %d", iteration)
		require.Equal(t, wantKeyword, gotKeyword, "iteration %d", iteration)
	}
}

func TestContentModerationPreparedRuleSetRandomizedSlowParity(t *testing.T) {
	rng := rand.New(rand.NewSource(20260724))
	runes := []rune("abcXYZ019 .-_/%敏感词ＡＰＩ\u200b")
	for iteration := 0; iteration < 1000; iteration++ {
		rules := make([]ContentModerationKeywordRule, 1+rng.Intn(20))
		for index := range rules {
			var keyword strings.Builder
			for range 1 + rng.Intn(8) {
				keyword.WriteRune(runes[rng.Intn(len(runes))])
			}
			rules[index] = ContentModerationKeywordRule{
				Keyword: keyword.String(), Enabled: rng.Intn(4) != 0,
				Category: ContentModerationKeywordCategoryCustom,
				Severity: ContentModerationKeywordSeverityHigh,
				Action:   ContentModerationKeywordActionBlock,
			}
		}
		var text strings.Builder
		for range 20 + rng.Intn(100) {
			text.WriteRune(runes[rng.Intn(len(runes))])
		}
		value := text.String()
		want := slowContentModerationKeywordMatches(value, rules)
		set := newContentModerationPreparedRuleSet(rules)
		got := set.Matches(value)

		require.Equal(t, want, got, "iteration %d", iteration)
		first, hit := set.Match(value)
		require.Equal(t, len(want) > 0, hit, "iteration %d", iteration)
		if len(want) > 0 {
			require.Equal(t, want[0], first, "iteration %d", iteration)
		}
	}
}

func TestContentModerationPreparedRuleSetDirectedSlowParity(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		rules []ContentModerationKeywordRule
	}{
		{
			name: "compact collision",
			text: "ab",
			rules: []ContentModerationKeywordRule{
				{Keyword: "a b", Enabled: true},
				{Keyword: "ab", Enabled: true},
			},
		},
		{
			name: "boundary rejection followed by valid occurrence",
			text: "xapikey api key",
			rules: []ContentModerationKeywordRule{
				{Keyword: "api key", Enabled: true},
				{Keyword: "apikey", Enabled: true},
			},
		},
		{
			name: "nested suffix outputs",
			text: "敏感世界",
			rules: []ContentModerationKeywordRule{
				{Keyword: "世界", Enabled: true},
				{Keyword: "敏感世界", Enabled: true},
				{Keyword: "感世界", Enabled: true},
			},
		},
		{
			name: "numeric compact disabled",
			text: "release 12 34",
			rules: []ContentModerationKeywordRule{
				{Keyword: "1234", Enabled: true},
			},
		},
		{
			name: "nfkc zero width and unicode punctuation",
			text: "ＳＥ\u200bＣ—ＲＥＴ",
			rules: []ContentModerationKeywordRule{
				{Keyword: "secret", Enabled: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := slowContentModerationKeywordMatches(tt.text, tt.rules)
			set := newContentModerationPreparedRuleSet(tt.rules)
			require.Equal(t, want, set.Matches(tt.text))
			first, hit := set.Match(tt.text)
			require.Equal(t, len(want) > 0, hit)
			if len(want) > 0 {
				require.Equal(t, want[0], first)
			}
		})
	}
}

func TestContentModerationPreparedRuleSetExpandsCompactCollisionOnce(t *testing.T) {
	rules := contentModerationCompactCollisionRules(1024)
	set := newContentModerationPreparedRuleSet(rules)
	matches := set.Matches("abcdefghijklmno")

	require.Len(t, matches, len(rules))
	for index := range matches {
		require.Equal(t, rules[index].Keyword, matches[index].Keyword)
	}
}

func TestContentModerationKeywordMatcherEarlyExitUsesMinimumEnabledIndex(t *testing.T) {
	matcher := newContentModerationKeywordMatcher([]string{"", "needle", "later"})
	require.Equal(t, int32(1), matcher.minimumKeyword)

	keyword, hit := matcher.Match("needle " + strings.Repeat("ordinary ", 1000))

	require.True(t, hit)
	require.Equal(t, "needle", keyword)
}

func contentModerationCompactCollisionRules(count int) []ContentModerationKeywordRule {
	const base = "abcdefghijklmno"
	maximum := 1 << (len(base) - 1)
	if count > maximum {
		count = maximum
	}
	rules := make([]ContentModerationKeywordRule, 0, count)
	for mask := 0; mask < count; mask++ {
		var keyword strings.Builder
		keyword.Grow(len(base) * 2)
		for index := 0; index < len(base); index++ {
			if index > 0 && mask&(1<<(index-1)) != 0 {
				keyword.WriteByte(' ')
			}
			keyword.WriteByte(base[index])
		}
		rules = append(rules, ContentModerationKeywordRule{Keyword: keyword.String(), Enabled: true})
	}
	return rules
}

func slowContentModerationKeywordMatches(text string, rules []ContentModerationKeywordRule) []ContentModerationKeywordRule {
	normalizedText := normalizeKeywordComparable(text)
	if normalizedText == "" {
		return nil
	}
	compactText := compactKeywordComparable(normalizedText)
	var matches []ContentModerationKeywordRule
	for _, rule := range normalizeContentModerationKeywordRules(rules) {
		if !rule.Enabled {
			continue
		}
		normalizedKeyword := normalizeKeywordComparable(rule.Keyword)
		if normalizedKeyword == "" {
			continue
		}
		_, _, direct := findKeywordComparableSpanWithBoundary(normalizedText, normalizedKeyword)
		compact := false
		if !direct && shouldUseCompactKeywordMatch(normalizedKeyword) {
			compactKeyword := compactKeywordComparable(normalizedKeyword)
			if compactKeyword != "" {
				_, _, compact = findCompactKeywordComparableSpanWithBoundary(normalizedText, compactText, compactKeyword)
			}
		}
		if direct || compact {
			matches = append(matches, rule)
		}
	}
	return matches
}
