package promptfilter

import (
	"encoding/json"
	"fmt"
	"regexp"
	"regexp/syntax"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	// BuiltinSourceRevision identifies the exact upstream rule set used here.
	BuiltinSourceRevision = "codex2api@6793e0b09fe170895878f73f256a3d7ee7e5a08b"
	// BuiltinRuleSetRevision identifies the upstream rules plus local supplemental
	// cyber and prompt-injection rules maintained in this repository.
	BuiltinRuleSetRevision = BuiltinSourceRevision + "+" + supplementalSourceRevision + "+" + candidateSourceRevision + "+" + DetectorRevision
	BuiltinSourceURL       = "https://github.com/james-6-23/codex2api/tree/6793e0b09fe170895878f73f256a3d7ee7e5a08b/security/promptfilter"
	BuiltinSourceAuthor    = "james-6-23/codex2api"
	// The upstream rule set is redistributed in this derivative with permission from its author.
	BuiltinSourcePermission = "redistributed with permission from the upstream author"
	// DetectorRevision changes whenever normalization, occurrence mapping, or
	// signal-family semantics change. It is part of the moderation policy/cache
	// revision and deliberately independent of the pinned pattern sources.
	DetectorRevision = "promptfilter-detector-v3"
)

const (
	ModeOff     = "off"
	ModeObserve = "observe"
	ModeWarn    = "warn"
	ModeBlock   = "block"

	DefaultThreshold       = 50
	DefaultStrictThreshold = 90
	DefaultMaxTextLength   = 80 * 1024
	defaultHeadScanLength  = 64 * 1024
	defaultTailScanLength  = 16 * 1024

	ActionAllow   = "allow"
	ActionObserve = "observe"
	ActionWarn    = "warn"
	ActionReview  = "review"
	ActionBlock   = "block"

	ScanChannelCanonical = "canonical"
	ScanChannelCompact   = "compact"

	SignalFamilyHierarchyOverride        = "hierarchy_override"
	SignalFamilyIdentityOverride         = "identity_override"
	SignalFamilySafetyOverride           = "safety_refusal_suppression"
	SignalFamilyAuthorizationFabrication = "authorization_fabrication"
	SignalFamilyToolPermissionBypass     = "tool_permission_bypass"
	SignalFamilySecretExtraction         = "secret_extraction"
	SignalFamilyOutputContractOverride   = "output_contract_override"
	SignalFamilyObfuscationEvasion       = "obfuscation_evasion"
)

type PatternConfig struct {
	Name           string `json:"name"`
	Regex          string `json:"regex,omitempty"`
	Weight         int    `json:"weight"`
	Category       string `json:"category,omitempty"`
	Strict         bool   `json:"strict,omitempty"`
	SignalFamily   string `json:"signal_family,omitempty"`
	Enabled        *bool  `json:"enabled,omitempty"`
	SourceRevision string `json:"source_revision,omitempty"`
	// Pattern is accepted for compatibility with older in-process callers. New rules use Regex.
	Pattern string `json:"pattern,omitempty"`
}

type Config struct {
	Mode             string          `json:"mode"`
	Threshold        int             `json:"threshold"`
	StrictThreshold  int             `json:"strict_threshold"`
	MaxTextLength    int             `json:"max_text_length"`
	CustomPatterns   []PatternConfig `json:"custom_patterns,omitempty"`
	DisabledPatterns []string        `json:"disabled_patterns,omitempty"`
}

type Match struct {
	Name           string `json:"name"`
	Weight         int    `json:"weight"`
	Category       string `json:"category,omitempty"`
	Strict         bool   `json:"strict,omitempty"`
	Operational    bool   `json:"operational,omitempty"`
	SignalFamily   string `json:"signal_family,omitempty"`
	Occurrence     int    `json:"occurrence,omitempty"`
	ScanChannel    string `json:"scan_channel,omitempty"`
	SourceRevision string `json:"source_revision,omitempty"`
	// StartByte and EndByte identify the raw user-text region that triggered the
	// pattern. They are intentionally excluded from serialized diagnostics: the
	// moderation service keeps the actual bounded payload in its encrypted
	// evidence store instead of leaking it through rule metadata.
	StartByte int `json:"-"`
	EndByte   int `json:"-"`
}

type Verdict struct {
	Action           string   `json:"action"`
	Score            int      `json:"score"`
	RawScore         int      `json:"raw_score"`
	StrictScore      int      `json:"strict_score"`
	Threshold        int      `json:"threshold"`
	StrictThreshold  int      `json:"strict_threshold"`
	StrictHit        bool     `json:"strict_hit"`
	OperationalHit   bool     `json:"operational_hit"`
	TerminalEligible bool     `json:"terminal_eligible"`
	SignalFamilies   []string `json:"signal_families,omitempty"`
	ReviewRequired   bool     `json:"review_required"`
	Matches          []Match  `json:"matches,omitempty"`
	TextPreview      string   `json:"text_preview,omitempty"`
	ExtractedRunes   int      `json:"extracted_runes"`
	ScannedRunes     int      `json:"scanned_runes"`
	ScanComplete     bool     `json:"scan_complete"`
	DetectorRevision string   `json:"detector_revision"`
	SourceRevision   string   `json:"source_revision"`
}

type compiledPattern struct {
	cfg          PatternConfig
	re           *regexp.Regexp
	operational  bool
	signalFamily string
	requiredText []string
}

type Engine struct {
	cfg      Config
	patterns []compiledPattern
}

var engineCache sync.Map // map[string]*Engine

func BuiltinPatternConfigs() []PatternConfig {
	out := builtinPatternConfigs()
	for idx := range out {
		if out[idx].Regex == "" {
			out[idx].Regex = out[idx].Pattern
		}
		out[idx].Pattern = ""
		if strings.TrimSpace(out[idx].SourceRevision) == "" {
			out[idx].SourceRevision = BuiltinSourceRevision
		}
	}
	return out
}

func builtinPatternConfigs() []PatternConfig {
	out := make([]PatternConfig, 0, len(defaultPatternConfigs)+len(supplementalPatternConfigs)+len(candidatePatternConfigs))
	out = append(out, defaultPatternConfigs...)
	out = append(out, supplementalPatternConfigs...)
	for idx := len(defaultPatternConfigs); idx < len(out); idx++ {
		if strings.TrimSpace(out[idx].SourceRevision) == "" {
			out[idx].SourceRevision = supplementalSourceRevision
		}
	}
	candidateStart := len(out)
	out = append(out, candidatePatternConfigs...)
	for idx := candidateStart; idx < len(out); idx++ {
		if strings.TrimSpace(out[idx].SourceRevision) == "" {
			out[idx].SourceRevision = candidateSourceRevision
		}
	}
	return out
}

// NewEngine compiles the pinned built-in set plus optional administrator rules.
func NewEngine(cfg Config) (*Engine, error) {
	cfg = normalizeConfig(cfg)
	keyBytes, _ := json.Marshal(cfg)
	cacheKey := string(keyBytes)
	if cached, ok := engineCache.Load(cacheKey); ok {
		return cached.(*Engine), nil
	}
	disabled := make(map[string]struct{}, len(cfg.DisabledPatterns))
	for _, name := range cfg.DisabledPatterns {
		disabled[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}
	builtins := builtinPatternConfigs()
	merged := make([]PatternConfig, 0, len(builtins)+len(cfg.CustomPatterns))
	for _, pattern := range builtins {
		if strings.TrimSpace(pattern.SourceRevision) == "" {
			pattern.SourceRevision = BuiltinSourceRevision
		}
		if pattern.Regex == "" {
			pattern.Regex = pattern.Pattern
		}
		pattern.Pattern = ""
		merged = append(merged, pattern)
	}
	for _, pattern := range cfg.CustomPatterns {
		if strings.TrimSpace(pattern.SourceRevision) == "" {
			pattern.SourceRevision = "custom"
		}
		merged = append(merged, pattern)
	}
	patterns := make([]compiledPattern, 0, len(merged))
	for _, pattern := range merged {
		pattern.Name = strings.TrimSpace(pattern.Name)
		pattern.Regex = strings.TrimSpace(pattern.Regex)
		pattern.Pattern = strings.TrimSpace(pattern.Pattern)
		if pattern.Regex == "" {
			pattern.Regex = pattern.Pattern
		}
		pattern.Category = strings.TrimSpace(pattern.Category)
		pattern.SourceRevision = strings.TrimSpace(pattern.SourceRevision)
		if pattern.Name == "" || pattern.Regex == "" || pattern.Weight <= 0 {
			continue
		}
		if _, ok := disabled[strings.ToLower(pattern.Name)]; ok {
			continue
		}
		if pattern.Enabled != nil && !*pattern.Enabled {
			continue
		}
		re, err := regexp.Compile(pattern.Regex)
		if err != nil {
			return nil, fmt.Errorf("compile prompt filter pattern %q: %w", pattern.Name, err)
		}
		patterns = append(patterns, compiledPattern{
			cfg:          pattern,
			re:           re,
			operational:  operationalPattern(pattern.Name),
			signalFamily: signalFamilyForPattern(pattern),
			requiredText: requiredPatternText(pattern.Regex),
		})
	}
	engine := &Engine{cfg: cfg, patterns: patterns}
	actual, _ := engineCache.LoadOrStore(cacheKey, engine)
	return actual.(*Engine), nil
}

// Inspect applies local evidence scoring. It never calls an external model.
func Inspect(text string, cfg Config) Verdict {
	cfg = normalizeConfig(cfg)
	verdict := Verdict{
		Action:           ActionAllow,
		Threshold:        cfg.Threshold,
		StrictThreshold:  cfg.StrictThreshold,
		ExtractedRunes:   utf8.RuneCountInString(text),
		ScannedRunes:     utf8.RuneCountInString(text),
		ScanComplete:     true,
		DetectorRevision: DetectorRevision,
		SourceRevision:   BuiltinRuleSetRevision,
	}
	if cfg.Mode == ModeOff || strings.TrimSpace(text) == "" {
		return verdict
	}
	engine, err := NewEngine(cfg)
	if err != nil {
		verdict.Action = ActionReview
		verdict.ReviewRequired = true
		return verdict
	}
	return engine.inspect(text)
}

func (e *Engine) inspect(text string) Verdict {
	cfg := e.cfg
	segments, scanComplete, scannedRunes := buildScanSegments(text, cfg.MaxTextLength)
	verdict := Verdict{
		Action:           ActionAllow,
		Threshold:        cfg.Threshold,
		StrictThreshold:  cfg.StrictThreshold,
		ExtractedRunes:   utf8.RuneCountInString(text),
		ScannedRunes:     scannedRunes,
		ScanComplete:     scanComplete,
		DetectorRevision: DetectorRevision,
		SourceRevision:   BuiltinRuleSetRevision,
	}
	if verdict.ScannedRunes < 3 {
		return verdict
	}
	views := make([]mappedScanView, 0, len(segments)*2)
	for _, segment := range segments {
		canonical := canonicalMappedScanView(segment)
		if canonical.text == "" {
			continue
		}
		views = append(views, canonical)
		compact := compactMappedScanView(canonical)
		if compact.text != "" && compact.text != canonical.text {
			views = append(views, compact)
		}
	}
	if len(views) == 0 {
		return verdict
	}
	type occurrenceKey struct {
		name       string
		start, end int
	}
	occurrenceIndexes := make(map[occurrenceKey]int)
	scoreMatchesByName := make(map[string]Match)
	for _, pattern := range e.patterns {
		patternMatched := false
		var scoreMatch Match
		for _, view := range views {
			if view.channel == ScanChannelCompact && !isPromptInjectionPattern(pattern.cfg.Name, pattern.signalFamily) {
				continue
			}
			if len(pattern.requiredText) > 0 && !containsAnyRequiredPatternText(view.text, pattern.requiredText) {
				continue
			}
			for _, normalizedSpan := range pattern.re.FindAllStringIndex(view.text, -1) {
				if len(normalizedSpan) != 2 || normalizedSpan[1] <= normalizedSpan[0] {
					continue
				}
				startByte, endByte, ok := view.rawSpan(normalizedSpan[0], normalizedSpan[1])
				if !ok || endByte <= startByte {
					continue
				}
				match := Match{
					Name:           pattern.cfg.Name,
					Weight:         pattern.cfg.Weight,
					Category:       pattern.cfg.Category,
					Strict:         pattern.cfg.Strict,
					Operational:    pattern.operational,
					SignalFamily:   pattern.signalFamily,
					ScanChannel:    view.channel,
					SourceRevision: pattern.cfg.SourceRevision,
					StartByte:      startByte,
					EndByte:        endByte,
				}
				key := occurrenceKey{name: match.Name, start: startByte, end: endByte}
				if idx, exists := occurrenceIndexes[key]; exists {
					// Later same-name pattern definitions retain the legacy override
					// semantics, while canonical evidence wins over a duplicate compact
					// projection of the same raw span.
					if verdict.Matches[idx].ScanChannel == ScanChannelCanonical && match.ScanChannel == ScanChannelCompact {
						match.ScanChannel = ScanChannelCanonical
					}
					verdict.Matches[idx] = match
				} else {
					occurrenceIndexes[key] = len(verdict.Matches)
					verdict.Matches = append(verdict.Matches, match)
				}
				if !patternMatched {
					scoreMatch = match
				}
				patternMatched = true
			}
		}
		if patternMatched {
			// Repeated occurrences are evidence, not additional score. Scoring once
			// per rule name preserves existing thresholds and prevents keyword
			// flooding from manufacturing a terminal verdict.
			scoreMatchesByName[pattern.cfg.Name] = scoreMatch
		}
	}
	if len(verdict.Matches) == 0 {
		return verdict
	}
	for _, match := range scoreMatchesByName {
		verdict.RawScore += match.Weight
		if match.Strict {
			verdict.StrictScore += match.Weight
		}
		if match.Strict && match.Operational {
			verdict.OperationalHit = true
		}
	}
	sort.Slice(verdict.Matches, func(i, j int) bool {
		if verdict.Matches[i].Weight == verdict.Matches[j].Weight {
			if verdict.Matches[i].Name == verdict.Matches[j].Name {
				if verdict.Matches[i].StartByte == verdict.Matches[j].StartByte {
					return verdict.Matches[i].EndByte < verdict.Matches[j].EndByte
				}
				return verdict.Matches[i].StartByte < verdict.Matches[j].StartByte
			}
			return verdict.Matches[i].Name < verdict.Matches[j].Name
		}
		return verdict.Matches[i].Weight > verdict.Matches[j].Weight
	})
	occurrencesByName := make(map[string]int)
	for idx := range verdict.Matches {
		occurrencesByName[verdict.Matches[idx].Name]++
		verdict.Matches[idx].Occurrence = occurrencesByName[verdict.Matches[idx].Name]
	}
	verdict.Score = verdict.RawScore
	verdict.StrictHit = verdict.StrictScore >= cfg.StrictThreshold
	verdict.SignalFamilies = promptInjectionSignalFamilies(verdict.Matches)
	verdict.TerminalEligible = verdict.ScanComplete && verdict.StrictHit && verdict.OperationalHit &&
		len(verdict.SignalFamilies) >= 2 && hasProtectedHierarchySignal(verdict.SignalFamilies)
	verdict.ReviewRequired = !verdict.OperationalHit || !verdict.StrictHit
	if verdict.OperationalHit {
		verdict.ReviewRequired = false
	}
	switch cfg.Mode {
	case ModeBlock:
		if verdict.OperationalHit {
			verdict.Action = ActionBlock
		} else {
			verdict.Action = ActionReview
		}
	case ModeWarn:
		verdict.Action = ActionWarn
	case ModeObserve:
		verdict.Action = ActionObserve
	default:
		verdict.Action = ActionAllow
	}
	return verdict
}

func promptInjectionSignalFamilies(matches []Match) []string {
	seen := make(map[string]struct{})
	for _, match := range matches {
		if !IsPromptInjectionMatch(match) {
			continue
		}
		family := strings.TrimSpace(match.SignalFamily)
		if family == "" {
			continue
		}
		seen[family] = struct{}{}
	}
	families := make([]string, 0, len(seen))
	for family := range seen {
		families = append(families, family)
	}
	sort.Strings(families)
	return families
}

func hasProtectedHierarchySignal(families []string) bool {
	for _, family := range families {
		switch family {
		case SignalFamilyHierarchyOverride, SignalFamilyIdentityOverride,
			SignalFamilySafetyOverride, SignalFamilyToolPermissionBypass,
			SignalFamilySecretExtraction, SignalFamilyOutputContractOverride:
			return true
		}
	}
	return false
}

// requiredPatternText extracts only literals that are provably required by
// every successful path through a regexp. It is therefore safe as a negative
// prefilter: expressions that cannot prove a required literal return nil and
// still execute normally. Alternations contribute one requirement per branch;
// concatenations choose the strongest provable child requirement.
func requiredPatternText(expression string) []string {
	parsed, err := syntax.Parse(expression, syntax.Perl)
	if err != nil {
		return nil
	}
	needles, guaranteed := requiredRegexpText(parsed.Simplify())
	if !guaranteed {
		return nil
	}
	seen := make(map[string]struct{}, len(needles))
	out := make([]string, 0, len(needles))
	for _, needle := range needles {
		needle = strings.ToLower(strings.TrimSpace(needle))
		if utf8.RuneCountInString(needle) < 2 {
			continue
		}
		if _, ok := seen[needle]; ok {
			continue
		}
		seen[needle] = struct{}{}
		out = append(out, needle)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func requiredRegexpText(expression *syntax.Regexp) ([]string, bool) {
	if expression == nil {
		return nil, false
	}
	switch expression.Op {
	case syntax.OpLiteral:
		if len(expression.Rune) < 2 {
			return nil, false
		}
		return []string{strings.ToLower(string(expression.Rune))}, true
	case syntax.OpCapture:
		if len(expression.Sub) != 1 {
			return nil, false
		}
		return requiredRegexpText(expression.Sub[0])
	case syntax.OpConcat:
		var best []string
		bestStrength := 0
		for _, child := range expression.Sub {
			needles, guaranteed := requiredRegexpText(child)
			if !guaranteed {
				continue
			}
			strength := shortestRequiredTextRunes(needles)
			if strength > bestStrength {
				best, bestStrength = needles, strength
			}
		}
		return best, len(best) > 0
	case syntax.OpAlternate:
		if len(expression.Sub) == 0 {
			return nil, false
		}
		out := make([]string, 0, len(expression.Sub))
		for _, child := range expression.Sub {
			needles, guaranteed := requiredRegexpText(child)
			if !guaranteed {
				return nil, false
			}
			out = append(out, needles...)
		}
		return out, len(out) > 0
	case syntax.OpPlus:
		if len(expression.Sub) != 1 {
			return nil, false
		}
		return requiredRegexpText(expression.Sub[0])
	case syntax.OpRepeat:
		if expression.Min < 1 || len(expression.Sub) != 1 {
			return nil, false
		}
		return requiredRegexpText(expression.Sub[0])
	default:
		return nil, false
	}
}

func shortestRequiredTextRunes(needles []string) int {
	shortest := 0
	for _, needle := range needles {
		length := utf8.RuneCountInString(needle)
		if shortest == 0 || length < shortest {
			shortest = length
		}
	}
	return shortest
}

func containsAnyRequiredPatternText(text string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func normalizeConfig(cfg Config) Config {
	if cfg.Mode == "" {
		cfg.Mode = ModeObserve
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Mode)) {
	case ModeOff:
		cfg.Mode = ModeOff
	case ModeBlock:
		cfg.Mode = ModeBlock
	case ModeWarn:
		cfg.Mode = ModeWarn
	default:
		cfg.Mode = ModeObserve
	}
	if cfg.Threshold <= 0 {
		cfg.Threshold = DefaultThreshold
	}
	if cfg.StrictThreshold <= 0 {
		cfg.StrictThreshold = DefaultStrictThreshold
	}
	if cfg.StrictThreshold < cfg.Threshold {
		cfg.StrictThreshold = cfg.Threshold
	}
	if cfg.MaxTextLength <= 0 || cfg.MaxTextLength > 1024*1024 {
		cfg.MaxTextLength = DefaultMaxTextLength
	}
	return cfg
}

func limitScanText(text string, maxRunes int) string {
	runes := []rune(text)
	if maxRunes <= 0 || len(runes) <= maxRunes {
		return text
	}
	head := defaultHeadScanLength
	tail := defaultTailScanLength
	if maxRunes < head+tail {
		head = maxRunes / 2
		tail = maxRunes - head
	}
	return string(runes[:head]) + "\n[...prompt-filter-truncated...]\n" + string(runes[len(runes)-tail:])
}

func normalizeForScan(text string) string {
	segments, _, _ := buildScanSegments(text, utf8.RuneCountInString(text))
	if len(segments) == 0 {
		return ""
	}
	return canonicalMappedScanView(segments[0]).text
}

type scanSegment struct {
	text     string
	rawStart int
	runes    int
}

type mappedScanUnit struct {
	viewStart int
	viewEnd   int
	rawStart  int
	rawEnd    int
}

type mappedScanView struct {
	text    string
	channel string
	units   []mappedScanUnit
}

func buildScanSegments(text string, maxRunes int) ([]scanSegment, bool, int) {
	runes := []rune(text)
	if len(runes) == 0 {
		return nil, true, 0
	}
	if maxRunes <= 0 || len(runes) <= maxRunes {
		return []scanSegment{{text: text, runes: len(runes)}}, true, len(runes)
	}
	headRunes := maxRunes * 4 / 5
	if headRunes <= 0 {
		headRunes = 1
	}
	tailRunes := maxRunes - headRunes
	headText := string(runes[:headRunes])
	segments := []scanSegment{{text: headText, runes: headRunes}}
	if tailRunes > 0 {
		tailText := string(runes[len(runes)-tailRunes:])
		segments = append(segments, scanSegment{
			text:     tailText,
			rawStart: len(text) - len(tailText),
			runes:    tailRunes,
		})
	}
	return segments, false, maxRunes
}

func canonicalMappedScanView(segment scanSegment) mappedScanView {
	view := mappedScanView{channel: ScanChannelCanonical}
	var builder strings.Builder
	builder.Grow(len(segment.text))
	var iter norm.Iter
	iter.InitString(norm.NFKC, segment.text)
	previousRawEnd := 0
	pendingSpace := false
	pendingSpaceStart := 0
	pendingSpaceEnd := 0
	appendRune := func(r rune, rawStart, rawEnd int) {
		start := builder.Len()
		builder.WriteRune(r)
		view.units = append(view.units, mappedScanUnit{
			viewStart: start,
			viewEnd:   builder.Len(),
			rawStart:  rawStart,
			rawEnd:    rawEnd,
		})
	}
	for !iter.Done() {
		normalized := iter.Next()
		rawEnd := iter.Pos()
		rawStart := previousRawEnd
		previousRawEnd = rawEnd
		for len(normalized) > 0 {
			r, size := utf8.DecodeRune(normalized)
			normalized = normalized[size:]
			if isPromptFilterZeroWidth(r) {
				continue
			}
			if unicode.IsSpace(r) {
				if len(view.units) > 0 {
					if !pendingSpace {
						pendingSpaceStart = segment.rawStart + rawStart
					}
					pendingSpace = true
					pendingSpaceEnd = segment.rawStart + rawEnd
				}
				continue
			}
			if pendingSpace {
				appendRune(' ', pendingSpaceStart, pendingSpaceEnd)
				pendingSpace = false
			}
			appendRune(unicode.ToLower(r), segment.rawStart+rawStart, segment.rawStart+rawEnd)
		}
	}
	view.text = builder.String()
	return view
}

func compactMappedScanView(canonical mappedScanView) mappedScanView {
	view := mappedScanView{channel: ScanChannelCompact}
	var builder strings.Builder
	builder.Grow(len(canonical.text))
	unitIndex := 0
	for _, r := range canonical.text {
		if unitIndex >= len(canonical.units) {
			break
		}
		unit := canonical.units[unitIndex]
		unitIndex++
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) || unicode.IsControl(r) {
			continue
		}
		start := builder.Len()
		builder.WriteRune(r)
		view.units = append(view.units, mappedScanUnit{
			viewStart: start,
			viewEnd:   builder.Len(),
			rawStart:  unit.rawStart,
			rawEnd:    unit.rawEnd,
		})
	}
	view.text = builder.String()
	return view
}

func (view mappedScanView) rawSpan(start, end int) (int, int, bool) {
	if start < 0 || end <= start || end > len(view.text) || len(view.units) == 0 {
		return 0, 0, false
	}
	first := sort.Search(len(view.units), func(idx int) bool {
		return view.units[idx].viewEnd > start
	})
	lastExclusive := sort.Search(len(view.units), func(idx int) bool {
		return view.units[idx].viewStart >= end
	})
	if first >= len(view.units) || lastExclusive <= first {
		return 0, 0, false
	}
	last := lastExclusive - 1
	return view.units[first].rawStart, view.units[last].rawEnd, true
}

func isPromptFilterZeroWidth(r rune) bool {
	switch r {
	case '\u034f', '\u061c', '\u180e', '\u200b', '\u200c', '\u200d', '\u200e', '\u200f',
		'\u202a', '\u202b', '\u202c', '\u202d', '\u202e', '\u2060', '\u2061', '\u2062', '\u2063',
		'\u2064', '\u2066', '\u2067', '\u2068', '\u2069', '\ufeff':
		return true
	default:
		return false
	}
}

func signalFamilyForPattern(pattern PatternConfig) string {
	if family := strings.ToLower(strings.TrimSpace(pattern.SignalFamily)); family != "" {
		return family
	}
	switch strings.ToLower(strings.TrimSpace(pattern.Name)) {
	case "prompt_injection_override":
		return SignalFamilyHierarchyOverride
	case "jailbreak_operational_request":
		return SignalFamilySafetyOverride
	case "system_prompt_extraction":
		return SignalFamilySecretExtraction
	case "agent_tool_permission_bypass":
		return SignalFamilyToolPermissionBypass
	case "prompt_obfuscation_evasion":
		return SignalFamilyObfuscationEvasion
	default:
		return ""
	}
}

// IsPromptInjectionMatch reports whether a match is part of the control-plane
// prompt-injection detector rather than an unrelated cyber/content rule.
func IsPromptInjectionMatch(match Match) bool {
	return isPromptInjectionPattern(match.Name, match.SignalFamily)
}

func isPromptInjectionPattern(name, signalFamily string) bool {
	if strings.TrimSpace(signalFamily) != "" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "jailbreak_operational_request", "jailbreak_topic", "prompt_injection_override",
		"system_prompt_extraction", "prompt_obfuscation_evasion", "agent_tool_permission_bypass":
		return true
	default:
		return false
	}
}

func operationalPattern(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "credential_theft", "evasion", "operational_remote_access_request",
		"reverse_engineering_secret_extraction", "reverse_engineering_license_bypass", "reverse_engineering_anti_debug_bypass",
		"frida_hook_abuse", "license_cracking", "data_exfiltration", "ransomware_deployment", "credential_dumping",
		"token_theft", "mass_exploitation", "jailbreak_operational_request", "prompt_injection_override",
		"system_prompt_extraction", "agent_tool_permission_bypass", "web_exploitation_unauthorized_harm_request",
		"binary_exploitation_unauthorized_harm_request", "crypto_unauthorized_key_theft_request",
		"pentest_unauthorized_harm_request", "credential_attack_operational_request":
		return true
	default:
		return false
	}
}
